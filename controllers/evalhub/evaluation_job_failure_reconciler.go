/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package evalhub

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Labels and annotations aligned with EvalHub Kubernetes runtime
// (see eval-hub internal/eval_hub/runtimes/k8s/job_builders.go).
const (
	evalHubAppLabel            = "app"
	evalHubAppValue            = "evalhub"
	evalHubComponentLabel      = "component"
	evalHubComponentValue      = "evaluation-job"
	evalHubJobIDLabel          = "job_id"
	evalHubProviderIDLabel     = "provider_id"
	evalHubBenchmarkIDLabel    = "benchmark_id"
	evalHubBenchmarkIndexLabel = "benchmark_index"
	annotationFailureReported  = "trustyai.opendatahub.io/evalhub-failure-reported"
	httpHeaderTenant           = "X-Tenant"
	eventsPathFmt              = "%s/api/v1/evaluations/jobs/%s/events"
	messageCodeRuntimeFailure  = "RUNTIME_FAILURE"
	// openshiftServiceCAMountPath: PEM for the OpenShift service signing CA (trust in-cluster *.svc HTTPS, e.g. EvalHub).
	// Appended to the HTTP client root CAs; the in-cluster SA transport defaults to apiserver trust only.
	openshiftServiceCAMountPath = "/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt"
	// evalHubExtraCABundleEnv, if set, is a filesystem path to additional PEM CA certs (optional).
	evalHubExtraCABundleEnv = "TRUSTYAI_EVALHUB_HTTP_EXTRA_CA_BUNDLE"
	adapterContainerName    = "adapter"
	initContainerName       = "init"
	sidecarContainerName    = "sidecar"
)

// evalHubEvaluationJobFailureControllerName matches ctrl.NewControllerManagedBy(mgr).Named(...) for logs and registration.
const evalHubEvaluationJobFailureControllerName = "evalhub-evaluation-job-failure"

//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;patch
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch

// EvalHubEvaluationJobFailureReconciler POSTs a failed benchmark event to EvalHub when init, adapter,
// or sidecar on the evaluation Job is in a state that usually means EvalHub was not notified yet.
//
// Adapter: only OOMKilled (terminated) or fatal Waiting reasons (e.g. ImagePullBackOff) trigger the operator;
// terminated Reason=Error / non-zero exit is ignored because the adapter often calls EvalHub with failed status after a normal run.
//
// Init and sidecar use broader terminated/Waiting rules. A Job that is “failed” in Kubernetes without those container signals is skipped.
type EvalHubEvaluationJobFailureReconciler struct {
	client.Client
	// RESTConfig is used to build an HTTP transport that authenticates like the operator (SA token + cluster CA).
	RESTConfig *rest.Config

	// tenantNamespaces keys are namespace names with label evalhub.trustyai.opendatahub.io/tenant; values are struct{}.
	tenantNamespaces sync.Map
}

func evalHubJobPodLabelSelector() metav1.LabelSelector {
	return metav1.LabelSelector{
		MatchLabels: map[string]string{
			evalHubAppLabel:       evalHubAppValue,
			evalHubComponentLabel: evalHubComponentValue,
		},
	}
}

func namespaceCarriesTenantLabel(ns *corev1.Namespace) bool {
	if ns == nil || ns.Labels == nil {
		return false
	}
	_, ok := ns.Labels[tenantLabel]
	return ok
}

// tenantNamespaceSync keeps tenantNamespaces in sync with Namespace label changes (no reconcile queue adds).
type tenantNamespaceSync struct {
	r *EvalHubEvaluationJobFailureReconciler
}

var _ handler.EventHandler = (*tenantNamespaceSync)(nil)

func (h *tenantNamespaceSync) Create(ctx context.Context, e event.CreateEvent, _ workqueue.RateLimitingInterface) {
	if ns, ok := e.Object.(*corev1.Namespace); ok && namespaceCarriesTenantLabel(ns) {
		h.r.addTenantNamespace(ns.Name)
	}
}

func (h *tenantNamespaceSync) Update(ctx context.Context, e event.UpdateEvent, _ workqueue.RateLimitingInterface) {
	oldNs, okOld := e.ObjectOld.(*corev1.Namespace)
	newNs, okNew := e.ObjectNew.(*corev1.Namespace)
	if !okOld || !okNew {
		return
	}
	if namespaceCarriesTenantLabel(oldNs) && !namespaceCarriesTenantLabel(newNs) {
		h.r.removeTenantNamespace(newNs.Name)
	}
	if !namespaceCarriesTenantLabel(oldNs) && namespaceCarriesTenantLabel(newNs) {
		h.r.addTenantNamespace(newNs.Name)
	}
}

func (h *tenantNamespaceSync) Delete(ctx context.Context, e event.DeleteEvent, _ workqueue.RateLimitingInterface) {
	if e.Object != nil {
		h.r.removeTenantNamespace(e.Object.GetName())
	}
}

func (h *tenantNamespaceSync) Generic(ctx context.Context, e event.GenericEvent, _ workqueue.RateLimitingInterface) {
}

func (r *EvalHubEvaluationJobFailureReconciler) addTenantNamespace(name string) {
	r.tenantNamespaces.Store(name, struct{}{})
}

func (r *EvalHubEvaluationJobFailureReconciler) removeTenantNamespace(name string) {
	r.tenantNamespaces.Delete(name)
}

func (r *EvalHubEvaluationJobFailureReconciler) isTenantNamespace(name string) bool {
	_, ok := r.tenantNamespaces.Load(name)
	return ok
}

// bootstrapTenantNamespaces must use an API reader that does not rely on the controller-runtime cache.
// registerEvalHubEvaluationJobFailureController runs during SetupWithManager, before mgr.Start(), so
// mgr.GetClient().List would fail with "the cache is not started".
func (r *EvalHubEvaluationJobFailureReconciler) bootstrapTenantNamespaces(ctx context.Context, apiReader client.Reader) error {
	nsList := &corev1.NamespaceList{}
	if err := apiReader.List(ctx, nsList, client.HasLabels{tenantLabel}); err != nil {
		return err
	}
	var toDelete []string
	r.tenantNamespaces.Range(func(key, _ interface{}) bool {
		toDelete = append(toDelete, key.(string))
		return true
	})
	for _, k := range toDelete {
		r.tenantNamespaces.Delete(k)
	}
	for i := range nsList.Items {
		r.tenantNamespaces.Store(nsList.Items[i].Name, struct{}{})
	}
	return nil
}

// registerEvalHubEvaluationJobFailureController registers the batch Job–centric failure sync reconciler.
// It is invoked from EvalHubReconciler.SetupWithManager (single setup entrypoint from ControllerSetUp).
func registerEvalHubEvaluationJobFailureController(mgr manager.Manager) error {
	r := &EvalHubEvaluationJobFailureReconciler{
		Client:     mgr.GetClient(),
		RESTConfig: rest.CopyConfig(mgr.GetConfig()),
	}
	if err := r.bootstrapTenantNamespaces(context.Background(), mgr.GetAPIReader()); err != nil {
		return fmt.Errorf("evalhub failure watcher: bootstrap tenant namespaces: %w", err)
	}

	labelPred, err := predicate.LabelSelectorPredicate(evalHubJobPodLabelSelector())
	if err != nil {
		return fmt.Errorf("evalhub failure watcher: job/pod label predicate: %w", err)
	}

	jobPred := predicate.And(labelPred, jobUpdatePredicate(r))
	podPred := predicate.And(labelPred, podUpdatePredicate(r))

	b := ctrl.NewControllerManagedBy(mgr).
		Named(evalHubEvaluationJobFailureControllerName).
		For(&batchv1.Job{}, builder.WithPredicates(jobPred)).
		Watches(
			&corev1.Pod{},
			handler.EnqueueRequestsFromMapFunc(r.mapPodToEvalHubJob),
			builder.WithPredicates(podPred),
		).
		Watches(
			&corev1.Namespace{},
			&tenantNamespaceSync{r: r},
			builder.WithPredicates(namespaceTenantEdgePredicate()),
		)
	return b.Complete(r)
}

func namespaceTenantEdgePredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			ns, ok := e.Object.(*corev1.Namespace)
			return ok && namespaceCarriesTenantLabel(ns)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldNs, okOld := e.ObjectOld.(*corev1.Namespace)
			newNs, okNew := e.ObjectNew.(*corev1.Namespace)
			if !okOld || !okNew {
				return false
			}
			return namespaceCarriesTenantLabel(oldNs) || namespaceCarriesTenantLabel(newNs)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return true
		},
		GenericFunc: func(e event.GenericEvent) bool { return false },
	}
}

func jobUpdatePredicate(r *EvalHubEvaluationJobFailureReconciler) predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool { return false },
		UpdateFunc: func(e event.UpdateEvent) bool {
			newJob, ok := e.ObjectNew.(*batchv1.Job)
			if !ok || !r.isTenantNamespace(newJob.Namespace) {
				return false
			}
			if !isEvalHubEvaluationJob(newJob) || failureAlreadyReported(newJob) {
				return false
			}
			// Job updates often lag pod/container status; reconcile runs detectFailure which only
			// posts when init/adapter/sidecar show operator-only failure reasons.
			return jobStatusIndicatesTerminalFailure(newJob)
		},
		DeleteFunc:  func(event.DeleteEvent) bool { return false },
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}

func podUpdatePredicate(r *EvalHubEvaluationJobFailureReconciler) predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool { return false },
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldPod, okOld := e.ObjectOld.(*corev1.Pod)
			newPod, okNew := e.ObjectNew.(*corev1.Pod)
			if !okOld || !okNew || !podOwnedByJob(newPod) || !r.isTenantNamespace(newPod.Namespace) {
				return false
			}
			// Only enqueue when init/adapter/sidecar transition into a state that implies no EvalHub callback
			return !podIndicatesOperatorOnlyFailure(oldPod) && podIndicatesOperatorOnlyFailure(newPod)
		},
		DeleteFunc:  func(event.DeleteEvent) bool { return false },
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}

func (r *EvalHubEvaluationJobFailureReconciler) mapPodToEvalHubJob(ctx context.Context, obj client.Object) []reconcile.Request {
	pod, ok := obj.(*corev1.Pod)
	if !ok || !r.isTenantNamespace(pod.Namespace) {
		return nil
	}
	jobName := jobNameFromPodOwner(pod)
	if jobName == "" {
		return nil
	}
	var job batchv1.Job
	if err := r.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: jobName}, &job); err != nil {
		return nil
	}
	if !isEvalHubEvaluationJob(&job) || failureAlreadyReported(&job) {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: job.Namespace, Name: job.Name}}}
}

func jobNameFromPodOwner(pod *corev1.Pod) string {
	for _, ref := range pod.OwnerReferences {
		if ref.Kind == "Job" && (ref.APIVersion == batchv1.SchemeGroupVersion.String() || ref.APIVersion == "batch/v1") {
			return ref.Name
		}
	}
	return ""
}

func podOwnedByJob(pod *corev1.Pod) bool {
	return jobNameFromPodOwner(pod) != ""
}

func (r *EvalHubEvaluationJobFailureReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("reconciling", "controller", evalHubEvaluationJobFailureControllerName, "namespace", req.Namespace, "name", req.Name)

	var job batchv1.Job
	if err := r.Get(ctx, req.NamespacedName, &job); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !isEvalHubEvaluationJob(&job) {
		return ctrl.Result{}, nil
	}
	if !r.isTenantNamespace(job.Namespace) {
		return ctrl.Result{}, nil
	}
	if failureAlreadyReported(&job) {
		return ctrl.Result{}, nil
	}

	failed, msg := r.detectFailure(ctx, &job)
	if !failed {
		return ctrl.Result{}, nil
	}

	baseURL, err := r.getEvalHubURLFromJob(ctx, &job)
	if err != nil {
		log.Info("skipping: cannot resolve EvalHub from job", "error", err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	jobID := strings.TrimSpace(job.Labels[evalHubJobIDLabel])
	if jobID == "" {
		log.Info("evalhub job missing job_id label", "job", job.Name, "namespace", job.Namespace)
		return ctrl.Result{}, nil
	}

	providerID := job.Labels[evalHubProviderIDLabel]
	benchmarkID := job.Labels[evalHubBenchmarkIDLabel]
	if providerID == "" || benchmarkID == "" {
		log.Info("evalhub job missing provider_id or benchmark_id label", "job", job.Name)
		return ctrl.Result{}, nil
	}

	benchmarkIndex := benchmarkIndexFromJob(&job)
	if err := postEvalHubBenchmarkFailed(ctx, r.RESTConfig, baseURL, job.Namespace, jobID, providerID, benchmarkID, benchmarkIndex, msg); err != nil {
		log.Error(err, "failed to post EvalHub benchmark failure event", "job", job.Name, "evalJobID", jobID)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	patch := client.MergeFrom(job.DeepCopy())
	if job.Annotations == nil {
		job.Annotations = map[string]string{}
	}
	job.Annotations[annotationFailureReported] = "true"
	if err := r.Patch(ctx, &job, patch); err != nil {
		log.Error(err, "failed to annotate job after EvalHub failure sync", "job", job.Name)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	log.Info("reported EvalHub benchmark failure", "evalJobID", jobID, "k8sJob", job.Name, "namespace", job.Namespace)
	return ctrl.Result{}, nil
}

func jobConditionTrue(conditions []batchv1.JobCondition, t batchv1.JobConditionType) bool {
	for i := range conditions {
		if conditions[i].Type == t && conditions[i].Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// jobStatusIndicatesTerminalFailure is used in predicates (no API calls).
func jobStatusIndicatesTerminalFailure(job *batchv1.Job) bool {
	if jobConditionTrue(job.Status.Conditions, batchv1.JobFailed) {
		return true
	}
	if job.Status.Active == 0 && job.Status.Failed > 0 &&
		!jobConditionTrue(job.Status.Conditions, batchv1.JobComplete) &&
		job.Status.Succeeded == 0 {
		return true
	}
	return false
}

func (r *EvalHubEvaluationJobFailureReconciler) detectFailure(ctx context.Context, job *batchv1.Job) (bool, string) {
	// Do not use generic JobFailed alone: the adapter may already have POSTed failed to EvalHub.
	// Only synthesize an event when workload containers (init/adapter/sidecar) show pre-callback failures.
	if msg, ok := r.operatorOnlyFailureFromPods(ctx, job); ok {
		return true, msg
	}
	return false, ""
}

func (r *EvalHubEvaluationJobFailureReconciler) operatorOnlyFailureFromPods(ctx context.Context, job *batchv1.Job) (string, bool) {
	list := &corev1.PodList{}
	if err := r.List(ctx, list, client.InNamespace(job.Namespace), client.MatchingLabels{
		"batch.kubernetes.io/job-name": job.Name,
	}); err != nil {
		return "", false
	}
	var parts []string
	for i := range list.Items {
		pod := &list.Items[i]
		if msg, fatal := podOperatorOnlyFailureMessage(pod); fatal {
			parts = append(parts, fmt.Sprintf("pod %s: %s", pod.Name, msg))
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, "; "), true
}

func podIndicatesOperatorOnlyFailure(pod *corev1.Pod) bool {
	_, ok := podOperatorOnlyFailureMessage(pod)
	return ok
}

// podOperatorOnlyFailureMessage returns true when the eval-hub init, adapter, or sidecar is in a state
// that typically means EvalHub was never notified (vs. adapter exiting after reporting failure).
func podOperatorOnlyFailureMessage(pod *corev1.Pod) (string, bool) {
	for _, name := range []string{initContainerName, adapterContainerName, sidecarContainerName} {
		if msg, ok := containerCannotReportToEvalHub(pod, name); ok {
			return fmt.Sprintf("%s: %s", name, msg), true
		}
	}
	return "", false
}

// terminatedAdapterNeedsOperatorReport is true only when the adapter likely never reached EvalHub.
// The adapter often exits with Reason=Error and non-zero after it has already POSTed failed to EvalHub;
// treating that as operator-only would duplicate EvalHub events.
func terminatedAdapterNeedsOperatorReport(t *corev1.ContainerStateTerminated) bool {
	return t.Reason == "OOMKilled"
}

// terminatedInitOrSidecarNeedsOperatorReport catches init/sidecar failures that block or prevent callbacks.
func terminatedInitOrSidecarNeedsOperatorReport(t *corev1.ContainerStateTerminated) bool {
	return t.Reason == "OOMKilled" || t.Reason == "Error" ||
		(t.ExitCode != 0 && t.Reason != "Completed")
}

// containerCannotReportToEvalHub detects fatal init/adapter/sidecar states before or without a successful callback.
func containerCannotReportToEvalHub(pod *corev1.Pod, containerName string) (string, bool) {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name != containerName {
			continue
		}
		if cs.State.Terminated != nil {
			t := cs.State.Terminated
			needReport := terminatedInitOrSidecarNeedsOperatorReport(t)
			if containerName == adapterContainerName {
				needReport = terminatedAdapterNeedsOperatorReport(t)
			}
			if needReport {
				return fmt.Sprintf("terminated reason=%s exitCode=%d message=%s", t.Reason, t.ExitCode, t.Message), true
			}
		}
		if cs.State.Waiting != nil {
			w := cs.State.Waiting
			switch w.Reason {
			case "ImagePullBackOff", "ErrImagePull", "CrashLoopBackOff", "CreateContainerConfigError":
				return fmt.Sprintf("waiting reason=%s message=%s", w.Reason, w.Message), true
			}
		}
	}
	// Init container status lives in InitContainerStatuses
	for _, ics := range pod.Status.InitContainerStatuses {
		if ics.Name != containerName {
			continue
		}
		if ics.State.Terminated != nil {
			t := ics.State.Terminated
			// Only eval-hub "init" appears here; use full non-adapter rules.
			if terminatedInitOrSidecarNeedsOperatorReport(t) {
				return fmt.Sprintf("terminated reason=%s exitCode=%d message=%s", t.Reason, t.ExitCode, t.Message), true
			}
		}
		if ics.State.Waiting != nil {
			w := ics.State.Waiting
			switch w.Reason {
			case "ImagePullBackOff", "ErrImagePull", "CrashLoopBackOff", "CreateContainerConfigError":
				return fmt.Sprintf("waiting reason=%s message=%s", w.Reason, w.Message), true
			}
		}
	}
	return "", false
}

func isEvalHubEvaluationJob(job *batchv1.Job) bool {
	if job.Labels[evalHubAppLabel] != evalHubAppValue {
		return false
	}
	if job.Labels[evalHubComponentLabel] != evalHubComponentValue {
		return false
	}
	return true
}

// benchmarkIndexFromJob returns the EvalHub benchmark_index from the Job label (set by eval-hub k8s runtime).
// If missing or invalid, 0 is used (correct for single-benchmark jobs and older eval-hub images).
func benchmarkIndexFromJob(job *batchv1.Job) int {
	if job.Labels == nil {
		return 0
	}
	raw := strings.TrimSpace(job.Labels[evalHubBenchmarkIndexLabel])
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func failureAlreadyReported(job *batchv1.Job) bool {
	if job.Annotations == nil {
		return false
	}
	return job.Annotations[annotationFailureReported] == "true"
}

// getEvalHubURLFromJob resolves the EvalHub API base URL from the job pod ServiceAccount name.
// Operator naming: generateJobsServiceAccountName → <EvalHub CR name>-<EvalHub namespace>-job
// (see eval-hub job_config.go and controllers/evalhub/service_accounts.go).
func (r *EvalHubEvaluationJobFailureReconciler) getEvalHubURLFromJob(ctx context.Context, job *batchv1.Job) (string, error) {
	saName := job.Spec.Template.Spec.ServiceAccountName
	if saName == "" {
		return "", fmt.Errorf("job has no ServiceAccount set")
	}
	name, namespace, err := parseEvalHubJobSA(saName)
	if err != nil {
		return "", err
	}
	var eh evalhubv1alpha1.EvalHub
	key := client.ObjectKey{Namespace: namespace, Name: name}
	if err := r.Get(ctx, key, &eh); err != nil {
		return "", fmt.Errorf("get EvalHub %s/%s: %w", namespace, name, err)
	}
	if !eh.IsReady() {
		return "", fmt.Errorf("EvalHub %s/%s not Ready", namespace, name)
	}
	url := strings.TrimSpace(eh.Status.URL)
	if url == "" {
		return "", fmt.Errorf("EvalHub %s/%s has no URL", namespace, name)
	}
	return strings.TrimSuffix(url, "/"), nil
}

// parseEvalHubJobSA parses the job SA name pattern: {name}-{namespace}-job (last hyphen-separated segment before -job is the EvalHub namespace).
// If the EvalHub CR namespace contains hyphens (e.g. my-ns), the string cannot be split unambiguously; use a single-segment namespace or rely on operator normalization.
func parseEvalHubJobSA(saName string) (name, namespace string, err error) {
	if !strings.HasSuffix(saName, "-job") {
		return "", "", fmt.Errorf("SA name %q does not match pattern {name}-{namespace}-job", saName)
	}
	withoutSuffix := strings.TrimSuffix(saName, "-job")
	parts := strings.Split(withoutSuffix, "-")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("SA name %q invalid: need at least {name}-{namespace}-job", saName)
	}
	namespace = parts[len(parts)-1]
	name = strings.Join(parts[:len(parts)-1], "-")
	return name, namespace, nil
}

// JSON body compatible with EvalHub pkg/api StatusEvent.
type benchmarkStatusEventPayload struct {
	ProviderID     string       `json:"provider_id"`
	ID             string       `json:"id"`
	BenchmarkIndex int          `json:"benchmark_index,omitempty"`
	Status         string       `json:"status"`
	ErrorMessage   *msgInfoJSON `json:"error_message,omitempty"`
}

type msgInfoJSON struct {
	Message     string `json:"message"`
	MessageCode string `json:"message_code"`
}

type statusEventPayload struct {
	BenchmarkStatusEvent *benchmarkStatusEventPayload `json:"benchmark_status_event"`
}

func evalHubTLSRootCAs() (*x509.CertPool, error) {
	var pool *x509.CertPool
	if sys, err := x509.SystemCertPool(); err == nil && sys != nil {
		pool = sys
	} else {
		pool = x509.NewCertPool()
	}
	if b, err := os.ReadFile(openshiftServiceCAMountPath); err == nil && len(b) > 0 {
		pool.AppendCertsFromPEM(b)
	}
	if extraPath := strings.TrimSpace(os.Getenv(evalHubExtraCABundleEnv)); extraPath != "" {
		b, err := os.ReadFile(extraPath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", evalHubExtraCABundleEnv, err)
		}
		pool.AppendCertsFromPEM(b)
	}
	return pool, nil
}

// newEvalHubHTTPClient returns an HTTP client suitable for https://<evalhub>.<ns>.svc...:8443.
// It trusts the OpenShift service CA (for in-cluster serving certs) and attaches the operator's
// service account bearer credentials like rest.TransportFor would for the API server.
func newEvalHubHTTPClient(restCfg *rest.Config) (*http.Client, error) {
	rootCAs, err := evalHubTLSRootCAs()
	if err != nil {
		return nil, err
	}
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.TLSClientConfig = &tls.Config{
		RootCAs:    rootCAs,
		MinVersion: tls.VersionTLS12,
	}
	rt, err := transport.New(&transport.Config{
		Transport:       base,
		BearerToken:     restCfg.BearerToken,
		BearerTokenFile: restCfg.BearerTokenFile,
		Username:        restCfg.Username,
		Password:        restCfg.Password,
	})
	if err != nil {
		return nil, fmt.Errorf("evalhub http transport: %w", err)
	}
	return &http.Client{Transport: rt, Timeout: 30 * time.Second}, nil
}

func postEvalHubBenchmarkFailed(ctx context.Context, restCfg *rest.Config, baseURL, tenant, jobID, providerID, benchmarkID string, benchmarkIndex int, failureMsg string) error {
	httpClient, err := newEvalHubHTTPClient(restCfg)
	if err != nil {
		return err
	}

	body := statusEventPayload{
		BenchmarkStatusEvent: &benchmarkStatusEventPayload{
			ProviderID:     providerID,
			ID:             benchmarkID,
			BenchmarkIndex: benchmarkIndex,
			Status:         "failed",
			ErrorMessage: &msgInfoJSON{
				Message:     failureMsg,
				MessageCode: messageCodeRuntimeFailure,
			},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}

	url := fmt.Sprintf(eventsPathFmt, baseURL, jobID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(httpHeaderTenant, tenant)

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("EvalHub events returned %s", resp.Status)
	}
	return nil
}

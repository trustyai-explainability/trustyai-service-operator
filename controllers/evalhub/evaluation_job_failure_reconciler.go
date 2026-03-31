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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	// EvalHub CR identity for getEvalHubURLFromJob (set by eval-hub job_builders; must match eval-hub).
	evalHubInstanceNameLabel      = "evalhub_instance_name"
	evalHubInstanceNamespaceLabel = "evalhub_instance_namespace"
	// annotationFailurePending: in-flight until POST succeeds and we set annotationFailureReported.
	annotationFailurePending  = "trustyai.opendatahub.io/evalhub-failure-pending"
	annotationFailureReported = "trustyai.opendatahub.io/evalhub-failure-reported"
	httpHeaderTenant          = "X-Tenant"
	eventsPathFmt             = "%s/api/v1/evaluations/jobs/%s/events"
	messageCodeRuntimeFailure = "RUNTIME_FAILURE"
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

// failureWatcherLogFields returns stable key/value pairs for every log line from this reconciler.
// JSON logs: filter with evalhub_watcher=failure_sync or controller=evalhub-evaluation-job-failure.
func failureWatcherLogFields() []any {
	return []any{
		"evalhub_watcher", "failure_sync",
		"controller", evalHubEvaluationJobFailureControllerName,
	}
}

//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;patch;delete
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=evalhubs,verbs=get;list;watch

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
		CreateFunc: func(e event.CreateEvent) bool {
			newJob, ok := e.Object.(*batchv1.Job)
			if !ok || !r.isTenantNamespace(newJob.Namespace) {
				return false
			}
			if !isEvalHubEvaluationJob(newJob) || failureAlreadyReported(newJob) {
				return false
			}
			// Informer list/watch replays existing objects as Create; catch Jobs already terminal
			// before this controller started (CreateFunc previously returned false and missed them).
			return jobStatusIndicatesTerminalFailure(newJob)
		},
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
		CreateFunc: func(e event.CreateEvent) bool {
			newPod, ok := e.Object.(*corev1.Pod)
			if !ok || !podOwnedByJob(newPod) || !r.isTenantNamespace(newPod.Namespace) {
				return false
			}
			// Replay and rare creates where status already shows operator-only failure.
			return podIndicatesOperatorOnlyFailure(newPod)
		},
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

func (r *EvalHubEvaluationJobFailureReconciler) mapPodToEvalHubJob(_ context.Context, obj client.Object) []reconcile.Request {
	pod, ok := obj.(*corev1.Pod)
	if !ok || !r.isTenantNamespace(pod.Namespace) {
		return nil
	}
	jobName := jobNameFromPodOwner(pod)
	if jobName == "" {
		return nil
	}
	// Do not Get the Job here: transient API errors would drop the event. Reconcile re-validates
	// tenant, labels, and failureAlreadyReported after loading the Job.
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: pod.Namespace, Name: jobName}}}
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
	log.Info("reconcile start",
		append(failureWatcherLogFields(), "action", "reconcile", "namespace", req.Namespace, "name", req.Name)...)

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
		// POST succeeded in a prior reconcile; ensure the Job is removed (delete may have failed after patch).
		if err := r.deleteEvalHubFailureSyncedJob(ctx, &job); err != nil {
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		return ctrl.Result{}, nil
	}

	failed, msg, err := r.detectFailure(ctx, &job)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("detect failure: %w", err)
	}
	if !failed {
		log.Info("skip EvalHub POST: no operator-only container failure",
			append(failureWatcherLogFields(), "action", "skip_no_operator_failure", "job", job.Name, "namespace", job.Namespace)...)
		return ctrl.Result{}, nil
	}
	detailForLog := msg
	if len(detailForLog) > 512 {
		detailForLog = detailForLog[:512] + "…"
	}
	log.Info("operator-only failure detected",
		append(failureWatcherLogFields(), "action", "failure_detected", "job", job.Name, "namespace", job.Namespace, "detail", detailForLog)...)

	baseURL, err := r.getEvalHubURLFromJob(ctx, &job)
	if err != nil {
		log.Info("cannot resolve EvalHub URL from job",
			append(failureWatcherLogFields(), "action", "skip_no_evalhub_url", "error", err.Error())...)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	jobID := strings.TrimSpace(job.Labels[evalHubJobIDLabel])
	if jobID == "" {
		log.Info("skip EvalHub POST: missing job_id label",
			append(failureWatcherLogFields(), "action", "skip_missing_label", "job", job.Name, "namespace", job.Namespace, "label", evalHubJobIDLabel)...)
		return ctrl.Result{}, nil
	}

	providerID := job.Labels[evalHubProviderIDLabel]
	benchmarkID := job.Labels[evalHubBenchmarkIDLabel]
	if providerID == "" || benchmarkID == "" {
		log.Info("skip EvalHub POST: missing provider_id or benchmark_id label",
			append(failureWatcherLogFields(), "action", "skip_missing_label", "job", job.Name, "namespace", job.Namespace)...)
		return ctrl.Result{}, nil
	}

	benchmarkIndex := benchmarkIndexFromJob(&job)

	if !failurePendingReport(&job) {
		pendingPatch := client.MergeFrom(job.DeepCopy())
		if job.Annotations == nil {
			job.Annotations = map[string]string{}
		}
		job.Annotations[annotationFailurePending] = "true"
		if err := r.Patch(ctx, &job, pendingPatch); err != nil {
			log.Error(err, "failed to annotate job pending EvalHub failure sync",
				append(failureWatcherLogFields(), "action", "patch_pending_failed", "job", job.Name)...)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
	}

	if err := postEvalHubBenchmarkFailed(ctx, r.RESTConfig, baseURL, job.Namespace, jobID, providerID, benchmarkID, benchmarkIndex, msg); err != nil {
		log.Error(err, "failed to post EvalHub benchmark failure event",
			append(failureWatcherLogFields(), "action", "post_events_failed", "job", job.Name, "evalJobID", jobID)...)
		revert := client.MergeFrom(job.DeepCopy())
		delete(job.Annotations, annotationFailurePending)
		if len(job.Annotations) == 0 {
			job.Annotations = nil
		}
		if err2 := r.Patch(ctx, &job, revert); err2 != nil {
			log.Error(err2, "failed to revert failure-pending annotation after POST failure",
				append(failureWatcherLogFields(), "action", "revert_pending_failed", "job", job.Name)...)
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	promotePatch := client.MergeFrom(job.DeepCopy())
	if job.Annotations == nil {
		job.Annotations = map[string]string{}
	}
	delete(job.Annotations, annotationFailurePending)
	job.Annotations[annotationFailureReported] = "true"
	if err := r.Patch(ctx, &job, promotePatch); err != nil {
		log.Error(err, "failed to promote failure-reported annotation after successful POST",
			append(failureWatcherLogFields(), "action", "promote_reported_failed", "job", job.Name)...)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	if err := r.deleteEvalHubFailureSyncedJob(ctx, &job); err != nil {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	log.Info("posted EvalHub benchmark failure event",
		append(failureWatcherLogFields(), "action", "post_events_ok", "evalJobID", jobID, "k8sJob", job.Name, "namespace", job.Namespace)...)
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

func (r *EvalHubEvaluationJobFailureReconciler) detectFailure(ctx context.Context, job *batchv1.Job) (bool, string, error) {
	// Do not use generic JobFailed alone: the adapter may already have POSTed failed to EvalHub.
	// Only synthesize an event when workload containers (init/adapter/sidecar) show pre-callback failures.
	msg, ok, err := r.operatorOnlyFailureFromPods(ctx, job)
	if err != nil {
		return false, "", err
	}
	if ok {
		return true, msg, nil
	}
	return false, "", nil
}

func (r *EvalHubEvaluationJobFailureReconciler) operatorOnlyFailureFromPods(ctx context.Context, job *batchv1.Job) (string, bool, error) {
	log := log.FromContext(ctx)
	list := &corev1.PodList{}
	if err := r.List(ctx, list, client.InNamespace(job.Namespace), client.MatchingLabels{
		"batch.kubernetes.io/job-name": job.Name,
	}); err != nil {
		return "", false, fmt.Errorf("list pods for job %s/%s: %w", job.Namespace, job.Name, err)
	}
	var parts []string
	matchedOwner := 0
	for i := range list.Items {
		pod := &list.Items[i]
		ownedByJob := false
		for _, ref := range pod.OwnerReferences {
			if ref.Kind == "Job" && ref.UID == job.UID {
				ownedByJob = true
				break
			}
		}
		if !ownedByJob {
			continue
		}
		matchedOwner++
		if msg, fatal := podOperatorOnlyFailureMessage(pod); fatal {
			parts = append(parts, fmt.Sprintf("pod %s: %s", pod.Name, msg))
		}
	}
	if len(parts) == 0 && len(list.Items) > 0 && matchedOwner == 0 {
		log.Info("pods match job-name label but none have OwnerReference UID matching Job; cannot read container failure signals",
			append(failureWatcherLogFields(), "action", "pod_owner_uid_mismatch", "job", job.Name, "namespace", job.Namespace, "jobUID", job.UID, "podCount", len(list.Items))...)
	}
	if len(parts) == 0 {
		return "", false, nil
	}
	return strings.Join(parts, "; "), true, nil
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

// Kubernetes ContainerStateWaiting.Reason values: container never ran successfully and EvalHub likely
// was not notified (operator-only path). Strings match kubelet/runtime (stable across releases).
const (
	waitingReasonImagePullBackOff           = "ImagePullBackOff"
	waitingReasonErrImagePull               = "ErrImagePull"
	waitingReasonCrashLoopBackOff           = "CrashLoopBackOff"
	waitingReasonCreateContainerConfigError = "CreateContainerConfigError"
	waitingReasonCreateContainerError       = "CreateContainerError"
	waitingReasonInvalidImageName           = "InvalidImageName"
	waitingReasonRunContainerError          = "RunContainerError"
)

var fatalContainerWaitingReasons = map[string]struct{}{
	waitingReasonImagePullBackOff:           {},
	waitingReasonErrImagePull:               {},
	waitingReasonCrashLoopBackOff:           {},
	waitingReasonCreateContainerConfigError: {},
	waitingReasonCreateContainerError:       {},
	waitingReasonInvalidImageName:           {},
	waitingReasonRunContainerError:          {},
}

func isFatalContainerWaitingReason(reason string) bool {
	_, ok := fatalContainerWaitingReasons[strings.TrimSpace(reason)]
	return ok
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
			if isFatalContainerWaitingReason(w.Reason) {
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
			if isFatalContainerWaitingReason(w.Reason) {
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

func failurePendingReport(job *batchv1.Job) bool {
	if job.Annotations == nil {
		return false
	}
	return job.Annotations[annotationFailurePending] == "true"
}

// deleteEvalHubFailureSyncedJob removes the Batch Job after EvalHub accepted the failure event (pods are GC'd with the Job).
func (r *EvalHubEvaluationJobFailureReconciler) deleteEvalHubFailureSyncedJob(ctx context.Context, job *batchv1.Job) error {
	log := log.FromContext(ctx)
	if err := r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		log.Error(err, "failed to delete evaluation job after EvalHub failure sync",
			append(failureWatcherLogFields(), "action", "delete_job_failed", "job", job.Name, "namespace", job.Namespace)...)
		return err
	}
	log.Info("deleted evaluation job after EvalHub failure sync",
		append(failureWatcherLogFields(), "action", "delete_job_ok", "job", job.Name, "namespace", job.Namespace)...)
	return nil
}

// getEvalHubURLFromJob resolves the EvalHub API base URL from Job labels evalhub_instance_name and evalhub_instance_namespace
// (set by eval-hub k8s runtime). Both labels are required.
func (r *EvalHubEvaluationJobFailureReconciler) getEvalHubURLFromJob(ctx context.Context, job *batchv1.Job) (string, error) {
	var name, namespace string
	if job.Labels != nil {
		name = strings.TrimSpace(job.Labels[evalHubInstanceNameLabel])
		namespace = strings.TrimSpace(job.Labels[evalHubInstanceNamespaceLabel])
	}
	if name != "" && namespace != "" {
		return r.evalHubURLFromCR(ctx, name, namespace)
	}
	if name != "" || namespace != "" {
		return "", fmt.Errorf("evalhub instance labels incomplete: need both %q and %q (got evalhub_instance_name=%q evalhub_instance_namespace=%q)",
			evalHubInstanceNameLabel, evalHubInstanceNamespaceLabel, name, namespace)
	}
	return "", fmt.Errorf("missing required evalhub instance labels %q and %q",
		evalHubInstanceNameLabel, evalHubInstanceNamespaceLabel)
}

func (r *EvalHubEvaluationJobFailureReconciler) evalHubURLFromCR(ctx context.Context, name, namespace string) (string, error) {
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

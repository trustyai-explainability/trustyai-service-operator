/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package evalhub

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	kueue "sigs.k8s.io/kueue/apis/kueue/v1beta1"
)

// kueueWorkloadReasonInadmissible matches Kueue's Workload condition Reason when quota cannot be reserved.
const kueueWorkloadReasonInadmissible = "Inadmissible"

// After a successful POST to EvalHub, the Workload is annotated so we do not report the same
// failure on every reconcile.
const annotationKueueFailedWorkloadEventReported = "trustyai.opendatahub.io/evalhub-kueue-failed-workload-reported"

// evalHubEvaluationFailedKueueWorkloadsControllerName matches ctrl.NewControllerManagedBy(mgr).Named(...).
const evalHubEvaluationFailedKueueWorkloadsControllerName = "evalhub-evaluation-failed-kueue-workloads"

// evaluationFailedKueueWorkloadsLogFields mirrors failureWatcherLogFields for the Job failure reconciler.
func evaluationFailedKueueWorkloadsLogFields() []any {
	return []any{
		"evalhub_watcher", "kueue_failed_workloads",
		"controller", evalHubEvaluationFailedKueueWorkloadsControllerName,
	}
}

//+kubebuilder:rbac:groups=kueue.x-k8s.io,resources=workloads,verbs=get;list;watch;patch
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get
//+kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=evalhubs,verbs=get

// EvalHubEvaluationFailedKueueWorkloadsReconciler POSTs a failed benchmark event to EvalHub when a Kueue
// Workload has QuotaReserved=False with Reason=Inadmissible and is owned by an EvalHub evaluation Job.
type EvalHubEvaluationFailedKueueWorkloadsReconciler struct {
	client.Client
	RESTConfig *rest.Config
	// tenantNS is the same instance updated by EvalHubEvaluationJobFailureReconciler's Namespace watch.
	tenantNS *evalHubTenantNamespaces
}

// registerEvalHubEvaluationFailedKueueWorkloadsReconciler registers a Workload watch; it is invoked from
// EvalHubReconciler.SetupWithManager; tenantNS is the same instance passed to registerEvalHubEvaluationJobFailureController.
func registerEvalHubEvaluationFailedKueueWorkloadsReconciler(mgr manager.Manager, tenantNS *evalHubTenantNamespaces) error {
	if tenantNS == nil {
		return fmt.Errorf("evalhub failed kueue workloads: tenantNS is nil")
	}
	r := &EvalHubEvaluationFailedKueueWorkloadsReconciler{
		Client:     mgr.GetClient(),
		RESTConfig: rest.CopyConfig(mgr.GetConfig()),
		tenantNS:   tenantNS,
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named(evalHubEvaluationFailedKueueWorkloadsControllerName).
		For(&kueue.Workload{}, builder.WithPredicates(workloadFailurePredicate(r))).
		Complete(r)
}

func workloadFailurePredicate(r *EvalHubEvaluationFailedKueueWorkloadsReconciler) predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			wl, ok := e.Object.(*kueue.Workload)
			return ok && workloadEnqueueCandidate(wl, r.tenantNS)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			newWl, ok := e.ObjectNew.(*kueue.Workload)
			return ok && workloadEnqueueCandidate(newWl, r.tenantNS)
		},
		DeleteFunc:  func(event.DeleteEvent) bool { return false },
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}

// workloadEnqueueCandidate mirrors jobUpdatePredicate: tenant namespace, not yet reported, has Job owner,
// and QuotaReserved=False with Reason=Inadmissible (including informer replay on Create).
func workloadEnqueueCandidate(wl *kueue.Workload, tenantNS *evalHubTenantNamespaces) bool {
	if wl == nil || tenantNS == nil || !tenantNS.IsTenant(wl.Namespace) {
		return false
	}
	if workloadFailedEventAlreadyReported(wl) {
		return false
	}
	if _, _, hasJob := jobOwnerFromWorkload(wl); !hasJob {
		return false
	}
	_, has := workloadQuotaReservedInadmissibleCondition(wl)
	return has
}

func workloadFailedEventAlreadyReported(wl *kueue.Workload) bool {
	if wl.Annotations == nil {
		return false
	}
	return wl.Annotations[annotationKueueFailedWorkloadEventReported] == "true"
}

// workloadQuotaReservedInadmissibleCondition returns a workload condition matching Kueue admission failure:
// Type QuotaReserved, Status False, Reason Inadmissible.
func workloadQuotaReservedInadmissibleCondition(wl *kueue.Workload) (*metav1.Condition, bool) {
	for i := range wl.Status.Conditions {
		c := &wl.Status.Conditions[i]
		if c.Type == kueue.WorkloadQuotaReserved &&
			c.Status == metav1.ConditionFalse &&
			c.Reason == kueueWorkloadReasonInadmissible {
			return c, true
		}
	}
	return nil, false
}

// jobOwnerFromWorkload returns the batch/v1 Job owner reference:
//
//	.metadata.ownerReferences[] | select(.kind=="Job")
func jobOwnerFromWorkload(wl *kueue.Workload) (name string, uid types.UID, ok bool) {
	for _, ref := range wl.OwnerReferences {
		if ref.Kind != "Job" {
			continue
		}
		if ref.APIVersion == batchv1.SchemeGroupVersion.String() || ref.APIVersion == "batch/v1" {
			return ref.Name, ref.UID, true
		}
	}
	return "", "", false
}

func (r *EvalHubEvaluationFailedKueueWorkloadsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("reconcile start",
		append(evaluationFailedKueueWorkloadsLogFields(), "action", "reconcile", "namespace", req.Namespace, "name", req.Name)...)

	if r.tenantNS == nil {
		return ctrl.Result{}, fmt.Errorf("tenantNS is nil")
	}

	var wl kueue.Workload
	if err := r.Get(ctx, req.NamespacedName, &wl); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if workloadFailedEventAlreadyReported(&wl) {
		return ctrl.Result{}, nil
	}

	cond, ok := workloadQuotaReservedInadmissibleCondition(&wl)
	if !ok {
		return ctrl.Result{}, nil
	}

	jobName, jobUID, hasJobOwner := jobOwnerFromWorkload(&wl)
	if !hasJobOwner {
		log.V(1).Info("skip workload: no batch/v1 Job ownerReference",
			append(evaluationFailedKueueWorkloadsLogFields(), "action", "skip_no_job_owner",
				"workload", client.ObjectKeyFromObject(&wl), "queue", wl.Spec.QueueName)...)
		return ctrl.Result{}, nil
	}

	var job batchv1.Job
	jobKey := types.NamespacedName{Namespace: wl.Namespace, Name: jobName}
	if err := r.Get(ctx, jobKey, &job); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get owning Job %s/%s: %w", wl.Namespace, jobName, err)
	}
	if job.UID != jobUID {
		log.V(1).Info("skip workload: Job UID does not match ownerReference",
			append(evaluationFailedKueueWorkloadsLogFields(), "action", "skip_job_uid_mismatch",
				"namespace", req.Namespace, "workload", req.Name, "job", jobName)...)
		return ctrl.Result{}, nil
	}

	if !isEvalHubEvaluationJob(&job) {
		log.V(1).Info("skip workload: owning Job is not an EvalHub evaluation job",
			append(evaluationFailedKueueWorkloadsLogFields(), "action", "skip_not_evalhub_job",
				"namespace", req.Namespace, "workload", req.Name, "job", job.Name)...)
		return ctrl.Result{}, nil
	}

	if !r.tenantNS.IsTenant(job.Namespace) {
		return ctrl.Result{}, nil
	}

	if failureAlreadyReported(&job) {
		return ctrl.Result{}, nil
	}

	jobID := strings.TrimSpace(job.Labels[evalHubJobIDLabel])
	if jobID == "" {
		log.V(1).Info("skip EvalHub POST: missing job_id label on Job",
			append(evaluationFailedKueueWorkloadsLogFields(), "action", "skip_missing_label",
				"workload", wl.Name, "job", job.Name, "namespace", job.Namespace, "label", evalHubJobIDLabel)...)
		return ctrl.Result{}, nil
	}

	providerID := job.Labels[evalHubProviderIDLabel]
	benchmarkID := job.Labels[evalHubBenchmarkIDLabel]
	if providerID == "" || benchmarkID == "" {
		log.V(1).Info("skip EvalHub POST: missing provider_id or benchmark_id label on Job",
			append(evaluationFailedKueueWorkloadsLogFields(), "action", "skip_missing_label",
				"workload", wl.Name, "job", job.Name, "namespace", job.Namespace)...)
		return ctrl.Result{}, nil
	}

	benchmarkIndex := benchmarkIndexFromJob(&job)

	baseURL, err := evalHubBaseURLFromJob(ctx, r.Client, &job)
	if err != nil {
		if errors.Is(err, ErrMissingEvalHubLabels) {
			log.V(1).Info("cannot resolve EvalHub URL from Job",
				append(evaluationFailedKueueWorkloadsLogFields(), "action", "skip_no_evalhub_url",
					"workload", wl.Name, "job", job.Name, "namespace", job.Namespace, "error", err.Error())...)
			return ctrl.Result{}, nil
		}
		log.V(1).Info("cannot resolve EvalHub URL from Job",
			append(evaluationFailedKueueWorkloadsLogFields(), "action", "skip_no_evalhub_url",
				"workload", wl.Name, "job", job.Name, "namespace", job.Namespace, "error", err.Error())...)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	msg := strings.TrimSpace(cond.Message)
	if msg == "" {
		msg = fmt.Sprintf("Kueue workload (conditionType=%s, reason=%s)", cond.Type, cond.Reason)
	}

	if err := postEvalHubBenchmarkFailed(ctx, r.RESTConfig, baseURL, job.Namespace, jobID, providerID, benchmarkID, benchmarkIndex, msg, messageCodeQueueError); err != nil {
		log.Error(err, "failed to post EvalHub benchmark failure event for Kueue workload",
			append(evaluationFailedKueueWorkloadsLogFields(), "action", "post_events_failed",
				"workload", wl.Name, "workloadNamespace", wl.Namespace, "queue", wl.Spec.QueueName,
				"job", job.Name, "evalJobID", jobID)...)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	if err := r.annotateWorkloadReported(ctx, &wl); err != nil {
		log.Error(err, "patch workload after EvalHub failed-workload event",
			append(evaluationFailedKueueWorkloadsLogFields(), "action", "patch_workload_failed",
				"workload", client.ObjectKeyFromObject(&wl))...)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	log.Info("posted EvalHub failed status for Kueue workload (via owning Job)",
		append(evaluationFailedKueueWorkloadsLogFields(), "action", "post_events_ok",
			"workload", wl.Name, "workloadNamespace", wl.Namespace, "queue", wl.Spec.QueueName,
			"job", job.Name, "jobUid", string(job.UID),
			"evalJobID", jobID, "providerID", providerID, "benchmarkID", benchmarkID,
			"conditionType", cond.Type, "conditionReason", cond.Reason)...)

	return ctrl.Result{}, nil
}

func (r *EvalHubEvaluationFailedKueueWorkloadsReconciler) annotateWorkloadReported(ctx context.Context, wl *kueue.Workload) error {
	patchBase := wl.DeepCopy()
	if wl.Annotations == nil {
		wl.Annotations = map[string]string{}
	}
	wl.Annotations[annotationKueueFailedWorkloadEventReported] = "true"
	return r.Patch(ctx, wl, client.MergeFrom(patchBase))
}

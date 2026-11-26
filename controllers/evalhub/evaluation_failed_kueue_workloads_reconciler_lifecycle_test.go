/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package evalhub

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	evalhubv1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	kueue "sigs.k8s.io/kueue/apis/kueue/v1beta1"
)

// kueueLifecycleScheme builds the minimal scheme for Kueue failure reconciler lifecycle tests.
func kueueLifecycleScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	sc := runtime.NewScheme()
	require.NoError(t, batchv1.AddToScheme(sc))
	require.NoError(t, corev1.AddToScheme(sc))
	require.NoError(t, evalhubv1.AddToScheme(sc))
	require.NoError(t, kueue.AddToScheme(sc))
	return sc
}

// inadmissibleWorkload builds a Kueue Workload with QuotaReserved=False/Inadmissible owned by a Job.
func inadmissibleWorkload(name, ns, jobName string, jobUID types.UID, condMsg string) *kueue.Workload {
	return &kueue.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			UID:       types.UID("wl-uid-" + name),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "batch/v1", Kind: "Job", Name: jobName, UID: jobUID,
			}},
		},
		Spec: kueue.WorkloadSpec{
			QueueName: "default-queue",
		},
		Status: kueue.WorkloadStatus{
			Conditions: []metav1.Condition{{
				Type:    kueue.WorkloadQuotaReserved,
				Status:  metav1.ConditionFalse,
				Reason:  kueueWorkloadReasonInadmissible,
				Message: condMsg,
			}},
		},
	}
}

// buildKueueReconciler returns a reconciler with a FakeRecorder and the given tenant namespace.
func buildKueueReconciler(fc client.Client, tenantNamespace string) (*EvalHubEvaluationFailedKueueWorkloadsReconciler, *record.FakeRecorder) {
	rec := record.NewFakeRecorder(10)
	tn := newEvalHubTenantNamespaces()
	tn.Add(tenantNamespace)
	return &EvalHubEvaluationFailedKueueWorkloadsReconciler{
		Client:        fc,
		RESTConfig:    &rest.Config{},
		EventRecorder: rec,
		tenantNS:      tn,
	}, rec
}

// TestKueueReconciler_Eviction_EmitsEventAndPatchesJobAndWorkload verifies the full lifecycle for a Kueue
// admission failure: the reconciler POSTs to EvalHub, emits an EvaluationFailed warning event on the Job,
// stamps evaluation-phase=Failed on the Job, and annotates the Workload as reported.
func TestKueueReconciler_Eviction_EmitsEventAndPatchesJobAndWorkload(t *testing.T) {
	srv, requests := capturingEvalHubServer(t)
	sc := kueueLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-kueue", ns, map[string]string{
		evalHubInstanceNameLabel:      "evalhub-1",
		evalHubInstanceNamespaceLabel: "control-ns",
	})
	wl := inadmissibleWorkload("wl-1", ns, job.Name, job.UID, "insufficient quota")
	eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)

	var patchedJobLabels map[string]string
	var workloadAnnotated bool

	fc := fake.NewClientBuilder().
		WithScheme(sc).
		WithObjects(eh, job, wl).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				switch o := obj.(type) {
				case *batchv1.Job:
					if o.Labels[labelEvaluationPhase] == labelEvaluationPhaseFailed {
						patchedJobLabels = o.Labels
					}
				case *kueue.Workload:
					if o.Annotations[annotationKueueFailedWorkloadEventReported] == "true" {
						workloadAnnotated = true
					}
				}
				return c.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()

	r, rec := buildKueueReconciler(fc, ns)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: wl.Name},
	})
	require.NoError(t, err)

	select {
	case payload := <-requests:
		require.NotNil(t, payload.BenchmarkStatusEvent)
		assert.Equal(t, "inspect/docvqa", payload.BenchmarkStatusEvent.ID)
	default:
		t.Fatal("expected EvalHub status event POST but none was captured")
	}

	select {
	case ev := <-rec.Events:
		assert.Contains(t, ev, corev1.EventTypeWarning)
		assert.Contains(t, ev, eventReasonEvaluationFailed)
	default:
		t.Fatal("expected EvaluationFailed event but recorder is empty")
	}

	require.NotNil(t, patchedJobLabels, "evaluation-phase=Failed patch was never applied to Job")
	assert.Equal(t, labelEvaluationPhaseFailed, patchedJobLabels[labelEvaluationPhase])
	assert.True(t, workloadAnnotated, "Workload should be annotated as reported")
}

func TestKueueReconciler_ResolvesBenchmarkID(t *testing.T) {
	for _, tc := range []struct {
		name       string
		annotation string
		label      string
		expected   string
	}{
		{"canonical annotation", "lcb:codegeneration_v6", "lcb-codegeneration_v6", "lcb:codegeneration_v6"},
		{"legacy label", "", "arc_easy", "arc_easy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, requests := capturingEvalHubServer(t)
			job := evalHubEvaluationJob("eval-job-kueue-id", "tenant-ns", map[string]string{
				evalHubInstanceNameLabel: "evalhub-1", evalHubInstanceNamespaceLabel: "control-ns",
				evalHubBenchmarkIDLabel: tc.label,
			})
			if tc.annotation == "" {
				delete(job.Annotations, evalHubBenchmarkIDAnnotation)
			} else {
				job.Annotations[evalHubBenchmarkIDAnnotation] = tc.annotation
			}
			wl := inadmissibleWorkload("wl-kueue-id", job.Namespace, job.Name, job.UID, "insufficient quota")
			eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)
			fc := fake.NewClientBuilder().WithScheme(kueueLifecycleScheme(t)).WithObjects(eh, job, wl).Build()
			r, _ := buildKueueReconciler(fc, job.Namespace)
			result, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(wl)})
			require.NoError(t, err)
			require.Equal(t, ctrl.Result{}, result)
			require.Len(t, requests, 1)
			event := (<-requests).BenchmarkStatusEvent
			require.NotNil(t, event)
			assert.Equal(t, tc.expected, event.ID)
			assert.Empty(t, event.Precondition)
		})
	}
}

func TestKueueReconciler_AlreadyRecordedFailure_PromotesWithoutRepost(t *testing.T) {
	srv, requests := evalHubJobStateServer(t, `{"message":"The job can not be failed because it is 'failed'."}`, "failed")
	job := evalHubEvaluationJob("eval-job-kueue-recorded", "tenant-ns", map[string]string{
		evalHubInstanceNameLabel: "evalhub-1", evalHubInstanceNamespaceLabel: "control-ns",
	})
	wl := inadmissibleWorkload("wl-recorded", job.Namespace, job.Name, job.UID, "insufficient quota")
	eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)
	fc := fake.NewClientBuilder().WithScheme(kueueLifecycleScheme(t)).WithObjects(eh, job, wl).Build()
	r, rec := buildKueueReconciler(fc, job.Namespace)
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(wl)}
	result, err := r.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
	var persistedJob batchv1.Job
	require.NoError(t, fc.Get(context.Background(), client.ObjectKeyFromObject(job), &persistedJob))
	assert.Equal(t, labelEvaluationPhaseFailed, persistedJob.Labels[labelEvaluationPhase])
	var persistedWorkload kueue.Workload
	require.NoError(t, fc.Get(context.Background(), client.ObjectKeyFromObject(wl), &persistedWorkload))
	assert.Equal(t, "true", persistedWorkload.Annotations[annotationKueueFailedWorkloadEventReported])
	select {
	case ev := <-rec.Events:
		assert.Contains(t, ev, eventReasonEvaluationFailed)
	default:
		t.Fatal("expected failure event")
	}
	require.Len(t, requests, 1)
	<-requests
	result, err = r.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
	assert.Empty(t, requests, "the recorded failure must not be posted again")
	assert.Empty(t, rec.Events, "the failure event must not be emitted again")
}

func TestKueueReconciler_TerminalConflictOutcomes(t *testing.T) {
	for _, state := range []string{"cancelled", "cancelled-without-benchmark", "completed", "", "running"} {
		t.Run(state, func(t *testing.T) {
			srv, requests := evalHubJobStateServer(t, `{"message":"The job can not be failed because it is 'cancelled'."}`, state)
			job := evalHubEvaluationJob("eval-job-kueue-superseded", "tenant-ns", map[string]string{
				evalHubInstanceNameLabel: "evalhub-1", evalHubInstanceNamespaceLabel: "control-ns",
			})
			wl := inadmissibleWorkload("wl-superseded", job.Namespace, job.Name, job.UID, "insufficient quota")
			eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)
			fc := fake.NewClientBuilder().WithScheme(kueueLifecycleScheme(t)).WithObjects(eh, job, wl).Build()
			r, rec := buildKueueReconciler(fc, job.Namespace)
			result, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(wl)})
			require.NoError(t, err)
			if state == "" || state == "running" {
				assert.Equal(t, 30*time.Second, result.RequeueAfter, "unknown outcomes must remain retryable")
			} else {
				assert.Equal(t, ctrl.Result{}, result, "superseded reports must stop requeueing")
			}
			require.Len(t, requests, 2)
			assert.Equal(t, http.MethodPost, (<-requests).Method)
			assert.Equal(t, http.MethodGet, (<-requests).Method)
			var persisted batchv1.Job
			require.NoError(t, fc.Get(context.Background(), client.ObjectKeyFromObject(job), &persisted))
			assert.NotEqual(t, labelEvaluationPhaseFailed, persisted.Labels[labelEvaluationPhase])
			var persistedWorkload kueue.Workload
			require.NoError(t, fc.Get(context.Background(), client.ObjectKeyFromObject(wl), &persistedWorkload))
			assert.Empty(t, persistedWorkload.Annotations[annotationKueueFailedWorkloadEventReported])
			assert.Empty(t, rec.Events)
		})
	}
}

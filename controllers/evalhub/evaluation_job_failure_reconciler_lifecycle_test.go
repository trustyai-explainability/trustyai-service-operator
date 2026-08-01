/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package evalhub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	evalhubv1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// jobFailureLifecycleScheme builds the minimal scheme for job failure reconciler lifecycle tests.
func jobFailureLifecycleScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	sc := runtime.NewScheme()
	require.NoError(t, batchv1.AddToScheme(sc))
	require.NoError(t, corev1.AddToScheme(sc))
	require.NoError(t, evalhubv1.AddToScheme(sc))
	return sc
}

// noopEvalHubServer starts a local HTTP server that accepts any request and returns 204.
func noopEvalHubServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// readyEvalHubCR returns a minimal EvalHub CR that IsReady() and points at the given URL.
func readyEvalHubCR(name, ns, url string) *evalhubv1.EvalHub {
	return &evalhubv1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Status: evalhubv1.EvalHubStatus{
			URL:   url,
			Ready: corev1.ConditionTrue,
		},
	}
}

// evalHubEvaluationJob builds a failed Job with the mandatory EvalHub labels.
// extra is merged into the label map (use it to add instance/phase labels per test).
func evalHubEvaluationJob(name, ns string, extra map[string]string) *batchv1.Job {
	labels := map[string]string{
		evalHubAppLabel:       evalHubAppValue,
		evalHubComponentLabel: evalHubComponentValue,
		evalHubJobIDLabel:     "jid-" + name,
		evalHubProviderIDLabel: "provider-1",
		evalHubBenchmarkIDLabel: "bench-1",
	}
	for k, v := range extra {
		labels[k] = v
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			UID:       types.UID("uid-" + name),
			Labels:    labels,
		},
		Status: batchv1.JobStatus{
			Conditions: []batchv1.JobCondition{{
				Type: batchv1.JobFailed, Status: corev1.ConditionTrue,
			}},
		},
	}
}

// oomKilledAdapterPod builds a Pod owned by the given Job where the adapter container is OOMKilled.
func oomKilledAdapterPod(jobName, ns string, jobUID types.UID) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName + "-pod",
			Namespace: ns,
			Labels:    map[string]string{"batch.kubernetes.io/job-name": jobName},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "batch/v1", Kind: "Job", Name: jobName, UID: jobUID,
			}},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: adapterContainerName,
				State: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137},
				},
			}},
		},
	}
}

// errImagePullInitPod builds a Pod owned by the given Job where the init container has ErrImagePull.
func errImagePullInitPod(jobName, ns string, jobUID types.UID) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName + "-pod",
			Namespace: ns,
			Labels:    map[string]string{"batch.kubernetes.io/job-name": jobName},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "batch/v1", Kind: "Job", Name: jobName, UID: jobUID,
			}},
		},
		Status: corev1.PodStatus{
			InitContainerStatuses: []corev1.ContainerStatus{{
				Name: initContainerName,
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{Reason: waitingReasonErrImagePull, Message: "pull failed"},
				},
			}},
		},
	}
}

// buildJobFailureReconciler returns a reconciler with a FakeRecorder and the given tenant namespace.
func buildJobFailureReconciler(fc client.Client, tenantNamespace string) (*EvalHubEvaluationJobFailureReconciler, *record.FakeRecorder) {
	rec := record.NewFakeRecorder(10)
	tn := newEvalHubTenantNamespaces()
	tn.Add(tenantNamespace)
	return &EvalHubEvaluationJobFailureReconciler{
		Client:        fc,
		RESTConfig:    &rest.Config{},
		EventRecorder: rec,
		tenantNS:      tn,
	}, rec
}

// TestJobFailureReconciler_ServerAlreadyHandled_NoEvent verifies that when the EvalHub server has
// already set evaluation-phase=Failed on the Job, the operator emits no duplicate event.
func TestJobFailureReconciler_ServerAlreadyHandled_NoEvent(t *testing.T) {
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-dedup", ns, map[string]string{
		labelEvaluationPhase: labelEvaluationPhaseFailed,
	})

	fc := fake.NewClientBuilder().WithScheme(sc).WithObjects(job).Build()
	r, rec := buildJobFailureReconciler(fc, ns)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: job.Name},
	})
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	select {
	case ev := <-rec.Events:
		t.Fatalf("expected no event after dedup (server already handled), got: %s", ev)
	default:
	}
}

// TestJobFailureReconciler_OOMKill_EmitsEventAndPatchesJob verifies the full lifecycle for an OOM-killed
// adapter container: the reconciler emits an EvaluationFailed event and stamps evaluation-phase=Failed.
func TestJobFailureReconciler_OOMKill_EmitsEventAndPatchesJob(t *testing.T) {
	srv := noopEvalHubServer(t)
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-oom", ns, map[string]string{
		evalHubInstanceNameLabel:      "evalhub-1",
		evalHubInstanceNamespaceLabel: "control-ns",
	})
	pod := oomKilledAdapterPod(job.Name, ns, job.UID)
	eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)

	var patchedLabels map[string]string
	var patchedAnnotations map[string]string

	fc := fake.NewClientBuilder().
		WithScheme(sc).
		WithObjects(eh, job, pod).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				if j, ok := obj.(*batchv1.Job); ok && j.Labels[labelEvaluationPhase] == labelEvaluationPhaseFailed {
					patchedLabels = j.Labels
					patchedAnnotations = j.Annotations
				}
				return c.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()

	r, rec := buildJobFailureReconciler(fc, ns)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: job.Name},
	})
	require.NoError(t, err)

	select {
	case ev := <-rec.Events:
		assert.Contains(t, ev, corev1.EventTypeWarning)
		assert.Contains(t, ev, eventReasonEvaluationFailed)
		assert.Contains(t, ev, "OOMKilled")
	default:
		t.Fatal("expected EvaluationFailed event but recorder is empty")
	}

	require.NotNil(t, patchedLabels, "evaluation-phase=Failed patch was never applied to Job")
	assert.Equal(t, labelEvaluationPhaseFailed, patchedLabels[labelEvaluationPhase])
	assert.NotEmpty(t, patchedAnnotations[annotationEvaluationStatus])

	// After a successful sync the job is deleted.
	err = fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: job.Name}, &batchv1.Job{})
	assert.True(t, apierrors.IsNotFound(err), "job should be deleted after successful failure sync")
}

// TestJobFailureReconciler_ImagePullError_EmitsEventAndPatchesJob verifies the full lifecycle for an init
// container stuck on ErrImagePull: the reconciler emits an EvaluationFailed event and stamps the Job.
func TestJobFailureReconciler_ImagePullError_EmitsEventAndPatchesJob(t *testing.T) {
	srv := noopEvalHubServer(t)
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-imagepull", ns, map[string]string{
		evalHubInstanceNameLabel:      "evalhub-1",
		evalHubInstanceNamespaceLabel: "control-ns",
	})
	pod := errImagePullInitPod(job.Name, ns, job.UID)
	eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)

	var patchedLabels map[string]string

	fc := fake.NewClientBuilder().
		WithScheme(sc).
		WithObjects(eh, job, pod).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				if j, ok := obj.(*batchv1.Job); ok && j.Labels[labelEvaluationPhase] == labelEvaluationPhaseFailed {
					patchedLabels = j.Labels
				}
				return c.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()

	r, rec := buildJobFailureReconciler(fc, ns)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: job.Name},
	})
	require.NoError(t, err)

	select {
	case ev := <-rec.Events:
		assert.Contains(t, ev, corev1.EventTypeWarning)
		assert.Contains(t, ev, eventReasonEvaluationFailed)
		assert.Contains(t, ev, waitingReasonErrImagePull)
	default:
		t.Fatal("expected EvaluationFailed event but recorder is empty")
	}

	require.NotNil(t, patchedLabels, "evaluation-phase=Failed patch was never applied to Job")
	assert.Equal(t, labelEvaluationPhaseFailed, patchedLabels[labelEvaluationPhase])
}

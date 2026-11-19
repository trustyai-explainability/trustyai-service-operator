/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package evalhub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

// capturingEvalHubServer records EvalHub status event requests and returns 204.
func capturingEvalHubServer(t *testing.T) (*httptest.Server, <-chan statusEventPayload) {
	t.Helper()
	requests := make(chan statusEventPayload, 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload statusEventPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		requests <- payload
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return srv, requests
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
		evalHubAppLabel:         evalHubAppValue,
		evalHubComponentLabel:   evalHubComponentValue,
		evalHubJobIDLabel:       "jid-" + name,
		evalHubProviderIDLabel:  "provider-1",
		evalHubBenchmarkIDLabel: "inspect-docvqa",
	}
	for k, v := range extra {
		labels[k] = v
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   ns,
			UID:         types.UID("uid-" + name),
			Labels:      labels,
			Annotations: map[string]string{evalHubBenchmarkIDAnnotation: "inspect/docvqa"},
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

// imagePullBackOffAdapterPod builds a Pod owned by the given Job where the adapter
// is waiting in ImagePullBackOff.
func imagePullBackOffAdapterPod(jobName, ns string, jobUID types.UID) *corev1.Pod {
	const adapterImage = "registry.example.com/evalhub/adapter:latest"
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              jobName + "-pod",
			Namespace:         ns,
			UID:               types.UID("pod-" + jobName),
			Labels:            map[string]string{"batch.kubernetes.io/job-name": jobName},
			CreationTimestamp: metav1.NewTime(time.Now().Add(-30 * time.Minute)),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "batch/v1", Kind: "Job", Name: jobName, UID: jobUID,
			}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			Conditions: []corev1.PodCondition{{
				Type:               corev1.PodScheduled,
				Status:             corev1.ConditionTrue,
				LastTransitionTime: metav1.NewTime(time.Now().Add(-30 * time.Minute)),
			}},
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:  adapterContainerName,
				Image: adapterImage,
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{Reason: waitingReasonImagePullBackOff, Message: "back-off pulling image"},
				},
			}},
		},
	}
}

func imagePullBackOffAdapterAndSidecarPod(jobName, ns string, jobUID types.UID) *corev1.Pod {
	pod := imagePullBackOffAdapterPod(jobName, ns, jobUID)
	pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, corev1.ContainerStatus{
		Name:  sidecarContainerName,
		Image: "registry.example.com/evalhub/sidecar:latest",
		State: corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{Reason: waitingReasonImagePullBackOff, Message: "back-off pulling image"},
		},
	})
	return pod
}

func imagePullBackOffEvent(pod *corev1.Pod, ago time.Duration) *corev1.Event {
	return imagePullBackOffEventForImageAndContainer(pod, ago, "registry.example.com/evalhub/adapter:latest", adapterContainerName)
}

func imagePullBackOffEventForImage(pod *corev1.Pod, ago time.Duration, image string) *corev1.Event {
	return imagePullBackOffEventForImageAndContainer(pod, ago, image, adapterContainerName)
}

func imagePullBackOffEventForImageAndContainer(pod *corev1.Pod, ago time.Duration, image, container string) *corev1.Event {
	t := metav1.NewTime(time.Now().Add(-ago))
	safeImage := strings.NewReplacer("/", "-", ":", "-", ".", "-").Replace(image)
	fieldPath := fmt.Sprintf("spec.containers{%s}", container)
	if container != adapterContainerName {
		fieldPath = fmt.Sprintf("spec.initContainers{%s}", container)
	}
	return &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name + "." + safeImage + ".imagepullbackoff",
			Namespace: pod.Namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:      "Pod",
			Namespace: pod.Namespace,
			Name:      pod.Name,
			UID:       pod.UID,
			FieldPath: fieldPath,
		},
		Reason:         eventReasonBackOff,
		Message:        fmt.Sprintf("Back-off pulling image %q", image),
		FirstTimestamp: t,
		LastTimestamp:  t,
	}
}

// buildJobFailureReconciler returns a reconciler with a FakeRecorder and the given tenant namespace.
func buildJobFailureReconciler(fc client.Client, tenantNamespace string) (*EvalHubEvaluationJobFailureReconciler, *record.FakeRecorder) {
	rec := record.NewFakeRecorder(10)
	tn := newEvalHubTenantNamespaces()
	tn.Add(tenantNamespace)
	return &EvalHubEvaluationJobFailureReconciler{
		Client:        fc,
		APIReader:     fc,
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

	// Retain the Job when the server already marked failure (do not delete after skip).
	err = fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: job.Name}, &batchv1.Job{})
	assert.NoError(t, err, "job should be retained when evaluation-phase=Failed was already set by the server")
}

// TestJobFailureReconciler_FailureAlreadyReported_RetainsJob verifies that when the operator has
// already posted failure (evalhub-failure-reported annotation), reconcile is a no-op that keeps the Job.
func TestJobFailureReconciler_FailureAlreadyReported_RetainsJob(t *testing.T) {
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-already-reported", ns, nil)
	job.Annotations = map[string]string{
		annotationFailureReported: "true",
	}

	fc := fake.NewClientBuilder().WithScheme(sc).WithObjects(job).Build()
	r, rec := buildJobFailureReconciler(fc, ns)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: job.Name},
	})
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	select {
	case ev := <-rec.Events:
		t.Fatalf("expected no event when failure already reported, got: %s", ev)
	default:
	}

	err = fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: job.Name}, &batchv1.Job{})
	assert.NoError(t, err, "job should be retained when failure-reported annotation is already set")
}

// TestJobFailureReconciler_OOMKill_EmitsEventAndPatchesJob verifies the full lifecycle for an OOM-killed
// adapter container: the reconciler emits an EvaluationFailed event and stamps evaluation-phase=Failed.
func TestJobFailureReconciler_OOMKill_EmitsEventAndPatchesJob(t *testing.T) {
	srv, requests := capturingEvalHubServer(t)
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
		assert.Contains(t, ev, "OOMKilled")
	default:
		t.Fatal("expected EvaluationFailed event but recorder is empty")
	}

	require.NotNil(t, patchedLabels, "evaluation-phase=Failed patch was never applied to Job")
	assert.Equal(t, labelEvaluationPhaseFailed, patchedLabels[labelEvaluationPhase])
	assert.NotEmpty(t, patchedAnnotations[annotationEvaluationStatus])

	// Retain the Job so pod logs remain available until ttlSecondsAfterFinished expires.
	err = fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: job.Name}, &batchv1.Job{})
	assert.NoError(t, err, "job should be retained after successful failure sync")
}

// TestJobFailureReconciler_MissingBenchmarkAnnotation_SkipsPost verifies that the operator does not
// fall back to the sanitized benchmark_id label when the original EvalHub benchmark ID annotation is missing.
func TestJobFailureReconciler_MissingBenchmarkAnnotation_SkipsPost(t *testing.T) {
	srv, requests := capturingEvalHubServer(t)
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-missing-benchmark-annotation", ns, map[string]string{
		evalHubInstanceNameLabel:      "evalhub-1",
		evalHubInstanceNamespaceLabel: "control-ns",
	})
	job.Annotations = nil
	pod := oomKilledAdapterPod(job.Name, ns, job.UID)
	eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)

	fc := fake.NewClientBuilder().WithScheme(sc).WithObjects(eh, job, pod).Build()
	r, rec := buildJobFailureReconciler(fc, ns)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: job.Name},
	})
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	select {
	case payload := <-requests:
		t.Fatalf("expected no EvalHub status event POST without benchmark annotation, got: %#v", payload)
	default:
	}

	select {
	case ev := <-rec.Events:
		t.Fatalf("expected no Kubernetes event without benchmark annotation, got: %s", ev)
	default:
	}

	var persisted batchv1.Job
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: job.Name}, &persisted))
	assert.Empty(t, persisted.Labels[labelEvaluationPhase])
	assert.Empty(t, persisted.Annotations[annotationFailureReported])
}

// TestJobFailureReconciler_ErrImagePull_DoesNotEmitEvent verifies a transient
// ErrImagePull does not immediately fail the EvalHub benchmark.
func TestJobFailureReconciler_ErrImagePull_DoesNotEmitEvent(t *testing.T) {
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-errimagepull", ns, map[string]string{
		evalHubInstanceNameLabel:      "evalhub-1",
		evalHubInstanceNamespaceLabel: "control-ns",
	})
	pod := errImagePullInitPod(job.Name, ns, job.UID)
	srv := noopEvalHubServer(t)
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
		t.Fatalf("expected no EvaluationFailed event for transient ErrImagePull, got: %s", ev)
	default:
	}

	assert.Nil(t, patchedLabels, "evaluation-phase=Failed should not be patched for transient ErrImagePull")
}

// TestJobFailureReconciler_ImagePullBackOffInGrace_Requeues verifies a recent image-pull
// BackOff event requeues instead of reporting failure immediately, even when the pod is old.
func TestJobFailureReconciler_ImagePullBackOffInGrace_Requeues(t *testing.T) {
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-imagepullbackoff-grace", ns, map[string]string{
		evalHubInstanceNameLabel:      "evalhub-1",
		evalHubInstanceNamespaceLabel: "control-ns",
	})
	pod := imagePullBackOffAdapterPod(job.Name, ns, job.UID)
	backOffEvent := imagePullBackOffEvent(pod, 30*time.Second)
	srv := noopEvalHubServer(t)
	eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)

	var patchedLabels map[string]string

	fc := fake.NewClientBuilder().
		WithScheme(sc).
		WithStatusSubresource(&corev1.Pod{}).
		WithObjects(eh, job, pod, backOffEvent).
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

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: job.Name},
	})
	require.NoError(t, err)
	assert.Greater(t, result.RequeueAfter, time.Duration(0))
	assert.LessOrEqual(t, result.RequeueAfter, imagePullBackOffGracePeriod)

	select {
	case ev := <-rec.Events:
		t.Fatalf("expected no EvaluationFailed event during ImagePullBackOff grace period, got: %s", ev)
	default:
	}

	assert.Nil(t, patchedLabels, "evaluation-phase=Failed should not be patched during ImagePullBackOff grace period")
}

// TestJobFailureReconciler_ImagePullBackOffWithoutEvent_Requeues verifies a pod in
// ImagePullBackOff still gets a positive recheck when no matching BackOff event is visible yet.
func TestJobFailureReconciler_ImagePullBackOffWithoutEvent_Requeues(t *testing.T) {
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-imagepullbackoff-no-event", ns, map[string]string{
		evalHubInstanceNameLabel:      "evalhub-1",
		evalHubInstanceNamespaceLabel: "control-ns",
	})
	pod := imagePullBackOffAdapterPod(job.Name, ns, job.UID)
	srv := noopEvalHubServer(t)
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

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: job.Name},
	})
	require.NoError(t, err)
	assert.Greater(t, result.RequeueAfter, time.Duration(0))

	select {
	case ev := <-rec.Events:
		t.Fatalf("expected no EvaluationFailed event before a matching image-pull BackOff event is visible, got: %s", ev)
	default:
	}

	assert.Nil(t, patchedLabels, "evaluation-phase=Failed should not be patched without a matching image-pull BackOff event")

	var persisted batchv1.Job
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: job.Name}, &persisted))
	observations := imagePullBackOffObservations(&persisted)
	require.Len(t, observations, 1)

	for key := range observations {
		observations[key] = time.Now().Add(-11 * time.Minute)
	}
	raw, err := marshalImagePullBackOffObservations(observations)
	require.NoError(t, err)
	persisted.Annotations[annotationImagePullBackOffObservations] = raw
	require.NoError(t, fc.Update(context.Background(), &persisted))

	_, err = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: job.Name},
	})
	require.NoError(t, err)

	select {
	case ev := <-rec.Events:
		assert.Contains(t, ev, corev1.EventTypeWarning)
		assert.Contains(t, ev, eventReasonEvaluationFailed)
		assert.Contains(t, ev, waitingReasonImagePullBackOff)
	default:
		t.Fatal("expected EvaluationFailed event after fallback observation passes grace")
	}

	require.NotNil(t, patchedLabels, "evaluation-phase=Failed patch was never applied after fallback observation passed grace")
	assert.Equal(t, labelEvaluationPhaseFailed, patchedLabels[labelEvaluationPhase])
}

func TestJobFailureReconciler_ImagePullBackOffWithoutEvent_TracksMultipleContainers(t *testing.T) {
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-imagepullbackoff-multi", ns, map[string]string{
		evalHubInstanceNameLabel:      "evalhub-1",
		evalHubInstanceNamespaceLabel: "control-ns",
	})
	pod := imagePullBackOffAdapterAndSidecarPod(job.Name, ns, job.UID)
	srv := noopEvalHubServer(t)
	eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)

	fc := fake.NewClientBuilder().WithScheme(sc).WithObjects(eh, job, pod).Build()
	r, rec := buildJobFailureReconciler(fc, ns)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: job.Name},
	})
	require.NoError(t, err)
	assert.Greater(t, result.RequeueAfter, time.Duration(0))

	select {
	case ev := <-rec.Events:
		t.Fatalf("expected no EvaluationFailed event before fallback observations pass grace, got: %s", ev)
	default:
	}

	var persisted batchv1.Job
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: job.Name}, &persisted))
	observations := imagePullBackOffObservations(&persisted)
	require.Len(t, observations, 2)
	assert.Contains(t, observations, imagePullBackOffObservationKey(pod, imagePullBackOffContainer{name: adapterContainerName, image: "registry.example.com/evalhub/adapter:latest"}))
	assert.Contains(t, observations, imagePullBackOffObservationKey(pod, imagePullBackOffContainer{name: sidecarContainerName, image: "registry.example.com/evalhub/sidecar:latest"}))
}

func TestJobFailureReconciler_ImagePullBackOffObservationSurvivesErrImagePullRetry(t *testing.T) {
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-imagepullbackoff-retry", ns, map[string]string{
		evalHubInstanceNameLabel:      "evalhub-1",
		evalHubInstanceNamespaceLabel: "control-ns",
	})
	pod := imagePullBackOffAdapterPod(job.Name, ns, job.UID)
	srv := noopEvalHubServer(t)
	eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)

	fc := fake.NewClientBuilder().WithScheme(sc).WithStatusSubresource(&corev1.Pod{}).WithObjects(eh, job, pod).Build()
	r, rec := buildJobFailureReconciler(fc, ns)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: job.Name},
	})
	require.NoError(t, err)
	assert.Greater(t, result.RequeueAfter, time.Duration(0))

	var persisted batchv1.Job
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: job.Name}, &persisted))
	require.Len(t, imagePullBackOffObservations(&persisted), 1)

	var persistedPod corev1.Pod
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: pod.Name}, &persistedPod))
	persistedPod.Status.ContainerStatuses = []corev1.ContainerStatus{{
		Name:  adapterContainerName,
		Image: "registry.example.com/evalhub/adapter:latest",
		State: corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{Reason: waitingReasonErrImagePull, Message: "pull failed"},
		},
	}}
	require.NoError(t, fc.Status().Update(context.Background(), &persistedPod))

	result, err = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: job.Name},
	})
	require.NoError(t, err)
	assert.Greater(t, result.RequeueAfter, time.Duration(0))

	select {
	case ev := <-rec.Events:
		t.Fatalf("expected no EvaluationFailed event while retry is in ErrImagePull, got: %s", ev)
	default:
	}

	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: job.Name}, &persisted))
	require.Len(t, imagePullBackOffObservations(&persisted), 1)
}

func TestJobFailureReconciler_ImagePullBackOffObservationPrunedDuringErrImagePullRetry(t *testing.T) {
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-imagepullbackoff-prune-retry", ns, map[string]string{
		evalHubInstanceNameLabel:      "evalhub-1",
		evalHubInstanceNamespaceLabel: "control-ns",
	})
	pod := imagePullBackOffAdapterAndSidecarPod(job.Name, ns, job.UID)
	srv := noopEvalHubServer(t)
	eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)

	fc := fake.NewClientBuilder().WithScheme(sc).WithStatusSubresource(&corev1.Pod{}).WithObjects(eh, job, pod).Build()
	r, rec := buildJobFailureReconciler(fc, ns)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: job.Name},
	})
	require.NoError(t, err)
	assert.Greater(t, result.RequeueAfter, time.Duration(0))

	var persisted batchv1.Job
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: job.Name}, &persisted))
	require.Len(t, imagePullBackOffObservations(&persisted), 2)

	var persistedPod corev1.Pod
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: pod.Name}, &persistedPod))
	persistedPod.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Name:  adapterContainerName,
			Image: "registry.example.com/evalhub/adapter:v2",
			State: corev1.ContainerState{
				Waiting: &corev1.ContainerStateWaiting{Reason: waitingReasonErrImagePull, Message: "pull failed"},
			},
		},
		{
			Name:  sidecarContainerName,
			Image: "registry.example.com/evalhub/sidecar:latest",
			State: corev1.ContainerState{
				Waiting: &corev1.ContainerStateWaiting{Reason: waitingReasonErrImagePull, Message: "pull failed"},
			},
		},
	}
	require.NoError(t, fc.Status().Update(context.Background(), &persistedPod))

	result, err = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: job.Name},
	})
	require.NoError(t, err)
	assert.Greater(t, result.RequeueAfter, time.Duration(0))

	select {
	case ev := <-rec.Events:
		t.Fatalf("expected no EvaluationFailed event while retry is in ErrImagePull, got: %s", ev)
	default:
	}

	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: job.Name}, &persisted))
	observations := imagePullBackOffObservations(&persisted)
	require.Len(t, observations, 1)
	assert.NotContains(t, observations, imagePullBackOffObservationKey(pod, imagePullBackOffContainer{name: adapterContainerName, image: "registry.example.com/evalhub/adapter:latest"}))
	assert.Contains(t, observations, imagePullBackOffObservationKey(pod, imagePullBackOffContainer{name: sidecarContainerName, image: "registry.example.com/evalhub/sidecar:latest"}))
}

// TestJobFailureReconciler_StaleBackOffForDifferentImage_DoesNotFail verifies an old
// BackOff event for another image does not age a fresh adapter ImagePullBackOff past grace.
func TestJobFailureReconciler_StaleBackOffForDifferentImage_DoesNotFail(t *testing.T) {
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-imagepullbackoff-different-image", ns, map[string]string{
		evalHubInstanceNameLabel:      "evalhub-1",
		evalHubInstanceNamespaceLabel: "control-ns",
	})
	pod := imagePullBackOffAdapterPod(job.Name, ns, job.UID)
	staleOtherImageBackOff := imagePullBackOffEventForImage(pod, 30*time.Minute, "registry.example.com/evalhub/init:latest")
	staleInitBackOffSameImage := imagePullBackOffEventForImageAndContainer(pod, 30*time.Minute, "registry.example.com/evalhub/adapter:latest", initContainerName)
	srv := noopEvalHubServer(t)
	eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)

	var patchedLabels map[string]string

	fc := fake.NewClientBuilder().
		WithScheme(sc).
		WithObjects(eh, job, pod, staleOtherImageBackOff, staleInitBackOffSameImage).
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

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: job.Name},
	})
	require.NoError(t, err)
	assert.Greater(t, result.RequeueAfter, time.Duration(0))

	select {
	case ev := <-rec.Events:
		t.Fatalf("expected no EvaluationFailed event from stale BackOff for another image, got: %s", ev)
	default:
	}

	assert.Nil(t, patchedLabels, "evaluation-phase=Failed should not be patched from stale BackOff for another image")
}

// TestJobFailureReconciler_ImagePullBackOff_EmitsEventAndPatchesJob verifies the full lifecycle for
// a sustained ImagePullBackOff: the reconciler emits an EvaluationFailed event and stamps the Job.
func TestJobFailureReconciler_ImagePullBackOff_EmitsEventAndPatchesJob(t *testing.T) {
	srv := noopEvalHubServer(t)
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-imagepullbackoff", ns, map[string]string{
		evalHubInstanceNameLabel:      "evalhub-1",
		evalHubInstanceNamespaceLabel: "control-ns",
	})
	pod := imagePullBackOffAdapterPod(job.Name, ns, job.UID)
	backOffEvent := imagePullBackOffEvent(pod, 11*time.Minute)
	eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)

	var patchedLabels map[string]string

	fc := fake.NewClientBuilder().
		WithScheme(sc).
		WithObjects(eh, job, pod, backOffEvent).
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
		assert.Contains(t, ev, waitingReasonImagePullBackOff)
	default:
		t.Fatal("expected EvaluationFailed event but recorder is empty")
	}

	require.NotNil(t, patchedLabels, "evaluation-phase=Failed patch was never applied to Job")
	assert.Equal(t, labelEvaluationPhaseFailed, patchedLabels[labelEvaluationPhase])
}

// TestJobFailureReconciler_PostAcceptedPromotionRetry_DoesNotNeedFailureToRecur verifies that
// once EvalHub accepts the failure POST, a later reconcile can finish Job promotion from annotations.
func TestJobFailureReconciler_PostAcceptedPromotionRetry_DoesNotNeedFailureToRecur(t *testing.T) {
	srv := noopEvalHubServer(t)
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-imagepullbackoff-promotion-retry", ns, map[string]string{
		evalHubInstanceNameLabel:      "evalhub-1",
		evalHubInstanceNamespaceLabel: "control-ns",
	})
	pod := imagePullBackOffAdapterPod(job.Name, ns, job.UID)
	backOffEvent := imagePullBackOffEvent(pod, 11*time.Minute)
	eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)

	var failedPromotionOnce bool
	var patchedLabels map[string]string

	fc := fake.NewClientBuilder().
		WithScheme(sc).
		WithObjects(eh, job, pod, backOffEvent).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				if j, ok := obj.(*batchv1.Job); ok &&
					j.Labels[labelEvaluationPhase] == labelEvaluationPhaseFailed &&
					j.Annotations[annotationFailureReported] == "true" {
					if !failedPromotionOnce {
						failedPromotionOnce = true
						return fmt.Errorf("inject promote patch failure")
					}
					patchedLabels = j.Labels
				}
				return c.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()

	r, rec := buildJobFailureReconciler(fc, ns)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: job.Name},
	})
	require.NoError(t, err)
	assert.Equal(t, 10*time.Second, result.RequeueAfter)
	assert.True(t, failedPromotionOnce)

	select {
	case ev := <-rec.Events:
		t.Fatalf("expected no Kubernetes event until promotion succeeds, got: %s", ev)
	default:
	}

	var persisted batchv1.Job
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: job.Name}, &persisted))
	assert.Equal(t, "true", persisted.Annotations[annotationFailureAccepted])
	assert.NotEmpty(t, persisted.Annotations[annotationFailureMessage])
	assert.Empty(t, persisted.Annotations[annotationFailureReported])

	var persistedPod corev1.Pod
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: pod.Name}, &persistedPod))
	persistedPod.Status.ContainerStatuses = nil
	persistedPod.Status.InitContainerStatuses = []corev1.ContainerStatus{{
		Name: initContainerName,
		State: corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{Reason: waitingReasonErrImagePull, Message: "pull failed"},
		},
	}}
	require.NoError(t, fc.Status().Update(context.Background(), &persistedPod))
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: pod.Name}, &persistedPod))
	require.Empty(t, persistedPod.Status.ContainerStatuses)
	require.Len(t, persistedPod.Status.InitContainerStatuses, 1)
	require.Equal(t, waitingReasonErrImagePull, containerStatusWaitingReason(&persistedPod.Status.InitContainerStatuses[0]))

	result, err = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: job.Name},
	})
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	select {
	case ev := <-rec.Events:
		assert.Contains(t, ev, corev1.EventTypeWarning)
		assert.Contains(t, ev, eventReasonEvaluationFailed)
		assert.Contains(t, ev, waitingReasonImagePullBackOff)
	default:
		t.Fatal("expected EvaluationFailed event after promotion retry")
	}

	require.NotNil(t, patchedLabels, "evaluation-phase=Failed patch was never applied on promotion retry")
	assert.Equal(t, labelEvaluationPhaseFailed, patchedLabels[labelEvaluationPhase])
}

func TestJobFailureReconciler_AcceptedMarkerPatchFailure_StillPromotesJob(t *testing.T) {
	srv, requests := capturingEvalHubServer(t)
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-imagepullbackoff-accepted-patch", ns, map[string]string{
		evalHubInstanceNameLabel:      "evalhub-1",
		evalHubInstanceNamespaceLabel: "control-ns",
	})
	pod := imagePullBackOffAdapterPod(job.Name, ns, job.UID)
	backOffEvent := imagePullBackOffEvent(pod, 11*time.Minute)
	eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)

	var failedAcceptedPatchOnce bool
	var patchedLabels map[string]string

	fc := fake.NewClientBuilder().
		WithScheme(sc).
		WithObjects(eh, job, pod, backOffEvent).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				if j, ok := obj.(*batchv1.Job); ok {
					if j.Annotations[annotationFailureAccepted] == "true" && j.Labels[labelEvaluationPhase] != labelEvaluationPhaseFailed && !failedAcceptedPatchOnce {
						failedAcceptedPatchOnce = true
						return fmt.Errorf("inject accepted marker patch failure")
					}
					if j.Labels[labelEvaluationPhase] == labelEvaluationPhaseFailed && j.Annotations[annotationFailureReported] == "true" {
						patchedLabels = j.Labels
					}
				}
				return c.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()

	r, rec := buildJobFailureReconciler(fc, ns)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: job.Name},
	})
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
	assert.True(t, failedAcceptedPatchOnce)

	select {
	case <-requests:
	default:
		t.Fatal("expected EvalHub failure POST before promotion")
	}

	select {
	case ev := <-rec.Events:
		assert.Contains(t, ev, corev1.EventTypeWarning)
		assert.Contains(t, ev, eventReasonEvaluationFailed)
	case <-time.After(time.Second):
		t.Fatal("expected EvaluationFailed event after promotion")
	}

	require.NotNil(t, patchedLabels, "evaluation-phase=Failed patch was never applied after accepted marker patch failed")
	assert.Equal(t, labelEvaluationPhaseFailed, patchedLabels[labelEvaluationPhase])
}

// TestJobFailureReconciler_FailedMount_EmitsEventAndPatchesJob verifies missing Secret volume mounts
// (Scheduled+Pending / ContainerCreating + FailedMount Event) are reported after the grace period.
func TestJobFailureReconciler_FailedMount_EmitsEventAndPatchesJob(t *testing.T) {
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"
	srv := noopEvalHubServer(t)

	eh := readyEvalHubCR("evalhub", "system", srv.URL)
	job := evalHubEvaluationJob("eval-job-mount", ns, map[string]string{
		evalHubInstanceNameLabel:      eh.Name,
		evalHubInstanceNamespaceLabel: eh.Namespace,
	})
	podUID := types.UID("pod-uid-mount")
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      job.Name + "-pod",
			Namespace: ns,
			UID:       podUID,
			Labels:    map[string]string{"batch.kubernetes.io/job-name": job.Name},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "batch/v1", Kind: "Job", Name: job.Name, UID: job.UID,
			}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			Conditions: []corev1.PodCondition{{
				Type:               corev1.PodScheduled,
				Status:             corev1.ConditionTrue,
				LastTransitionTime: metav1.NewTime(time.Now().Add(-3 * time.Minute)),
			}},
			InitContainerStatuses: []corev1.ContainerStatus{{
				Name: initContainerName,
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"},
				},
			}},
		},
	}
	mountEvent := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: "mount-fail", Namespace: ns},
		InvolvedObject: corev1.ObjectReference{
			Kind: "Pod", Name: pod.Name, Namespace: ns, UID: podUID,
		},
		Reason:        eventReasonFailedMount,
		Message:       `MountVolume.SetUp failed for volume "test-data-hf-auth" : secret "missing-hf-token" not found`,
		Type:          corev1.EventTypeWarning,
		LastTimestamp: metav1.NewTime(time.Now()),
	}

	var patchedLabels map[string]string
	fc := fake.NewClientBuilder().
		WithScheme(sc).
		WithObjects(eh, job, pod, mountEvent).
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
		assert.Contains(t, ev, "missing-hf-token")
	default:
		t.Fatal("expected EvaluationFailed event but recorder is empty")
	}

	require.NotNil(t, patchedLabels, "evaluation-phase=Failed patch was never applied to Job")
	assert.Equal(t, labelEvaluationPhaseFailed, patchedLabels[labelEvaluationPhase])
}

func TestJobFailureReconciler_FailedMountStillReportsWhenImagePullPrunePatchFails(t *testing.T) {
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"
	srv := noopEvalHubServer(t)

	eh := readyEvalHubCR("evalhub", "system", srv.URL)
	job := evalHubEvaluationJob("eval-job-mount-prune-error", ns, map[string]string{
		evalHubInstanceNameLabel:      eh.Name,
		evalHubInstanceNamespaceLabel: eh.Namespace,
	})
	podUID := types.UID("pod-uid-mount-prune-error")
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      job.Name + "-pod",
			Namespace: ns,
			UID:       podUID,
			Labels:    map[string]string{"batch.kubernetes.io/job-name": job.Name},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "batch/v1", Kind: "Job", Name: job.Name, UID: job.UID,
			}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			Conditions: []corev1.PodCondition{{
				Type:               corev1.PodScheduled,
				Status:             corev1.ConditionTrue,
				LastTransitionTime: metav1.NewTime(time.Now().Add(-3 * time.Minute)),
			}},
			InitContainerStatuses: []corev1.ContainerStatus{{
				Name: initContainerName,
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"},
				},
			}},
		},
	}
	observations := map[string]time.Time{
		imagePullBackOffObservationKey(pod, imagePullBackOffContainer{name: adapterContainerName, image: "registry.example.com/evalhub/adapter:stale"}): time.Now().Add(-time.Hour),
	}
	raw, err := marshalImagePullBackOffObservations(observations)
	require.NoError(t, err)
	job.Annotations[annotationImagePullBackOffObservations] = raw
	mountEvent := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: "mount-fail-prune-error", Namespace: ns},
		InvolvedObject: corev1.ObjectReference{
			Kind: "Pod", Name: pod.Name, Namespace: ns, UID: podUID,
		},
		Reason:        eventReasonFailedMount,
		Message:       `MountVolume.SetUp failed for volume "test-data-hf-auth" : secret "missing-hf-token" not found`,
		Type:          corev1.EventTypeWarning,
		LastTimestamp: metav1.NewTime(time.Now()),
	}

	var failedPrunePatch bool
	var patchedLabels map[string]string
	fc := fake.NewClientBuilder().
		WithScheme(sc).
		WithObjects(eh, job, pod, mountEvent).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				if j, ok := obj.(*batchv1.Job); ok {
					if !failedPrunePatch && j.Labels[labelEvaluationPhase] != labelEvaluationPhaseFailed &&
						j.Annotations[annotationFailurePending] == "" && j.Annotations[annotationImagePullBackOffObservations] == "" {
						failedPrunePatch = true
						return fmt.Errorf("inject image-pull observation prune patch failure")
					}
					if j.Labels[labelEvaluationPhase] == labelEvaluationPhaseFailed {
						patchedLabels = j.Labels
					}
				}
				return c.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()

	r, rec := buildJobFailureReconciler(fc, ns)

	_, err = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: job.Name},
	})
	require.NoError(t, err)
	require.True(t, failedPrunePatch, "expected image-pull observation prune patch to fail")

	select {
	case ev := <-rec.Events:
		assert.Contains(t, ev, corev1.EventTypeWarning)
		assert.Contains(t, ev, eventReasonEvaluationFailed)
		assert.Contains(t, ev, "missing-hf-token")
	default:
		t.Fatal("expected EvaluationFailed event but recorder is empty")
	}

	require.NotNil(t, patchedLabels, "evaluation-phase=Failed patch was never applied to Job")
	assert.Equal(t, labelEvaluationPhaseFailed, patchedLabels[labelEvaluationPhase])
}

// TestJobFailureReconciler_AcceptedPatchFailure_StillPromotes verifies that when the
// patch persisting the accepted marker fails after EvalHub accepted the failure POST,
// reconciliation continues to the promotion patch instead of returning early.
func TestJobFailureReconciler_AcceptedPatchFailure_StillPromotes(t *testing.T) {
	srv := noopEvalHubServer(t)
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-accepted-patch-fail", ns, map[string]string{
		evalHubInstanceNameLabel:      "evalhub-1",
		evalHubInstanceNamespaceLabel: "control-ns",
	})
	pod := imagePullBackOffAdapterPod(job.Name, ns, job.UID)
	backOffEvent := imagePullBackOffEvent(pod, 11*time.Minute)
	eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)

	var failedAcceptedPatch bool
	var patchedLabels map[string]string

	fc := fake.NewClientBuilder().
		WithScheme(sc).
		WithObjects(eh, job, pod, backOffEvent).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				if j, ok := obj.(*batchv1.Job); ok &&
					j.Annotations[annotationFailureAccepted] == "true" &&
					j.Annotations[annotationFailureReported] == "" &&
					j.Labels[labelEvaluationPhase] != labelEvaluationPhaseFailed {
					failedAcceptedPatch = true
					return fmt.Errorf("inject accepted-marker patch failure")
				}
				if j, ok := obj.(*batchv1.Job); ok && j.Labels[labelEvaluationPhase] == labelEvaluationPhaseFailed {
					patchedLabels = j.Labels
				}
				return c.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()

	r, rec := buildJobFailureReconciler(fc, ns)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: job.Name},
	})
	require.NoError(t, err)
	require.True(t, failedAcceptedPatch, "expected the accepted-marker patch to fail")
	assert.Equal(t, ctrl.Result{}, result, "promotion must complete in the same reconcile despite the accepted-marker patch failure")

	var persisted batchv1.Job
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: job.Name}, &persisted))
	assert.Equal(t, "true", persisted.Annotations[annotationFailureReported])
	assert.NotContains(t, persisted.Annotations, annotationFailurePending)

	require.NotNil(t, patchedLabels, "promotion patch was never applied")
	assert.Equal(t, labelEvaluationPhaseFailed, patchedLabels[labelEvaluationPhase])

	select {
	case ev := <-rec.Events:
		assert.Contains(t, ev, corev1.EventTypeWarning)
		assert.Contains(t, ev, eventReasonEvaluationFailed)
	default:
		t.Fatal("expected EvaluationFailed event despite accepted-marker patch failure")
	}
}

// TestJobFailureReconciler_PostAlreadyTerminal_PromotesWithoutRepost verifies that a
// 409 Conflict from EvalHub (the job already holds a terminal status, e.g. after a
// restart between the accepted POST and the promote patch) promotes the Job in the
// same reconcile instead of reverting pending and re-posting forever.
func TestJobFailureReconciler_PostAlreadyTerminal_PromotesWithoutRepost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"The job can not be failed because it is 'failed'."}`))
	}))
	t.Cleanup(srv.Close)
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-post-already-terminal", ns, map[string]string{
		evalHubInstanceNameLabel:      "evalhub-1",
		evalHubInstanceNamespaceLabel: "control-ns",
	})
	pod := oomKilledAdapterPod(job.Name, ns, job.UID)
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

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: job.Name},
	})
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result, "a 409 from EvalHub must promote in the same reconcile, not requeue")

	var persisted batchv1.Job
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: job.Name}, &persisted))
	assert.Equal(t, "true", persisted.Annotations[annotationFailureReported])
	assert.NotContains(t, persisted.Annotations, annotationFailurePending)

	require.NotNil(t, patchedLabels, "promotion patch was never applied")
	assert.Equal(t, labelEvaluationPhaseFailed, patchedLabels[labelEvaluationPhase])

	select {
	case ev := <-rec.Events:
		assert.Contains(t, ev, corev1.EventTypeWarning)
		assert.Contains(t, ev, eventReasonEvaluationFailed)
	default:
		t.Fatal("expected EvaluationFailed event despite 409 from EvalHub")
	}
}

// TestJobFailureReconciler_MountLookupError_DoesNotDiscardConfirmedFailure verifies
// that an Event-list failure during one pod's volume-mount check does not discard a
// failure already confirmed for another pod.
func TestJobFailureReconciler_MountLookupError_DoesNotDiscardConfirmedFailure(t *testing.T) {
	srv := noopEvalHubServer(t)
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-mount-lookup-error", ns, map[string]string{
		evalHubInstanceNameLabel:      "evalhub-1",
		evalHubInstanceNamespaceLabel: "control-ns",
	})
	oomPod := oomKilledAdapterPod(job.Name, ns, job.UID)
	mountPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      job.Name + "-pod-b",
			Namespace: ns,
			UID:       types.UID("pod-uid-mount-lookup"),
			Labels:    map[string]string{"batch.kubernetes.io/job-name": job.Name},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "batch/v1", Kind: "Job", Name: job.Name, UID: job.UID,
			}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			Conditions: []corev1.PodCondition{{
				Type:               corev1.PodScheduled,
				Status:             corev1.ConditionTrue,
				LastTransitionTime: metav1.NewTime(time.Now().Add(-3 * time.Minute)),
			}},
			InitContainerStatuses: []corev1.ContainerStatus{{
				Name: initContainerName,
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"},
				},
			}},
		},
	}
	eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)

	fc := fake.NewClientBuilder().
		WithScheme(sc).
		WithObjects(eh, job, oomPod, mountPod).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*corev1.EventList); ok {
					return fmt.Errorf("inject event list failure")
				}
				return c.List(ctx, list, opts...)
			},
		}).
		Build()

	r, rec := buildJobFailureReconciler(fc, ns)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: job.Name},
	})
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	select {
	case ev := <-rec.Events:
		assert.Contains(t, ev, eventReasonEvaluationFailed)
		assert.Contains(t, ev, "OOMKilled", "the confirmed OOM failure must still be reported")
	default:
		t.Fatal("expected EvaluationFailed event despite the mount-lookup error")
	}

	var persisted batchv1.Job
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: job.Name}, &persisted))
	assert.Equal(t, "true", persisted.Annotations[annotationFailureReported])
}

// TestJobFailureReconciler_ObservationError_DoesNotSkipSidecarBackOff verifies that
// an observation-patch failure for one container does not skip the remaining
// containers: a sidecar with a past-grace BackOff event is still reported.
func TestJobFailureReconciler_ObservationError_DoesNotSkipSidecarBackOff(t *testing.T) {
	srv := noopEvalHubServer(t)
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-observation-error-sidecar", ns, map[string]string{
		evalHubInstanceNameLabel:      "evalhub-1",
		evalHubInstanceNamespaceLabel: "control-ns",
	})
	pod := imagePullBackOffAdapterAndSidecarPod(job.Name, ns, job.UID)
	backOffAt := metav1.NewTime(time.Now().Add(-11 * time.Minute))
	sidecarEvent := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name + ".sidecar.imagepullbackoff",
			Namespace: ns,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:      "Pod",
			Namespace: ns,
			Name:      pod.Name,
			UID:       pod.UID,
			FieldPath: "spec.containers{" + sidecarContainerName + "}",
		},
		Reason:         eventReasonBackOff,
		Message:        fmt.Sprintf("Back-off pulling image %q", "registry.example.com/evalhub/sidecar:latest"),
		FirstTimestamp: backOffAt,
		LastTimestamp:  backOffAt,
	}
	eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)

	var failedObservationPatch bool
	var patchedLabels map[string]string
	fc := fake.NewClientBuilder().
		WithScheme(sc).
		WithObjects(eh, job, pod, sidecarEvent).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				if j, ok := obj.(*batchv1.Job); ok &&
					j.Annotations[annotationImagePullBackOffObservations] != "" &&
					j.Annotations[annotationFailurePending] == "" &&
					j.Labels[labelEvaluationPhase] != labelEvaluationPhaseFailed {
					failedObservationPatch = true
					return fmt.Errorf("inject image-pull observation patch failure")
				}
				if j, ok := obj.(*batchv1.Job); ok && j.Labels[labelEvaluationPhase] == labelEvaluationPhaseFailed {
					patchedLabels = j.Labels
				}
				return c.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()

	r, rec := buildJobFailureReconciler(fc, ns)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: job.Name},
	})
	require.NoError(t, err)
	require.True(t, failedObservationPatch, "expected the adapter observation patch to fail")
	assert.Equal(t, ctrl.Result{}, result)

	select {
	case ev := <-rec.Events:
		assert.Contains(t, ev, eventReasonEvaluationFailed)
		assert.Contains(t, ev, sidecarContainerName+": waiting reason="+waitingReasonImagePullBackOff, "the sidecar BackOff failure must still be reported")
	default:
		t.Fatal("expected EvaluationFailed event despite the adapter observation error")
	}

	var persisted batchv1.Job
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: job.Name}, &persisted))
	assert.Equal(t, "true", persisted.Annotations[annotationFailureReported])
	require.NotNil(t, patchedLabels, "promotion patch was never applied")
	assert.Equal(t, labelEvaluationPhaseFailed, patchedLabels[labelEvaluationPhase])
}

// TestJobFailureReconciler_EventListError_UsesRecordedObservation verifies that when
// Event listing fails, a previously recorded observation still provides the grace
// clock: a past-grace recorded backoff is reported without any Event lookup.
func TestJobFailureReconciler_EventListError_UsesRecordedObservation(t *testing.T) {
	srv := noopEvalHubServer(t)
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-event-list-error-observation", ns, map[string]string{
		evalHubInstanceNameLabel:      "evalhub-1",
		evalHubInstanceNamespaceLabel: "control-ns",
	})
	pod := imagePullBackOffAdapterPod(job.Name, ns, job.UID)
	observations := map[string]time.Time{
		imagePullBackOffObservationKey(pod, imagePullBackOffContainer{
			name:  adapterContainerName,
			image: "registry.example.com/evalhub/adapter:latest",
		}): time.Now().Add(-11 * time.Minute),
	}
	raw, err := marshalImagePullBackOffObservations(observations)
	require.NoError(t, err)
	job.Annotations[annotationImagePullBackOffObservations] = raw
	eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)

	var patchedLabels map[string]string
	fc := fake.NewClientBuilder().
		WithScheme(sc).
		WithObjects(eh, job, pod).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*corev1.EventList); ok {
					return fmt.Errorf("inject event list failure")
				}
				return c.List(ctx, list, opts...)
			},
			Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				if j, ok := obj.(*batchv1.Job); ok && j.Labels[labelEvaluationPhase] == labelEvaluationPhaseFailed {
					patchedLabels = j.Labels
				}
				return c.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()

	r, rec := buildJobFailureReconciler(fc, ns)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: job.Name},
	})
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	select {
	case ev := <-rec.Events:
		assert.Contains(t, ev, eventReasonEvaluationFailed)
		assert.Contains(t, ev, adapterContainerName+": waiting reason="+waitingReasonImagePullBackOff)
	default:
		t.Fatal("expected EvaluationFailed event from the recorded observation despite event list failures")
	}

	var persisted batchv1.Job
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: job.Name}, &persisted))
	assert.Equal(t, "true", persisted.Annotations[annotationFailureReported])
	require.NotNil(t, patchedLabels, "promotion patch was never applied")
	assert.Equal(t, labelEvaluationPhaseFailed, patchedLabels[labelEvaluationPhase])
}

// TestJobFailureReconciler_ObservationBeforeSuccessfulRun_StartsNewGraceWindow
// verifies that a recorded observation older than the container's last completed
// run is discarded: the second backoff episode gets a fresh grace window instead
// of inheriting the first episode's clock and failing immediately.
func TestJobFailureReconciler_ObservationBeforeSuccessfulRun_StartsNewGraceWindow(t *testing.T) {
	srv := noopEvalHubServer(t)
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-observation-before-run", ns, map[string]string{
		evalHubInstanceNameLabel:      "evalhub-1",
		evalHubInstanceNamespaceLabel: "control-ns",
	})
	pod := imagePullBackOffAdapterPod(job.Name, ns, job.UID)
	// The adapter pulled successfully, ran, and exited five minutes ago.
	pod.Status.ContainerStatuses[0].LastTerminationState = corev1.ContainerState{
		Terminated: &corev1.ContainerStateTerminated{
			StartedAt:  metav1.NewTime(time.Now().Add(-5 * time.Minute)),
			FinishedAt: metav1.NewTime(time.Now().Add(-5 * time.Minute)),
		},
	}
	// The first backoff episode was observed eleven minutes ago, before that run.
	observations := map[string]time.Time{
		imagePullBackOffObservationKey(pod, imagePullBackOffContainer{
			name:  adapterContainerName,
			image: "registry.example.com/evalhub/adapter:latest",
		}): time.Now().Add(-11 * time.Minute),
	}
	raw, err := marshalImagePullBackOffObservations(observations)
	require.NoError(t, err)
	job.Annotations[annotationImagePullBackOffObservations] = raw
	eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)

	fc := fake.NewClientBuilder().WithScheme(sc).WithObjects(eh, job, pod).Build()
	r, rec := buildJobFailureReconciler(fc, ns)

	testStart := time.Now()
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: job.Name},
	})
	require.NoError(t, err)
	assert.Greater(t, result.RequeueAfter, time.Duration(0),
		"the second backoff episode must get a fresh grace window")

	select {
	case ev := <-rec.Events:
		t.Fatalf("expected no EvaluationFailed event while the fresh grace window runs, got: %s", ev)
	default:
	}

	var persisted batchv1.Job
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: job.Name}, &persisted))
	fresh := imagePullBackOffObservations(&persisted)[imagePullBackOffObservationKey(pod, imagePullBackOffContainer{
		name:  adapterContainerName,
		image: "registry.example.com/evalhub/adapter:latest",
	})]
	require.False(t, fresh.IsZero(), "a fresh observation must be recorded")
	assert.False(t, fresh.Before(testStart),
		"the recorded observation must start a new grace window, got %v", fresh)
}

// TestJobFailureReconciler_StaleFailedMount_DoesNotFail verifies a transient old FailedMount Event
// does not mark the Job failed while the pod is still only waiting on a slow start.
func TestJobFailureReconciler_StaleFailedMount_DoesNotFail(t *testing.T) {
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"
	srv := noopEvalHubServer(t)

	eh := readyEvalHubCR("evalhub", "system", srv.URL)
	job := evalHubEvaluationJob("eval-job-stale-mount", ns, map[string]string{
		evalHubInstanceNameLabel:      eh.Name,
		evalHubInstanceNamespaceLabel: eh.Namespace,
	})
	podUID := types.UID("pod-uid-stale-mount")
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      job.Name + "-pod",
			Namespace: ns,
			UID:       podUID,
			Labels:    map[string]string{"batch.kubernetes.io/job-name": job.Name},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "batch/v1", Kind: "Job", Name: job.Name, UID: job.UID,
			}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			Conditions: []corev1.PodCondition{{
				Type:               corev1.PodScheduled,
				Status:             corev1.ConditionTrue,
				LastTransitionTime: metav1.NewTime(time.Now().Add(-3 * time.Minute)),
			}},
			InitContainerStatuses: []corev1.ContainerStatus{{
				Name: initContainerName,
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"},
				},
			}},
		},
	}
	staleEvent := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: "mount-fail-stale", Namespace: ns},
		InvolvedObject: corev1.ObjectReference{
			Kind: "Pod", Name: pod.Name, Namespace: ns, UID: podUID,
		},
		Reason:        eventReasonFailedMount,
		Message:       `MountVolume.SetUp failed for volume "test-data-hf-auth" : secret "missing-hf-token" not found`,
		Type:          corev1.EventTypeWarning,
		LastTimestamp: metav1.NewTime(time.Now().Add(-3 * time.Minute)),
	}

	fc := fake.NewClientBuilder().WithScheme(sc).WithObjects(eh, job, pod, staleEvent).Build()
	r, rec := buildJobFailureReconciler(fc, ns)

	res, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: job.Name},
	})
	require.NoError(t, err)
	// Stale Event must not fail the Job, but the pod is still mount-stuck within the watch
	// window so we requeue to catch a later, recent FailedMount.
	require.Greater(t, res.RequeueAfter, time.Duration(0), "expected bounded FailedMount recheck requeue")

	select {
	case ev := <-rec.Events:
		t.Fatalf("did not expect EvaluationFailed for stale FailedMount, got %q", ev)
	default:
	}
}

// conflictEvalHubServer starts a local HTTP server that answers every request with
// 409 Conflict and the given body, mimicking EvalHub's JobCanNotBeUpdated response.
func conflictEvalHubServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestEvalHubConflictClassification verifies that only the precise per-job
// 'failed' state proves a report was delivered, that aggregate
// 'partially_failed' is never treated as proof, and that unrecognised or empty
// bodies stay unclassified so the caller retries.
func TestEvalHubConflictClassification(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		failure    bool
		nonFailure bool
	}{
		{name: "failed", body: `{"message":"The job can not be failed because it is 'failed'."}`, failure: true},
		{name: "partially_failed", body: `{"message":"The job can not be failed because it is 'partially_failed'."}`, nonFailure: true},
		{name: "cancelled", body: `{"message":"The job can not be failed because it is 'cancelled'."}`, nonFailure: true},
		{name: "canceled", body: `{"message":"The job can not be failed because it is 'canceled'."}`, nonFailure: true},
		{name: "completed", body: `{"message":"The job can not be failed because it is 'completed'."}`, nonFailure: true},
		{name: "unrecognised", body: `{"message":"conflict"}`},
		{name: "empty", body: ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.failure, isEvalHubFailureTerminalState([]byte(tc.body)))
			assert.Equal(t, tc.nonFailure, isEvalHubNonFailureTerminalState([]byte(tc.body)))
		})
	}
}

// TestRememberImagePullBackOffStart_ReplacesStaleObservation verifies a stored
// observation that predates the container's last completed run is replaced by
// the current episode's Event time instead of blocking the write.
func TestRememberImagePullBackOffStart_ReplacesStaleObservation(t *testing.T) {
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-remember-stale", ns, nil)
	pod := imagePullBackOffAdapterPod(job.Name, ns, job.UID)
	container := imagePullBackOffContainers(pod)[0]
	// The adapter completed a run five minutes ago: an older observation belongs
	// to a previous pull episode.
	container.lastRunStart = time.Now().Add(-5 * time.Minute)
	observations := map[string]time.Time{
		imagePullBackOffObservationKey(pod, container): time.Now().Add(-20 * time.Minute),
	}
	raw, err := marshalImagePullBackOffObservations(observations)
	require.NoError(t, err)
	job.Annotations[annotationImagePullBackOffObservations] = raw

	fc := fake.NewClientBuilder().WithScheme(sc).WithObjects(job, pod).Build()
	r, _ := buildJobFailureReconciler(fc, ns)

	eventStart := time.Now().Add(-8 * time.Minute)
	r.rememberImagePullBackOffStart(context.Background(), job, pod, container, eventStart)

	var persisted batchv1.Job
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: job.Name}, &persisted))
	stored := imagePullBackOffObservations(&persisted)[imagePullBackOffObservationKey(pod, container)]
	assert.WithinDuration(t, eventStart, stored, time.Second,
		"a stale observation must be replaced by the current episode's Event time")
}

// TestImagePullBackOffStartedAt_UsesEarlierStoredObservation verifies that a
// later BackOff Event cannot extend an uninterrupted pull failure: the earlier
// valid stored observation wins.
func TestImagePullBackOffStartedAt_UsesEarlierStoredObservation(t *testing.T) {
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-earlier-start", ns, nil)
	pod := imagePullBackOffAdapterPod(job.Name, ns, job.UID)
	container := imagePullBackOffContainers(pod)[0]
	stored := time.Now().Add(-11 * time.Minute)
	observations := map[string]time.Time{
		imagePullBackOffObservationKey(pod, container): stored,
	}
	raw, err := marshalImagePullBackOffObservations(observations)
	require.NoError(t, err)
	job.Annotations[annotationImagePullBackOffObservations] = raw
	recentEvent := imagePullBackOffEvent(pod, time.Minute)

	fc := fake.NewClientBuilder().WithScheme(sc).WithObjects(job, pod, recentEvent).Build()
	r, _ := buildJobFailureReconciler(fc, ns)

	startedAt, found, err := r.imagePullBackOffStartedAt(context.Background(), job, pod, container, time.Now())
	require.NoError(t, err)
	require.True(t, found)
	assert.WithinDuration(t, stored, startedAt, time.Second,
		"a later Event must not replace the earlier valid episode start")
}

// TestPodImagePullBackOffFailureMessage_KeepsObservationAcrossTransientPullReasons
// verifies the grace clock survives RegistryUnavailable and
// SignatureValidationFailed, which kubelet can publish between pull attempts.
func TestPodImagePullBackOffFailureMessage_KeepsObservationAcrossTransientPullReasons(t *testing.T) {
	for _, reason := range []string{waitingReasonRegistryUnavailable, waitingReasonSignatureValidationFailed} {
		t.Run(reason, func(t *testing.T) {
			sc := jobFailureLifecycleScheme(t)
			ns := "tenant-ns"

			job := evalHubEvaluationJob("eval-job-"+strings.ToLower(reason), ns, nil)
			pod := imagePullBackOffAdapterPod(job.Name, ns, job.UID)
			container := imagePullBackOffContainers(pod)[0]
			observations := map[string]time.Time{
				imagePullBackOffObservationKey(pod, container): time.Now().Add(-2 * time.Minute),
			}
			raw, err := marshalImagePullBackOffObservations(observations)
			require.NoError(t, err)
			job.Annotations[annotationImagePullBackOffObservations] = raw
			pod.Status.ContainerStatuses[0].State = corev1.ContainerState{
				Waiting: &corev1.ContainerStateWaiting{Reason: reason, Message: "pull retry"},
			}

			fc := fake.NewClientBuilder().WithScheme(sc).WithObjects(job, pod).Build()
			r, _ := buildJobFailureReconciler(fc, ns)

			msg, fatal, err := r.podImagePullBackOffFailureMessage(context.Background(), job, pod, time.Now())
			require.NoError(t, err)
			assert.False(t, fatal, "transient pull reasons must not report a terminal failure")
			assert.Empty(t, msg)

			var persisted batchv1.Job
			require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: job.Name}, &persisted))
			assert.Contains(t, imagePullBackOffObservations(&persisted), imagePullBackOffObservationKey(pod, container),
				"the grace clock must survive %s", reason)
		})
	}
}

// TestPendingGraceRequeueAfter_ExpiredBackOffNotSuppressedBySibling verifies an
// expired container schedules the short recheck even when a sibling still has a
// long grace window left.
func TestPendingGraceRequeueAfter_ExpiredBackOffNotSuppressedBySibling(t *testing.T) {
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-expired-sibling", ns, nil)
	pod := imagePullBackOffAdapterAndSidecarPod(job.Name, ns, job.UID)
	adapter := imagePullBackOffContainer{name: adapterContainerName, image: "registry.example.com/evalhub/adapter:latest"}
	sidecar := imagePullBackOffContainer{name: sidecarContainerName, image: "registry.example.com/evalhub/sidecar:latest"}
	observations := map[string]time.Time{
		imagePullBackOffObservationKey(pod, adapter): time.Now().Add(-11 * time.Minute), // past grace
		imagePullBackOffObservationKey(pod, sidecar): time.Now().Add(-time.Minute),     // in grace
	}
	raw, err := marshalImagePullBackOffObservations(observations)
	require.NoError(t, err)
	job.Annotations[annotationImagePullBackOffObservations] = raw

	fc := fake.NewClientBuilder().WithScheme(sc).WithObjects(job, pod).Build()
	r, _ := buildJobFailureReconciler(fc, ns)

	requeue := r.pendingGraceRequeueAfter(context.Background(), job, time.Now())
	assert.Equal(t, 30*time.Second, requeue,
		"an expired container must schedule the short recheck even when a sibling still has grace left")
}

// TestPendingGraceRequeueAfter_TransientPullReasonKeepsRequeue verifies a
// container waiting in a transient pull reason still requeues from its recorded
// observation instead of parking the Job without a recheck.
func TestPendingGraceRequeueAfter_TransientPullReasonKeepsRequeue(t *testing.T) {
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-registry-unavailable", ns, nil)
	pod := imagePullBackOffAdapterPod(job.Name, ns, job.UID)
	container := imagePullBackOffContainers(pod)[0]
	observations := map[string]time.Time{
		imagePullBackOffObservationKey(pod, container): time.Now().Add(-2 * time.Minute),
	}
	raw, err := marshalImagePullBackOffObservations(observations)
	require.NoError(t, err)
	job.Annotations[annotationImagePullBackOffObservations] = raw
	pod.Status.ContainerStatuses[0].State = corev1.ContainerState{
		Waiting: &corev1.ContainerStateWaiting{Reason: waitingReasonRegistryUnavailable, Message: "pull retry"},
	}

	fc := fake.NewClientBuilder().WithScheme(sc).WithObjects(job, pod).Build()
	r, _ := buildJobFailureReconciler(fc, ns)

	requeue := r.pendingGraceRequeueAfter(context.Background(), job, time.Now())
	assert.Greater(t, requeue, time.Duration(0),
		"a transient pull reason must keep a bounded recheck from the recorded observation")
}

// TestJobFailureReconciler_PostPartiallyFailed_DoesNotPromote verifies an
// aggregate partially_failed conflict never promotes this benchmark's Job: the
// response does not prove EvalHub recorded a failure for it.
func TestJobFailureReconciler_PostPartiallyFailed_DoesNotPromote(t *testing.T) {
	srv := conflictEvalHubServer(t, `{"message":"The job can not be failed because it is 'partially_failed'."}`)
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-post-partially-failed", ns, map[string]string{
		evalHubInstanceNameLabel:      "evalhub-1",
		evalHubInstanceNamespaceLabel: "control-ns",
	})
	pod := oomKilledAdapterPod(job.Name, ns, job.UID)
	eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)

	fc := fake.NewClientBuilder().WithScheme(sc).WithObjects(eh, job, pod).Build()
	r, rec := buildJobFailureReconciler(fc, ns)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: job.Name},
	})
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result, "an aggregate partially_failed conflict must not promote or requeue")

	select {
	case ev := <-rec.Events:
		t.Fatalf("expected no EvaluationFailed event for partially_failed conflict, got: %s", ev)
	default:
	}

	var persisted batchv1.Job
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: job.Name}, &persisted))
	assert.NotContains(t, persisted.Annotations, annotationFailureReported)
	assert.NotContains(t, persisted.Annotations, annotationFailurePending)
	assert.NotEqual(t, labelEvaluationPhaseFailed, persisted.Labels[labelEvaluationPhase])
}

// TestJobFailureReconciler_PostUnknownConflict_Requeues verifies an
// unclassifiable 409 body stays eligible for recovery instead of being treated
// as a confirmed non-failure state.
func TestJobFailureReconciler_PostUnknownConflict_Requeues(t *testing.T) {
	srv := conflictEvalHubServer(t, `{"message":"conflict"}`)
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-post-unknown-conflict", ns, map[string]string{
		evalHubInstanceNameLabel:      "evalhub-1",
		evalHubInstanceNamespaceLabel: "control-ns",
	})
	pod := oomKilledAdapterPod(job.Name, ns, job.UID)
	eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)

	fc := fake.NewClientBuilder().WithScheme(sc).WithObjects(eh, job, pod).Build()
	r, rec := buildJobFailureReconciler(fc, ns)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: job.Name},
	})
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, result.RequeueAfter, "an unclassifiable 409 must be retried")

	select {
	case ev := <-rec.Events:
		t.Fatalf("expected no EvaluationFailed event for unknown conflict, got: %s", ev)
	default:
	}

	var persisted batchv1.Job
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: job.Name}, &persisted))
	assert.NotContains(t, persisted.Annotations, annotationFailureReported)
	assert.NotContains(t, persisted.Annotations, annotationFailurePending)
}

// TestJobFailureReconciler_SupersededCleanupPatchFailure_Requeues verifies a
// failed cleanup patch after a confirmed non-failure terminal state requeues the
// cleanup instead of leaving the in-flight marker on a quiet Job.
func TestJobFailureReconciler_SupersededCleanupPatchFailure_Requeues(t *testing.T) {
	srv := conflictEvalHubServer(t, `{"message":"The job can not be failed because it is 'cancelled'."}`)
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-superseded-patch-fail", ns, map[string]string{
		evalHubInstanceNameLabel:      "evalhub-1",
		evalHubInstanceNamespaceLabel: "control-ns",
	})
	pod := oomKilledAdapterPod(job.Name, ns, job.UID)
	eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)

	var failedRevert bool
	fc := fake.NewClientBuilder().WithScheme(sc).WithObjects(eh, job, pod).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				if j, ok := obj.(*batchv1.Job); ok &&
					j.Annotations[annotationFailurePending] == "" &&
					j.Annotations[annotationFailureReported] == "" &&
					j.Labels[labelEvaluationPhase] != labelEvaluationPhaseFailed {
					failedRevert = true
					return fmt.Errorf("inject superseded cleanup patch failure")
				}
				return c.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()
	r, rec := buildJobFailureReconciler(fc, ns)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: job.Name},
	})
	require.NoError(t, err)
	require.True(t, failedRevert, "expected the superseded cleanup patch to fail")
	assert.Equal(t, 10*time.Second, result.RequeueAfter, "a failed superseded cleanup must requeue")

	select {
	case ev := <-rec.Events:
		t.Fatalf("expected no EvaluationFailed event for superseded report, got: %s", ev)
	default:
	}
}

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
	"sync/atomic"
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
	"sigs.k8s.io/controller-runtime/pkg/event"
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

// capturingEvalHubServer records status events and acknowledges their delivery.
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
		if payload.BenchmarkStatusEvent != nil && payload.BenchmarkStatusEvent.Precondition == failurePreconditionNonterminal {
			_, _ = fmt.Fprint(w, `{"outcome":"applied"}`)
			return
		}
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

// evalHubEvaluationJob builds a Job with the mandatory EvalHub labels.
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
	for _, cs := range pod.Status.InitContainerStatuses {
		if cs.Name == container {
			fieldPath = fmt.Sprintf("spec.initContainers{%s}", container)
			break
		}
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
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}
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
		assert.Equal(t, failurePreconditionNonterminal, payload.BenchmarkStatusEvent.Precondition)
		require.NotNil(t, payload.BenchmarkStatusEvent.ErrorMessage)
		assert.Contains(t, payload.BenchmarkStatusEvent.ErrorMessage.Message, "OOMKilled")
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

func TestJobFailureReconciler_UsesLegacyBenchmarkLabel(t *testing.T) {
	for _, legacyID := range []string{"arc_easy", ""} {
		name := "with label"
		if legacyID == "" {
			name = "without label"
		}
		t.Run(name, func(t *testing.T) {
			srv, requests := capturingEvalHubServer(t)
			job := evalHubEvaluationJob("legacy-job", "tenant-ns", map[string]string{
				evalHubInstanceNameLabel: "evalhub-1", evalHubInstanceNamespaceLabel: "control-ns",
				evalHubBenchmarkIDLabel: legacyID,
			})
			job.Annotations = nil
			job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}
			eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)
			fc := fake.NewClientBuilder().WithScheme(jobFailureLifecycleScheme(t)).WithObjects(eh, job).Build()
			r, rec := buildJobFailureReconciler(fc, job.Namespace)

			ctx := context.Background()
			result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(job)})
			require.NoError(t, err)
			require.Equal(t, ctrl.Result{}, result)
			if legacyID == "" {
				assert.Empty(t, requests)
				assert.Empty(t, rec.Events)
				return
			}
			require.NotEmpty(t, requests)
			payload := <-requests
			require.NotNil(t, payload.BenchmarkStatusEvent)
			assert.Equal(t, legacyID, payload.BenchmarkStatusEvent.ID)
			assert.Equal(t, failurePreconditionNonterminal, payload.BenchmarkStatusEvent.Precondition)
		})
	}
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
// 'failed' state proves a report was delivered, that cancelled/completed are
// conclusive non-failure terminals, that aggregate 'partially_failed' needs a
// per-benchmark lookup, and that unrecognised or empty bodies stay unclassified.
func TestEvalHubConflictClassification(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		failure    bool
		nonFailure bool
		aggregate  bool
	}{
		{name: "failed", body: `{"message":"The job can not be failed because it is 'failed'."}`, failure: true},
		{name: "partially_failed", body: `{"message":"The job can not be failed because it is 'partially_failed'."}`, aggregate: true},
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
			assert.Equal(t, tc.aggregate, isEvalHubAggregateFailureState([]byte(tc.body)))
		})
	}
}

// TestImagePullBackOffStartedAt_ReplacesStaleObservation verifies end to end
// that a stored observation older than the container's last completed run is
// discarded: the resolver returns the current episode's Event time and persists
// it, so the second episode gets a full grace window.
func TestImagePullBackOffStartedAt_ReplacesStaleObservation(t *testing.T) {
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-stale-start", ns, nil)
	pod := imagePullBackOffAdapterPod(job.Name, ns, job.UID)
	// The adapter completed a run fifteen minutes ago: an older observation
	// belongs to a previous pull episode.
	pod.Status.ContainerStatuses[0].LastTerminationState = corev1.ContainerState{
		Terminated: &corev1.ContainerStateTerminated{
			StartedAt: metav1.NewTime(time.Now().Add(-15 * time.Minute)),
		},
	}
	container := imagePullBackOffContainers(pod)[0]
	observations := map[string]time.Time{
		imagePullBackOffObservationKey(pod, container): time.Now().Add(-20 * time.Minute),
	}
	raw, err := marshalImagePullBackOffObservations(observations)
	require.NoError(t, err)
	job.Annotations[annotationImagePullBackOffObservations] = raw

	backOffAt := time.Now().Add(-8 * time.Minute)
	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: pod.Name + ".adapter.imagepullbackoff", Namespace: ns},
		InvolvedObject: corev1.ObjectReference{
			Kind: "Pod", Namespace: ns, Name: pod.Name, UID: pod.UID,
			FieldPath: "spec.containers{" + adapterContainerName + "}",
		},
		Reason:         eventReasonBackOff,
		Message:        fmt.Sprintf("Back-off pulling image %q", "registry.example.com/evalhub/adapter:latest"),
		FirstTimestamp: metav1.NewTime(backOffAt),
		LastTimestamp:  metav1.NewTime(backOffAt),
	}

	staleEvent := event.DeepCopy()
	staleEvent.Name += ".previous-episode"
	staleEvent.FirstTimestamp = metav1.NewTime(time.Now().Add(-20 * time.Minute))
	fc := fake.NewClientBuilder().WithScheme(sc).WithObjects(job, pod, event, staleEvent).Build()
	r, _ := buildJobFailureReconciler(fc, ns)

	startedAt, found, err := r.imagePullBackOffStartedAt(context.Background(), job, pod, container, time.Now())
	require.NoError(t, err)
	require.True(t, found)
	assert.WithinDuration(t, backOffAt, startedAt, time.Second,
		"the resolver must return the current episode's Event time, not the stale observation")

	var persisted batchv1.Job
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: job.Name}, &persisted))
	stored := imagePullBackOffObservations(&persisted)[imagePullBackOffObservationKey(pod, container)]
	assert.WithinDuration(t, backOffAt, stored, time.Second,
		"the persisted observation must be updated to the current episode's Event time")
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

// TestPodImagePullAssessment_KeepsObservationAcrossTransientPullReasons
// verifies the grace clock survives RegistryUnavailable and
// SignatureValidationFailed, which kubelet can publish between pull attempts.
func TestPodImagePullAssessment_KeepsObservationAcrossTransientPullReasons(t *testing.T) {
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

			assessment := r.assessPodImagePull(context.Background(), job, pod, time.Now())
			msg, fatal, err := assessment.message, assessment.failed, assessment.err
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

// An expired clock reports directly from the same assessment that checks its
// sibling; another grace deadline cannot postpone the confirmed failure.
func TestJobFailureAssessment_ExpiredBackOffNotSuppressedBySibling(t *testing.T) {
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-expired-sibling", ns, nil)
	pod := imagePullBackOffAdapterAndSidecarPod(job.Name, ns, job.UID)
	adapter := imagePullBackOffContainer{name: adapterContainerName, image: "registry.example.com/evalhub/adapter:latest"}
	sidecar := imagePullBackOffContainer{name: sidecarContainerName, image: "registry.example.com/evalhub/sidecar:latest"}
	observations := map[string]time.Time{
		imagePullBackOffObservationKey(pod, adapter): time.Now().Add(-11 * time.Minute), // past grace
		imagePullBackOffObservationKey(pod, sidecar): time.Now().Add(-time.Minute),      // in grace
	}
	raw, err := marshalImagePullBackOffObservations(observations)
	require.NoError(t, err)
	job.Annotations[annotationImagePullBackOffObservations] = raw

	fc := fake.NewClientBuilder().WithScheme(sc).WithObjects(job, pod).Build()
	r, _ := buildJobFailureReconciler(fc, ns)

	requeue := r.assessJobFailure(context.Background(), job, time.Now()).requeueAfter
	assert.Zero(t, requeue, "an expired container is reported directly from the single assessment")
	assert.True(t, r.assessJobFailure(context.Background(), job, time.Now()).failed)
}

// TestJobFailureAssessment_TransientPullReasonKeepsRequeue verifies a
// container waiting in a transient pull reason still requeues from its recorded
// observation instead of parking the Job without a recheck.
func TestJobFailureAssessment_TransientPullReasonKeepsRequeue(t *testing.T) {
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

	requeue := r.assessJobFailure(context.Background(), job, time.Now()).requeueAfter
	assert.Greater(t, requeue, time.Duration(0),
		"a transient pull reason must keep a bounded recheck from the recorded observation")
}

// recordedEvalHubRequest captures one request the fake EvalHub received.
type recordedEvalHubRequest struct {
	Method string
	Path   string
	Tenant string
}

// evalHubJobStateServer answers the failure POST with 409 and the given conflict
// body, and GET /api/v1/evaluations/jobs/{id} with this benchmark's state. An
// empty state makes the GET fail and "notfound" makes it answer 404. Every
// request is recorded so tests can assert the exact EvalHub interaction.
func evalHubJobStateServer(t *testing.T, conflictBody, benchmarkState string) (*httptest.Server, <-chan recordedEvalHubRequest) {
	t.Helper()
	requests := make(chan recordedEvalHubRequest, 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- recordedEvalHubRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Tenant: r.Header.Get(httpHeaderTenant),
		}
		if r.Method == http.MethodGet {
			if benchmarkState == "" {
				http.Error(w, "state lookup injected failure", http.StatusInternalServerError)
				return
			}
			if benchmarkState == "cancelled-without-benchmark" {
				_, _ = fmt.Fprint(w, `{"status":{"state":"cancelled","benchmarks":[]}}`)
				return
			}
			if benchmarkState == "notfound" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w,
				`{"status":{"benchmarks":[{"provider_id":"provider-1","id":"inspect/docvqa","benchmark_index":0,"status":%q}]}}`,
				benchmarkState)
			return
		}
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(conflictBody))
	}))
	t.Cleanup(srv.Close)
	return srv, requests
}

// TestJobFailureReconciler_PostPartiallyFailed_BenchmarkCompleted_DoesNotPromote
// verifies an aggregate partially_failed conflict whose own benchmark completed
// is treated as superseded: no promotion, no requeue, no event.
func TestJobFailureReconciler_PostPartiallyFailed_BenchmarkCompleted_DoesNotPromote(t *testing.T) {
	srv, _ := evalHubJobStateServer(t, `{"message":"The job can not be failed because it is 'partially_failed'."}`, "completed")
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-partially-failed-completed", ns, map[string]string{
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
	assert.Equal(t, ctrl.Result{}, result, "a completed benchmark must not be promoted or requeued")

	select {
	case ev := <-rec.Events:
		t.Fatalf("expected no EvaluationFailed event for a completed benchmark, got: %s", ev)
	default:
	}

	var persisted batchv1.Job
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: job.Name}, &persisted))
	assert.NotContains(t, persisted.Annotations, annotationFailureReported)
	assert.NotContains(t, persisted.Annotations, annotationFailurePending)
	assert.NotEqual(t, labelEvaluationPhaseFailed, persisted.Labels[labelEvaluationPhase])
}

// TestJobFailureReconciler_PostPartiallyFailed_BenchmarkFailed_Promotes verifies
// the per-benchmark lookup: when EvalHub's partially_failed job reports this
// benchmark as failed, the failure was already recorded, so the Job is promoted
// without another POST.
func TestJobFailureReconciler_PostPartiallyFailed_BenchmarkFailed_Promotes(t *testing.T) {
	srv, _ := evalHubJobStateServer(t, `{"message":"The job can not be failed because it is 'partially_failed'."}`, "failed")
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-partially-failed-failed", ns, map[string]string{
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
	assert.Equal(t, ctrl.Result{}, result)

	var persisted batchv1.Job
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: job.Name}, &persisted))
	assert.Equal(t, "true", persisted.Annotations[annotationFailureReported])
	assert.NotContains(t, persisted.Annotations, annotationFailurePending)
	assert.Equal(t, labelEvaluationPhaseFailed, persisted.Labels[labelEvaluationPhase])

	select {
	case ev := <-rec.Events:
		assert.Contains(t, ev, eventReasonEvaluationFailed)
	default:
		t.Fatal("expected EvaluationFailed event after the per-benchmark state proved the failure was recorded")
	}
}

// TestJobFailureReconciler_PostPartiallyFailed_StateLookupFails_Requeues verifies
// an unavailable per-benchmark state keeps the report eligible for recovery
// instead of selecting the superseded path blindly.
func TestJobFailureReconciler_PostPartiallyFailed_StateLookupFails_Requeues(t *testing.T) {
	srv, requests := evalHubJobStateServer(t, `{"message":"The job can not be failed because it is 'partially_failed'."}`, "")
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-partially-failed-lookup-fail", ns, map[string]string{
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
	assert.Equal(t, 30*time.Second, result.RequeueAfter, "an unavailable benchmark state must be retried")

	// Exactly one POST and one GET: the lookup must target this job with the
	// tenant header so kube-rbac-proxy authorizes the request.
	var seen []recordedEvalHubRequest
	for len(seen) < 2 {
		select {
		case req := <-requests:
			seen = append(seen, req)
		case <-time.After(2 * time.Second):
			t.Fatalf("expected a POST and one GET, got %+v", seen)
		}
	}
	getCount := 0
	for _, req := range seen {
		if req.Method != http.MethodGet {
			continue
		}
		getCount++
		assert.Equal(t, "/api/v1/evaluations/jobs/"+job.Labels[evalHubJobIDLabel], req.Path)
		assert.Equal(t, ns, req.Tenant)
	}
	assert.Equal(t, 1, getCount, "exactly one benchmark-state GET must be issued")
	select {
	case req := <-requests:
		t.Fatalf("unexpected extra EvalHub request: %+v", req)
	default:
	}

	select {
	case ev := <-rec.Events:
		t.Fatalf("expected no EvaluationFailed event when the benchmark state is unavailable, got: %s", ev)
	default:
	}

	var persisted batchv1.Job
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: job.Name}, &persisted))
	assert.NotContains(t, persisted.Annotations, annotationFailureReported)
	assert.Equal(t, "true", persisted.Annotations[annotationFailurePending])
	assert.NotEmpty(t, persisted.Annotations[annotationFailureRequest])
}

// TestJobFailureReconciler_PostPartiallyFailed_JobNotFound_DoesNotPromote
// verifies a deleted EvalHub job settles as superseded instead of retrying a
// conflict that can never resolve.
func TestJobFailureReconciler_PostPartiallyFailed_JobNotFound_DoesNotPromote(t *testing.T) {
	srv, _ := evalHubJobStateServer(t, `{"message":"The job can not be failed because it is 'partially_failed'."}`, "notfound")
	sc := jobFailureLifecycleScheme(t)
	ns := "tenant-ns"

	job := evalHubEvaluationJob("eval-job-partially-failed-notfound", ns, map[string]string{
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
	assert.Equal(t, ctrl.Result{}, result, "a deleted EvalHub job must not keep requeueing")

	select {
	case ev := <-rec.Events:
		t.Fatalf("expected no EvaluationFailed event for a deleted EvalHub job, got: %s", ev)
	default:
	}

	var persisted batchv1.Job
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: job.Name}, &persisted))
	assert.NotContains(t, persisted.Annotations, annotationFailureReported)
	assert.NotContains(t, persisted.Annotations, annotationFailurePending)
}

// TestFetchEvalHubBenchmarkState_IndexMatching verifies the lookup prefers an
// exact index match, accepts a single unindexed (legacy) record, and refuses to
// attribute an ambiguous or explicit-mismatch record to the requested benchmark.
func TestFetchEvalHubBenchmarkState_IndexMatching(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		want      string
		wantError bool
	}{
		{
			name: "explicit index match",
			body: `{"status":{"benchmarks":[{"provider_id":"p","id":"b","benchmark_index":1,"status":"failed"}]}}`,
			want: "failed",
		},
		{
			name: "single unindexed record",
			body: `{"status":{"benchmarks":[{"provider_id":"p","id":"b","status":"completed"}]}}`,
			want: "completed",
		},
		{
			name:      "explicit other index",
			body:      `{"status":{"benchmarks":[{"provider_id":"p","id":"b","benchmark_index":0,"status":"failed"}]}}`,
			wantError: true,
		},
		{
			name:      "ambiguous unindexed records",
			body:      `{"status":{"benchmarks":[{"provider_id":"p","id":"b","status":"failed"},{"provider_id":"p","id":"b","status":"completed"}]}}`,
			wantError: true,
		},
		{
			name:      "mixed indexed and unindexed records",
			body:      `{"status":{"benchmarks":[{"provider_id":"p","id":"b","benchmark_index":0,"status":"failed"},{"provider_id":"p","id":"b","status":"completed"}]}}`,
			wantError: true,
		},
		{
			name:      "no matching record",
			body:      `{"status":{"benchmarks":[{"provider_id":"other","id":"b","benchmark_index":1,"status":"failed"}]}}`,
			wantError: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)
			state, err := fetchEvalHubBenchmarkState(context.Background(), &rest.Config{}, srv.URL, "tenant-ns", "job-1", "p", "b", 1)
			if tc.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, state)
		})
	}
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
	assert.Equal(t, "true", persisted.Annotations[annotationFailurePending])
	assert.NotEmpty(t, persisted.Annotations[annotationFailureRequest])
}

// TestJobFailureReconciler_SupersededCleanupPatchFailure_Requeues verifies a
// failed cleanup patch after a confirmed non-failure terminal state requeues the
// cleanup instead of leaving the in-flight marker on a quiet Job.
func TestJobFailureReconciler_SupersededCleanupPatchFailure_Requeues(t *testing.T) {
	srv, _ := evalHubJobStateServer(t, `{"message":"The job can not be failed because it is 'cancelled'."}`, "cancelled")
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

func TestImagePullBackOffStartedAt_RetriesFailedObservationPatch(t *testing.T) {
	ctx := context.Background()
	job := evalHubEvaluationJob("eval-job-observation-retry", "tenant-ns", nil)
	pod := imagePullBackOffAdapterPod(job.Name, job.Namespace, job.UID)
	event := imagePullBackOffEvent(pod, 8*time.Minute)
	container := imagePullBackOffContainers(pod)[0]
	patches := 0
	fc := fake.NewClientBuilder().WithScheme(jobFailureLifecycleScheme(t)).WithObjects(job, pod, event).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				patches++
				if patches == 1 {
					return fmt.Errorf("inject observation patch failure")
				}
				return c.Patch(ctx, obj, patch, opts...)
			},
		}).Build()
	r, _ := buildJobFailureReconciler(fc, job.Namespace)
	start, found, err := r.imagePullBackOffStartedAt(ctx, job, pod, container, time.Now())
	require.Error(t, err)
	require.True(t, found)
	require.WithinDuration(t, event.FirstTimestamp.Time, start, time.Second)
	require.NotContains(t, job.Annotations, annotationImagePullBackOffObservations)
	// Reuse the same working Job, as the requeue calculation does.
	_, found, err = r.imagePullBackOffStartedAt(ctx, job, pod, container, time.Now())
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, 2, patches)
	require.NoError(t, fc.Delete(ctx, event))
	var persisted batchv1.Job
	require.NoError(t, fc.Get(ctx, client.ObjectKeyFromObject(job), &persisted))
	recovered, found, err := r.imagePullBackOffStartedAt(ctx, &persisted, pod, container, time.Now())
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, start.Equal(recovered), "the saved clock must survive Event disappearance")
}

func TestJobFailureReconciler_PostCancelled_ChecksBenchmarkState(t *testing.T) {
	for _, aggregate := range []string{"cancelled", "canceled", "completed"} {
		for _, state := range []string{"failed", "cancelled", "completed", "", "running", "notfound"} {
			t.Run(aggregate+"/"+state, func(t *testing.T) {
				srv, requests := evalHubJobStateServer(t, fmt.Sprintf(`{"message":"The job can not be failed because it is '%s'."}`, aggregate), state)
				job := evalHubEvaluationJob("eval-job-cancelled", "tenant-ns", map[string]string{
					evalHubInstanceNameLabel: "evalhub-1", evalHubInstanceNamespaceLabel: "control-ns",
				})
				pod := oomKilledAdapterPod(job.Name, job.Namespace, job.UID)
				eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)
				fc := fake.NewClientBuilder().WithScheme(jobFailureLifecycleScheme(t)).WithObjects(eh, job, pod).Build()
				r, rec := buildJobFailureReconciler(fc, job.Namespace)
				result, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(job)})
				require.NoError(t, err)
				if state == "" || state == "running" {
					assert.Positive(t, result.RequeueAfter)
				} else {
					assert.Equal(t, ctrl.Result{}, result)
				}
				require.Len(t, requests, 2)
				assert.Equal(t, http.MethodPost, (<-requests).Method)
				assert.Equal(t, http.MethodGet, (<-requests).Method)
				var persisted batchv1.Job
				require.NoError(t, fc.Get(context.Background(), client.ObjectKeyFromObject(job), &persisted))
				if state == "" || state == "running" {
					assert.Equal(t, "true", persisted.Annotations[annotationFailurePending])
					assert.NotEmpty(t, persisted.Annotations[annotationFailureRequest])
				} else {
					assert.NotContains(t, persisted.Annotations, annotationFailurePending)
				}
				if state == "failed" {
					assert.Equal(t, "true", persisted.Annotations[annotationFailureReported])
					assert.Equal(t, labelEvaluationPhaseFailed, persisted.Labels[labelEvaluationPhase])
					select {
					case ev := <-rec.Events:
						assert.Contains(t, ev, eventReasonEvaluationFailed)
					default:
						t.Fatal("expected failure event")
					}
				} else {
					assert.NotContains(t, persisted.Annotations, annotationFailureReported)
					assert.NotEqual(t, labelEvaluationPhaseFailed, persisted.Labels[labelEvaluationPhase])
					select {
					case ev := <-rec.Events:
						t.Fatalf("unexpected failure event: %s", ev)
					default:
					}
				}
			})
		}
	}
}

func TestJobFailureReconciler_ImagePullIgnoresUnrelatedContainers(t *testing.T) {
	for _, name := range []string{"telemetry", initContainerName, adapterContainerName, sidecarContainerName} {
		for _, initStatus := range []bool{false, true} {
			for _, reason := range imagePullRetryWaitingReasons {
				t.Run(fmt.Sprintf("%s/init=%t/%s", name, initStatus, reason), func(t *testing.T) {
					srv, requests := capturingEvalHubServer(t)
					job := evalHubEvaluationJob("eval-job-container-filter", "tenant-ns", map[string]string{
						evalHubInstanceNameLabel: "evalhub-1", evalHubInstanceNamespaceLabel: "control-ns",
					})
					pod := imagePullBackOffAdapterPod(job.Name, job.Namespace, job.UID)
					waiting := pod.Status.ContainerStatuses[0]
					waiting.Name = name
					waiting.State.Waiting.Reason = reason
					pod.Status.ContainerStatuses = nil
					if initStatus {
						pod.Status.InitContainerStatuses = []corev1.ContainerStatus{waiting}
					} else {
						pod.Status.ContainerStatuses = []corev1.ContainerStatus{waiting}
					}
					if name != adapterContainerName {
						pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, corev1.ContainerStatus{
							Name: adapterContainerName, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
						})
					}
					// Seed an expired observation to cover both BackOff detection and the
					// transient-retry requeue path, including clocks from older operators.
					raw, err := marshalImagePullBackOffObservations(map[string]time.Time{
						imagePullBackOffObservationKey(pod, imagePullBackOffContainer{name: name, image: waiting.Image}): time.Now().Add(-11 * time.Minute),
					})
					require.NoError(t, err)
					job.Annotations[annotationImagePullBackOffObservations] = raw
					eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)
					fc := fake.NewClientBuilder().WithScheme(jobFailureLifecycleScheme(t)).WithObjects(eh, job, pod).Build()
					r, rec := buildJobFailureReconciler(fc, job.Namespace)
					requeue := r.assessJobFailure(context.Background(), job, time.Now()).requeueAfter
					result, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(job)})
					require.NoError(t, err)
					var persisted batchv1.Job
					require.NoError(t, fc.Get(context.Background(), client.ObjectKeyFromObject(job), &persisted))
					if name == "telemetry" {
						assert.Zero(t, requeue)
						assert.Equal(t, ctrl.Result{}, result)
						assert.Empty(t, requests, "unrelated image pulls must never fail the benchmark")
						assert.Empty(t, rec.Events)
						assert.NotEqual(t, labelEvaluationPhaseFailed, persisted.Labels[labelEvaluationPhase])
					} else if reason == waitingReasonImagePullBackOff {
						assert.Len(t, requests, 1, "an expired workload container must still report")
						assert.Equal(t, labelEvaluationPhaseFailed, persisted.Labels[labelEvaluationPhase])
					} else {
						assert.Positive(t, requeue, "workload pull retries must keep requeueing")
						assert.Empty(t, requests)
					}
				})
			}
		}
	}
}

func TestJobFailureReconciler_InjectedInitImagePullBlocksAdapter(t *testing.T) {
	for _, restartable := range []bool{false, true} {
		for _, age := range []time.Duration{2 * time.Minute, 11 * time.Minute} {
			t.Run(fmt.Sprintf("restartable=%t/age=%s", restartable, age), func(t *testing.T) {
				srv, requests := capturingEvalHubServer(t)
				job := evalHubEvaluationJob("eval-job-blocking-init", "tenant-ns", map[string]string{
					evalHubInstanceNameLabel: "evalhub-1", evalHubInstanceNamespaceLabel: "control-ns",
				})
				pod := imagePullBackOffAdapterPod(job.Name, job.Namespace, job.UID)
				waiting := pod.Status.ContainerStatuses[0]
				waiting.Name = "telemetry"
				pod.Status.InitContainerStatuses = []corev1.ContainerStatus{waiting}
				pod.Spec.InitContainers = []corev1.Container{{Name: "telemetry", Image: waiting.Image}}
				if restartable {
					policy := corev1.ContainerRestartPolicyAlways
					pod.Spec.InitContainers[0].RestartPolicy = &policy
				}
				pod.Status.ContainerStatuses[0].State = corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{Reason: "PodInitializing"},
				}
				event := imagePullBackOffEventForImageAndContainer(pod, age, waiting.Image, waiting.Name)
				eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)
				fc := fake.NewClientBuilder().WithScheme(jobFailureLifecycleScheme(t)).WithObjects(eh, job, pod, event).Build()
				r, rec := buildJobFailureReconciler(fc, job.Namespace)
				result, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(job)})
				require.NoError(t, err)
				var persisted batchv1.Job
				require.NoError(t, fc.Get(context.Background(), client.ObjectKeyFromObject(job), &persisted))
				if age < imagePullBackOffGracePeriod {
					assert.Positive(t, result.RequeueAfter)
					assert.LessOrEqual(t, result.RequeueAfter, imagePullBackOffGracePeriod-age+time.Second)
					assert.Empty(t, requests)
					assert.Empty(t, rec.Events)
					assert.NotEqual(t, labelEvaluationPhaseFailed, persisted.Labels[labelEvaluationPhase])
				} else {
					assert.Equal(t, ctrl.Result{}, result)
					require.Len(t, requests, 1)
					payload := <-requests
					require.NotNil(t, payload.BenchmarkStatusEvent)
					assert.Equal(t, "failed", payload.BenchmarkStatusEvent.Status)
					assert.Equal(t, labelEvaluationPhaseFailed, persisted.Labels[labelEvaluationPhase])
					select {
					case ev := <-rec.Events:
						assert.Contains(t, ev, eventReasonEvaluationFailed)
					default:
						t.Fatal("expected failure event for blocking init")
					}
				}
			})
		}
	}
}

func TestJobFailureReconciler_StartedNativeSidecarDoesNotBlockAdapterPull(t *testing.T) {
	for _, reason := range imagePullRetryWaitingReasons {
		t.Run(reason, func(t *testing.T) {
			now := time.Now()
			srv, requests := capturingEvalHubServer(t)
			job := evalHubEvaluationJob("eval-job-native-sidecar-retry", "tenant-ns", map[string]string{
				evalHubInstanceNameLabel: "evalhub-1", evalHubInstanceNamespaceLabel: "control-ns",
			})
			pod := imagePullBackOffAdapterPod(job.Name, job.Namespace, job.UID)
			telemetry := *pod.Status.ContainerStatuses[0].DeepCopy()
			telemetry.Name = "telemetry"
			telemetry.LastTerminationState.Terminated = &corev1.ContainerStateTerminated{
				StartedAt:  metav1.NewTime(now.Add(-20 * time.Minute)),
				FinishedAt: metav1.NewTime(now.Add(-15 * time.Minute)),
			}
			pod.Status.InitContainerStatuses = []corev1.ContainerStatus{telemetry}
			policy := corev1.ContainerRestartPolicyAlways
			pod.Spec.InitContainers = []corev1.Container{{Name: telemetry.Name, Image: telemetry.Image, RestartPolicy: &policy}}
			pod.Status.ContainerStatuses[0].State.Waiting.Reason = reason
			telemetryKey := imagePullBackOffObservationKey(pod, imagePullBackOffContainer{name: telemetry.Name, image: telemetry.Image})
			adapterKey := imagePullBackOffObservationKey(pod, imagePullBackOffContainer{name: adapterContainerName, image: pod.Status.ContainerStatuses[0].Image})
			raw, err := marshalImagePullBackOffObservations(map[string]time.Time{
				telemetryKey: now.Add(-11 * time.Minute), adapterKey: now.Add(-2 * time.Minute),
			})
			require.NoError(t, err)
			job.Annotations[annotationImagePullBackOffObservations] = raw
			event := imagePullBackOffEventForImageAndContainer(pod, 11*time.Minute, telemetry.Image, telemetry.Name)
			eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)
			fc := fake.NewClientBuilder().WithScheme(jobFailureLifecycleScheme(t)).WithObjects(eh, job, pod, event).Build()
			r, rec := buildJobFailureReconciler(fc, job.Namespace)
			// The unrelated sidecar must not shorten the adapter's own grace requeue.
			assert.Equal(t, 8*time.Minute, r.assessJobFailure(context.Background(), job, now).requeueAfter)
			result, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(job)})
			require.NoError(t, err)
			assert.Greater(t, result.RequeueAfter, 7*time.Minute)
			assert.LessOrEqual(t, result.RequeueAfter, 8*time.Minute)
			assert.Empty(t, requests, "a previously started native sidecar must not cause a failure POST")
			assert.Empty(t, rec.Events)
			var persisted batchv1.Job
			require.NoError(t, fc.Get(context.Background(), client.ObjectKeyFromObject(job), &persisted))
			assert.NotEqual(t, labelEvaluationPhaseFailed, persisted.Labels[labelEvaluationPhase])
			assert.NotContains(t, imagePullBackOffObservations(&persisted), telemetryKey)
			assert.Contains(t, imagePullBackOffObservations(&persisted), adapterKey)
		})
	}
}

// imagePullBackOffStartedAt exercises the same snapshot lookup and resolver used
// by the per-Pod assessment in tests of individual grace clocks.
func (r *EvalHubEvaluationJobFailureReconciler) imagePullBackOffStartedAt(ctx context.Context, job *batchv1.Job, pod *corev1.Pod, c imagePullBackOffContainer, now time.Time) (time.Time, bool, error) {
	events, err := r.imagePullBackOffEvents(ctx, pod)
	return r.imagePullBackOffStartedAtFromEvents(ctx, job, pod, c, now, events, err)
}

func TestJobFailureReconciler_UncertainReportSurvivesPodRecovery(t *testing.T) {
	for _, outcome := range []string{"unknown conflict", "lost response", "lost markers"} {
		t.Run(outcome, func(t *testing.T) {
			calls := make(chan string, 10)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				calls <- req.Method
				if req.Method == http.MethodGet {
					_, _ = fmt.Fprint(w, `{"status":{"benchmarks":[{"provider_id":"provider-1","id":"inspect/docvqa","benchmark_index":0,"status":"failed"}]}}`)
					return
				}
				switch outcome {
				case "unknown conflict":
					w.WriteHeader(http.StatusConflict)
					_, _ = fmt.Fprint(w, `{}`)
				case "lost response":
					conn, _, err := w.(http.Hijacker).Hijack()
					if err == nil {
						_ = conn.Close()
					}
				default:
					w.WriteHeader(http.StatusNoContent)
				}
			}))
			defer srv.Close()
			job := evalHubEvaluationJob("eval-job-uncertain", "tenant-ns", map[string]string{
				evalHubInstanceNameLabel: "evalhub-1", evalHubInstanceNamespaceLabel: "control-ns",
			})
			pod := oomKilledAdapterPod(job.Name, job.Namespace, job.UID)
			eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)
			failMarkers := outcome == "lost markers"
			markerFailures := 0
			fc := fake.NewClientBuilder().WithScheme(jobFailureLifecycleScheme(t)).WithObjects(job, pod, eh).
				WithInterceptorFuncs(interceptor.Funcs{
					Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
						if j, ok := obj.(*batchv1.Job); ok && failMarkers && (j.Annotations[annotationFailureAccepted] == "true" || j.Labels[labelEvaluationPhase] == labelEvaluationPhaseFailed) {
							markerFailures++
							return fmt.Errorf("inject lost accepted and promoted markers")
						}
						return c.Patch(ctx, obj, patch, opts...)
					},
				}).Build()
			r, _ := buildJobFailureReconciler(fc, job.Namespace)
			req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(job)}
			result, err := r.Reconcile(context.Background(), req)
			require.NoError(t, err)
			require.Positive(t, result.RequeueAfter)
			require.Len(t, calls, 1)
			assert.Equal(t, http.MethodPost, <-calls)
			if outcome == "lost markers" {
				assert.Equal(t, 2, markerFailures)
			}
			var persisted batchv1.Job
			require.NoError(t, fc.Get(context.Background(), req.NamespacedName, &persisted))
			require.NotEmpty(t, persisted.Annotations[annotationFailureRequest])
			require.NotContains(t, persisted.Annotations, annotationFailureAccepted)
			// Model operator restart after the source Pod has recovered/disappeared.
			require.NoError(t, fc.Delete(context.Background(), pod))
			failMarkers = false
			r, rec := buildJobFailureReconciler(fc, job.Namespace)
			persisted.Status = batchv1.JobStatus{}
			assert.True(t, jobUpdatePredicate(r).Create(event.CreateEvent{Object: &persisted}), "startup must recover reports without a failure signal")
			result, err = r.Reconcile(context.Background(), req)
			require.NoError(t, err)
			assert.Equal(t, ctrl.Result{}, result)
			require.Len(t, calls, 1)
			assert.Equal(t, http.MethodGet, <-calls, "confirmed acceptance must not POST again")
			require.NoError(t, fc.Get(context.Background(), req.NamespacedName, &persisted))
			assert.Equal(t, labelEvaluationPhaseFailed, persisted.Labels[labelEvaluationPhase])
			assert.Contains(t, persisted.Annotations[annotationEvaluationStatus], "OOMKilled")
			assert.NotContains(t, persisted.Annotations, annotationFailureRequest)
			assert.NotContains(t, persisted.Annotations, annotationFailurePending)
			assert.Len(t, rec.Events, 1)
		})
	}
}

func TestJobFailureReconciler_PendingRequestChecksRemoteState(t *testing.T) {
	for _, state := range []string{"running", "pending-without-benchmark", "failed", "completed", "cancelled", "cancelled-without-benchmark", "notfound", "unavailable"} {
		t.Run(state, func(t *testing.T) {
			calls := make(chan recordedEvalHubRequest, 10)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				calls <- recordedEvalHubRequest{Method: req.Method, Path: req.URL.Path, Tenant: req.Header.Get(httpHeaderTenant)}
				if req.Method == http.MethodPost {
					var payload statusEventPayload
					if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
						t.Errorf("decode failure request: %v", err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					assert.Equal(t, "original failure", payload.BenchmarkStatusEvent.ErrorMessage.Message)
					assert.Equal(t, "saved-benchmark", payload.BenchmarkStatusEvent.ID)
					assert.Equal(t, 2, payload.BenchmarkStatusEvent.BenchmarkIndex)
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if state == "notfound" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				if state == "unavailable" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				switch state {
				case "pending-without-benchmark":
					_, _ = fmt.Fprint(w, `{"status":{"state":"pending"}}`)
					return
				case "cancelled-without-benchmark":
					_, _ = fmt.Fprint(w, `{"status":{"state":"cancelled","benchmarks":[]}}`)
					return
				}
				_, _ = fmt.Fprintf(w, `{"status":{"benchmarks":[{"provider_id":"saved-provider","id":"saved-benchmark","benchmark_index":2,"status":%q}]}}`, state)
			}))
			defer srv.Close()
			job := evalHubEvaluationJob("eval-job-saved-request", "tenant-ns", map[string]string{
				evalHubInstanceNameLabel: "evalhub-1", evalHubInstanceNamespaceLabel: "control-ns",
			})
			job.Status = batchv1.JobStatus{}
			raw, err := json.Marshal(evalHubFailureRequest{JobID: "saved-job", ProviderID: "saved-provider", BenchmarkID: "saved-benchmark", BenchmarkIndex: 2, Message: "original failure"})
			require.NoError(t, err)
			job.Annotations[annotationFailurePending] = "true"
			job.Annotations[annotationFailureRequest] = string(raw)
			eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)
			fc := fake.NewClientBuilder().WithScheme(jobFailureLifecycleScheme(t)).WithObjects(job, eh).Build()
			r, rec := buildJobFailureReconciler(fc, job.Namespace)
			assert.True(t, jobUpdatePredicate(r).Create(event.CreateEvent{Object: job}))
			result, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(job)})
			if state == "unavailable" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, ctrl.Result{}, result)
			}
			shouldPost := state == "running" || state == "pending-without-benchmark"
			expectedRequests := 1
			if shouldPost {
				expectedRequests = 2
			}
			require.Len(t, calls, expectedRequests, "recovery must read the remote state before settling or retrying")
			first := <-calls
			assert.Equal(t, http.MethodGet, first.Method)
			assert.Equal(t, "/api/v1/evaluations/jobs/saved-job", first.Path)
			assert.Equal(t, job.Namespace, first.Tenant)
			if shouldPost {
				require.Len(t, calls, 1)
				assert.Equal(t, http.MethodPost, (<-calls).Method, "a request saved before sending must still be delivered")
			}
			assert.Empty(t, calls)
			var persisted batchv1.Job
			require.NoError(t, fc.Get(context.Background(), client.ObjectKeyFromObject(job), &persisted))
			if state == "unavailable" {
				assert.Equal(t, string(raw), persisted.Annotations[annotationFailureRequest])
				assert.NotEqual(t, labelEvaluationPhaseFailed, persisted.Labels[labelEvaluationPhase])
			} else {
				assert.NotContains(t, persisted.Annotations, annotationFailureRequest)
				assert.NotContains(t, persisted.Annotations, annotationFailurePending)
				assert.Equal(t, shouldPost || state == "failed", persisted.Labels[labelEvaluationPhase] == labelEvaluationPhaseFailed)
			}
			if !shouldPost && state != "failed" {
				assert.Empty(t, rec.Events)
			}
		})
	}
}

func TestJobFailureReconciler_AssessmentUsesOneSnapshot(t *testing.T) {
	for _, age := range []time.Duration{2 * time.Minute, imagePullBackOffGracePeriod} {
		t.Run(age.String(), func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Second)
			job := evalHubEvaluationJob("eval-job-single-scan", "tenant-ns", nil)
			pod := imagePullBackOffAdapterAndSidecarPod(job.Name, job.Namespace, job.UID)
			events := []client.Object{job, pod}
			for _, cs := range append(pod.Status.ContainerStatuses, pod.Status.InitContainerStatuses...) {
				containerAge := 2 * time.Minute
				if cs.Name == sidecarContainerName {
					containerAge = age
				}
				ev := imagePullBackOffEventForImageAndContainer(pod, containerAge, cs.Image, cs.Name)
				ev.FirstTimestamp = metav1.NewTime(now.Add(-containerAge))
				events = append(events, ev)
			}
			podLists, eventLists := 0, 0
			fc := fake.NewClientBuilder().WithScheme(jobFailureLifecycleScheme(t)).WithObjects(events...).
				WithIndex(&corev1.Event{}, "involvedObject.kind", func(obj client.Object) []string { return []string{obj.(*corev1.Event).InvolvedObject.Kind} }).
				WithIndex(&corev1.Event{}, "involvedObject.uid", func(obj client.Object) []string { return []string{string(obj.(*corev1.Event).InvolvedObject.UID)} }).
				WithInterceptorFuncs(interceptor.Funcs{
					List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
						switch list.(type) {
						case *corev1.PodList:
							podLists++
						case *corev1.EventList:
							eventLists++
						}
						return c.List(ctx, list, opts...)
					},
				}).Build()
			r, _ := buildJobFailureReconciler(fc, job.Namespace)
			assessment := r.assessJobFailure(context.Background(), job, now)
			require.NoError(t, assessment.err)
			assert.Equal(t, 1, podLists)
			assert.Equal(t, 1, eventLists, "both containers share the same Event snapshot")
			if age < imagePullBackOffGracePeriod {
				assert.False(t, assessment.failed)
				assert.Equal(t, imagePullBackOffGracePeriod-age, assessment.requeueAfter)
			} else {
				assert.True(t, assessment.failed, "the exact grace deadline must report directly")
				assert.Equal(t, fmt.Sprintf("pod %s: sidecar: waiting reason=ImagePullBackOff message=back-off pulling image", pod.Name), assessment.message)
			}
		})
	}
}

func TestJobFailureReconciler_RequestUpdatesDoNotLoop(t *testing.T) {
	job := evalHubEvaluationJob("eval-job-request-predicate", "tenant-ns", nil)
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}
	r, _ := buildJobFailureReconciler(fake.NewClientBuilder().WithScheme(jobFailureLifecycleScheme(t)).Build(), job.Namespace)
	pred := jobUpdatePredicate(r)
	pending := job.DeepCopy()
	pending.Annotations[annotationFailurePending] = "true"
	pending.Annotations[annotationFailureRequest] = `{"message":"original failure"}`
	assert.False(t, pred.Update(event.UpdateEvent{ObjectOld: job, ObjectNew: pending}), "persisting a request must not enqueue another POST")
	assert.False(t, pred.Update(event.UpdateEvent{ObjectOld: pending, ObjectNew: job}), "superseded cleanup must not enqueue another report")
	accepted := pending.DeepCopy()
	accepted.Annotations[annotationFailureAccepted] = "true"
	accepted.Annotations[annotationFailureMessage] = "original failure"
	assert.True(t, pred.Update(event.UpdateEvent{ObjectOld: pending, ObjectNew: accepted}), "acceptance must still enqueue promotion")
	changed := pending.DeepCopy()
	changed.Status.Conditions = nil
	assert.True(t, pred.Update(event.UpdateEvent{ObjectOld: pending, ObjectNew: changed}), "a request must remain recoverable after Job status changes")
}

func TestJobFailureReconciler_LabelOnlyRequestUpdatesDoNotLoop(t *testing.T) {
	job := evalHubEvaluationJob("eval-job-label-only", "tenant-ns", nil)
	job.Annotations = nil
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}
	r, _ := buildJobFailureReconciler(fake.NewClientBuilder().WithScheme(jobFailureLifecycleScheme(t)).Build(), job.Namespace)
	pred := jobUpdatePredicate(r)

	pending := job.DeepCopy()
	pending.Annotations = map[string]string{
		annotationFailurePending: "true",
		annotationFailureRequest: `{"message":"original failure"}`,
	}
	assert.False(t, pred.Update(event.UpdateEvent{ObjectOld: job, ObjectNew: pending}), "persisting a request must not re-enqueue a label-only Job")

	cleared := job.DeepCopy()
	assert.False(t, pred.Update(event.UpdateEvent{ObjectOld: pending, ObjectNew: cleared}), "clearing a request must not re-enqueue a label-only Job")

	changed := cleared.DeepCopy()
	changed.Annotations = map[string]string{"example.com/diagnostic": "updated"}
	assert.True(t, pred.Update(event.UpdateEvent{ObjectOld: pending, ObjectNew: changed}), "a separate annotation change must still enqueue the Job")
}

func TestJobFailureReconciler_StaleJobCannotReplaceSavedRequest(t *testing.T) {
	for _, observationWrite := range []string{"none", "record", "prune"} {
		t.Run(observationWrite, func(t *testing.T) {
			ctx := context.Background()
			srv, requests := evalHubJobStateServer(t, "", "failed")
			job := evalHubEvaluationJob("eval-job-stale-request", "tenant-ns", map[string]string{
				evalHubInstanceNameLabel: "evalhub-1", evalHubInstanceNamespaceLabel: "control-ns",
			})
			pod := oomKilledAdapterPod(job.Name, job.Namespace, job.UID)
			var backOffEvent *corev1.Event
			if observationWrite != "none" {
				pod = imagePullBackOffAdapterPod(job.Name, job.Namespace, job.UID)
				backOffEvent = imagePullBackOffEvent(pod, 11*time.Minute)
			}
			if observationWrite == "prune" {
				observations, err := marshalImagePullBackOffObservations(map[string]time.Time{
					imagePullBackOffObservationKey(pod, imagePullBackOffContainer{name: "removed-container", image: "obsolete"}): time.Now().Add(-12 * time.Minute),
				})
				require.NoError(t, err)
				job.Annotations[annotationImagePullBackOffObservations] = observations
			}
			eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)
			var stale batchv1.Job
			injectStale := false
			objects := []client.Object{job, pod, eh}
			if backOffEvent != nil {
				objects = append(objects, backOffEvent)
			}
			fc := fake.NewClientBuilder().WithScheme(jobFailureLifecycleScheme(t)).WithObjects(objects...).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if j, ok := obj.(*batchv1.Job); ok && injectStale && key == client.ObjectKeyFromObject(job) {
							injectStale = false
							stale.DeepCopyInto(j)
							return nil
						}
						return c.Get(ctx, key, obj, opts...)
					},
				}).Build()
			require.NoError(t, fc.Get(ctx, client.ObjectKeyFromObject(job), &stale))
			saved := stale.DeepCopy()
			raw, err := json.Marshal(evalHubFailureRequest{
				JobID: job.Labels[evalHubJobIDLabel], ProviderID: "provider-1", BenchmarkID: "inspect/docvqa", Message: "original accepted failure",
			})
			require.NoError(t, err)
			saved.Annotations[annotationFailurePending] = "true"
			saved.Annotations[annotationFailureRequest] = string(raw)
			require.NoError(t, fc.Update(ctx, saved))
			require.NotEqual(t, stale.ResourceVersion, saved.ResourceVersion)
			r, rec := buildJobFailureReconciler(fc, job.Namespace)
			req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(job)}
			injectStale = true
			result, err := r.Reconcile(ctx, req)
			require.NoError(t, err)
			assert.Equal(t, 10*time.Second, result.RequeueAfter, "a stale request write must conflict and requeue")
			assert.Empty(t, requests, "a conflicting write must not send a replacement failure")
			assert.Empty(t, rec.Events)
			var persisted batchv1.Job
			require.NoError(t, fc.Get(ctx, req.NamespacedName, &persisted))
			assert.Equal(t, string(raw), persisted.Annotations[annotationFailureRequest])
			assert.Equal(t, saved.ResourceVersion, persisted.ResourceVersion, "stale observation writes must not refresh the Job")
			assert.Equal(t, saved.Annotations[annotationImagePullBackOffObservations], persisted.Annotations[annotationImagePullBackOffObservations])
			assert.NotEqual(t, labelEvaluationPhaseFailed, persisted.Labels[labelEvaluationPhase])
			// The next fresh read resolves the saved request instead of posting the new message.
			result, err = r.Reconcile(ctx, req)
			require.NoError(t, err)
			assert.Equal(t, ctrl.Result{}, result)
			require.Len(t, requests, 1)
			assert.Equal(t, http.MethodGet, (<-requests).Method)
			require.NoError(t, fc.Get(ctx, req.NamespacedName, &persisted))
			assert.Equal(t, "original accepted failure", persisted.Annotations[annotationEvaluationStatus])
			require.Len(t, rec.Events, 1)
			assert.Contains(t, <-rec.Events, "original accepted failure")
		})
	}
}

func TestJobFailureReconciler_TerminalJobFallback(t *testing.T) {
	for _, tc := range []struct {
		name       string
		terminal   bool
		statusCode int
		body       string
		wantReport bool
	}{
		{"failed pod count alone", false, 0, "", false},
		{"terminal job without pod", true, http.StatusOK, `{"outcome":"applied"}`, true},
		{"adapter already reported", true, http.StatusOK, `{"outcome":"already_terminal"}`, false},
		{"evalhub job removed", true, http.StatusNotFound, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := evalHubEvaluationJob("terminal-test", "tenant-ns", map[string]string{
				evalHubInstanceNameLabel: "evalhub-1", evalHubInstanceNamespaceLabel: "control-ns",
			})
			job.Status.Failed = 1
			if tc.terminal {
				job.Status.Conditions = []batchv1.JobCondition{{
					Type: batchv1.JobFailed, Status: corev1.ConditionTrue,
					LastTransitionTime: metav1.NewTime(time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)),
				}}
			}
			requests := make(chan statusEventPayload, 2)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if !assert.Equal(t, http.MethodPost, req.Method) {
					http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
					return
				}
				assert.Equal(t, job.Namespace, req.Header.Get(httpHeaderTenant))
				var payload statusEventPayload
				if !assert.NoError(t, json.NewDecoder(req.Body).Decode(&payload)) {
					http.Error(w, "invalid payload", http.StatusBadRequest)
					return
				}
				requests <- payload
				if !tc.terminal {
					http.Error(w, "unexpected report", http.StatusBadRequest)
					return
				}
				w.WriteHeader(tc.statusCode)
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)
			fc := fake.NewClientBuilder().WithScheme(jobFailureLifecycleScheme(t)).WithObjects(job, eh).Build()
			r, rec := buildJobFailureReconciler(fc, job.Namespace)
			ctx := context.Background()
			req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(job)}
			result, err := r.Reconcile(ctx, req)
			require.NoError(t, err)
			require.Equal(t, ctrl.Result{}, result)
			if tc.terminal {
				require.Len(t, requests, 1)
				status := (<-requests).BenchmarkStatusEvent
				require.NotNil(t, status)
				assert.Equal(t, "inspect/docvqa", status.ID)
				assert.Equal(t, "failed", status.Status)
				assert.Equal(t, failurePreconditionNonterminal, status.Precondition)
				assert.Equal(t, "2026-11-24T12:00:00Z", status.CompletedAt)
				require.NotNil(t, status.ErrorMessage)
				assert.Equal(t, "Kubernetes marked the evaluation Job as failed.", status.ErrorMessage.Message)
			}
			var stored batchv1.Job
			require.NoError(t, fc.Get(ctx, req.NamespacedName, &stored))
			assert.NotContains(t, stored.Annotations, annotationFailureRequest)
			assert.NotContains(t, stored.Annotations, annotationFailurePending)
			if tc.wantReport {
				assert.Equal(t, "true", stored.Annotations[annotationFailureReported])
				assert.Equal(t, labelEvaluationPhaseFailed, stored.Labels[labelEvaluationPhase])
				assert.Len(t, rec.Events, 1)
				result, err = r.Reconcile(ctx, req)
				require.NoError(t, err)
				assert.Equal(t, ctrl.Result{}, result)
			} else {
				assert.NotContains(t, stored.Annotations, annotationFailureReported)
				assert.NotContains(t, stored.Labels, labelEvaluationPhase)
				assert.Empty(t, rec.Events)
			}
			assert.Empty(t, requests, "no extra failure report should be sent")
		})
	}
}

func TestJobFailureReconciler_RetriesConditionalReportWithoutInferringDelivery(t *testing.T) {
	for _, tc := range []struct {
		name       string
		statusCode int
		body       string
	}{
		{"temporary error", http.StatusServiceUnavailable, ""},
		{"legacy response", http.StatusNoContent, ""},
		{"malformed response", http.StatusOK, "{"},
		{"unknown outcome", http.StatusOK, `{"outcome":"unknown"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := evalHubEvaluationJob("conditional-retry", "tenant-ns", map[string]string{
				evalHubInstanceNameLabel: "evalhub-1", evalHubInstanceNamespaceLabel: "control-ns",
			})
			job.Status.Conditions = []batchv1.JobCondition{{
				Type: batchv1.JobFailed, Status: corev1.ConditionTrue,
				LastTransitionTime: metav1.NewTime(time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)),
			}}
			requests := make(chan statusEventPayload, 2)
			var replies atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if !assert.Equal(t, http.MethodPost, req.Method, "retry the original conditional POST") {
					http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
					return
				}
				var payload statusEventPayload
				if !assert.NoError(t, json.NewDecoder(req.Body).Decode(&payload)) {
					http.Error(w, "invalid payload", http.StatusBadRequest)
					return
				}
				requests <- payload
				if replies.Add(1) == 1 {
					w.WriteHeader(tc.statusCode)
					if tc.body != "" {
						_, _ = fmt.Fprint(w, tc.body)
					}
					return
				}
				_, _ = fmt.Fprint(w, `{"outcome":"already_terminal"}`)
			}))
			defer srv.Close()
			eh := readyEvalHubCR("evalhub-1", "control-ns", srv.URL)
			fc := fake.NewClientBuilder().WithScheme(jobFailureLifecycleScheme(t)).WithObjects(job, eh).Build()
			r, rec := buildJobFailureReconciler(fc, job.Namespace)
			ctx := context.Background()
			req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(job)}
			result, err := r.Reconcile(ctx, req)
			require.NoError(t, err)
			assert.Equal(t, 30*time.Second, result.RequeueAfter)
			require.Len(t, requests, 1)
			first := <-requests
			require.NotNil(t, first.BenchmarkStatusEvent)
			assert.Equal(t, failurePreconditionNonterminal, first.BenchmarkStatusEvent.Precondition)
			var stored batchv1.Job
			require.NoError(t, fc.Get(ctx, req.NamespacedName, &stored))
			require.NotEmpty(t, stored.Annotations[annotationFailureRequest])
			assert.NotContains(t, stored.Annotations, annotationFailureAccepted)
			assert.Empty(t, rec.Events)
			// EvalHub has since published the adapter's terminal failure. The
			// restarted operator must still resolve its own pending report.
			stored.Labels[labelEvaluationPhase] = labelEvaluationPhaseFailed
			require.NoError(t, fc.Update(ctx, &stored))
			restarted, rec := buildJobFailureReconciler(fc, job.Namespace)
			result, err = restarted.Reconcile(ctx, req)
			require.NoError(t, err)
			assert.Equal(t, ctrl.Result{}, result)
			require.Len(t, requests, 1)
			assert.Equal(t, first, <-requests)
			require.NoError(t, fc.Get(ctx, req.NamespacedName, &stored))
			assert.NotContains(t, stored.Annotations, annotationFailurePending)
			assert.NotContains(t, stored.Annotations, annotationFailureRequest)
			assert.NotContains(t, stored.Annotations, annotationFailureReported)
			assert.Empty(t, rec.Events)
		})
	}
}

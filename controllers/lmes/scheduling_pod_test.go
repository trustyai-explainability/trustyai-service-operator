/*
Copyright 2026 The TrustyAI Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package lmes

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	lmesv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/lmes/v1alpha1"
)

// podReadResult is one canned answer to a pod lookup.
type podReadResult struct {
	pod *corev1.Pod
	err error
}

// schedulingStubClient answers pod reads from a scripted sequence so the
// scheduling branches can be exercised without an API server. The last entry
// repeats once the sequence is exhausted.
type schedulingStubClient struct {
	client.Client
	reads      []podReadResult
	readCalls  int
	createErr  error
	createdPod *corev1.Pod
}

func (c *schedulingStubClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if pod, ok := obj.(*corev1.Pod); ok {
		if len(c.reads) == 0 {
			return apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, key.Name)
		}
		index := c.readCalls
		if index > len(c.reads)-1 {
			index = len(c.reads) - 1
		}
		c.readCalls++
		result := c.reads[index]
		if result.err != nil {
			return result.err
		}
		result.pod.DeepCopyInto(pod)
		return nil
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

func (c *schedulingStubClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if pod, ok := obj.(*corev1.Pod); ok {
		if c.createErr != nil {
			return c.createErr
		}
		c.createdPod = pod.DeepCopy()
		return nil
	}
	return c.Client.Create(ctx, obj, opts...)
}

func newSchedulingTestJob() *lmesv1alpha1.LMEvalJob {
	return &lmesv1alpha1.LMEvalJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "scheduling-job",
			Namespace: "test",
			UID:       types.UID("job-uid"),
		},
	}
}

func newOwnedPod(job *lmesv1alpha1.LMEvalJob) *corev1.Pod {
	controller := true
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      job.GetPodName(),
			Namespace: job.Namespace,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: lmesv1alpha1.GroupVersion.String(),
				Kind:       lmesv1alpha1.KindName,
				Name:       job.Name,
				UID:        job.UID,
				Controller: &controller,
			}},
		},
	}
}

func newTerminatingPod(job *lmesv1alpha1.LMEvalJob) *corev1.Pod {
	pod := newOwnedPod(job)
	now := metav1.Now()
	pod.DeletionTimestamp = &now
	pod.Finalizers = []string{"test/finalizer"}
	return pod
}

func newSchedulingReconciler(c *schedulingStubClient) *LMEvalJobReconciler {
	return &LMEvalJobReconciler{Client: c}
}

func Test_ensureExecutionPod(t *testing.T) {
	job := newSchedulingTestJob()
	found := func(pod *corev1.Pod) podReadResult { return podReadResult{pod: pod} }
	notFound := func() podReadResult {
		return podReadResult{err: apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, job.GetPodName())}
	}

	t.Run("creates the pod when none exists", func(t *testing.T) {
		c := &schedulingStubClient{Client: fake.NewClientBuilder().Build(), reads: []podReadResult{notFound()}}
		created, err := newSchedulingReconciler(c).ensureExecutionPod(context.Background(), job, newOwnedPod(job), logr.Discard())
		require.NoError(t, err)
		assert.True(t, created)
		require.NotNil(t, c.createdPod)
		assert.Equal(t, job.GetPodName(), c.createdPod.Name)
	})

	t.Run("accepts an existing owned pod without creating a second one", func(t *testing.T) {
		c := &schedulingStubClient{Client: fake.NewClientBuilder().Build(), reads: []podReadResult{found(newOwnedPod(job))}}
		created, err := newSchedulingReconciler(c).ensureExecutionPod(context.Background(), job, newOwnedPod(job), logr.Discard())
		require.NoError(t, err)
		assert.False(t, created)
		assert.Nil(t, c.createdPod, "the existing pod must be reused, not replaced")
	})

	t.Run("waits for a terminating pod instead of scheduling on top of it", func(t *testing.T) {
		c := &schedulingStubClient{Client: fake.NewClientBuilder().Build(), reads: []podReadResult{found(newTerminatingPod(job))}}

		created, err := newSchedulingReconciler(c).ensureExecutionPod(context.Background(), job, newOwnedPod(job), logr.Discard())
		assert.False(t, created)
		assert.ErrorIs(t, err, errPodTerminating)
		assert.Nil(t, c.createdPod)
	})

	t.Run("waits when the create race is lost to a terminating pod", func(t *testing.T) {
		// The pod was missing from the cache, so creation was attempted, but the
		// server already holds a same-named pod and it is being deleted.
		c := &schedulingStubClient{
			Client:    fake.NewClientBuilder().Build(),
			reads:     []podReadResult{notFound(), found(newTerminatingPod(job))},
			createErr: apierrors.NewAlreadyExists(schema.GroupResource{Resource: "pods"}, job.GetPodName()),
		}

		created, err := newSchedulingReconciler(c).ensureExecutionPod(context.Background(), job, newOwnedPod(job), logr.Discard())
		assert.False(t, created)
		assert.ErrorIs(t, err, errPodTerminating, "a terminating pod must never be adopted as scheduled")
		assert.Nil(t, c.createdPod)
	})

	t.Run("accepts a healthy pod when the create race is lost", func(t *testing.T) {
		c := &schedulingStubClient{
			Client:    fake.NewClientBuilder().Build(),
			reads:     []podReadResult{notFound(), found(newOwnedPod(job))},
			createErr: apierrors.NewAlreadyExists(schema.GroupResource{Resource: "pods"}, job.GetPodName()),
		}

		created, err := newSchedulingReconciler(c).ensureExecutionPod(context.Background(), job, newOwnedPod(job), logr.Discard())
		require.NoError(t, err)
		assert.False(t, created)
		assert.Nil(t, c.createdPod)
	})

	t.Run("treats a pod read failure as retryable", func(t *testing.T) {
		c := &schedulingStubClient{
			Client: fake.NewClientBuilder().Build(),
			reads:  []podReadResult{{err: errors.New("connection reset by peer")}},
		}

		created, err := newSchedulingReconciler(c).ensureExecutionPod(context.Background(), job, newOwnedPod(job), logr.Discard())
		assert.False(t, created)
		assert.ErrorIs(t, err, errPodLookupFailed)
		assert.Nil(t, c.createdPod, "a failed lookup must not be followed by a blind create")
	})

	t.Run("rejects an unrelated pod with the same name", func(t *testing.T) {
		foreign := newOwnedPod(job)
		foreign.OwnerReferences = nil
		c := &schedulingStubClient{Client: fake.NewClientBuilder().Build(), reads: []podReadResult{found(foreign)}}

		created, err := newSchedulingReconciler(c).ensureExecutionPod(context.Background(), job, newOwnedPod(job), logr.Discard())
		assert.False(t, created)
		require.Error(t, err)
		assert.NotErrorIs(t, err, errPodTerminating)
		assert.NotErrorIs(t, err, errPodLookupFailed, "a name collision must fail the job, not retry forever")
		assert.Contains(t, err.Error(), "not controlled by this LMEvalJob")
		assert.Nil(t, c.createdPod)
	})
}

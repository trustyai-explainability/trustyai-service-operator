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

// schedulingStubClient answers pod lookups from a canned result so the
// scheduling branches can be exercised without an API server.
type schedulingStubClient struct {
	client.Client
	pod        *corev1.Pod
	getErr     error
	createdPod *corev1.Pod
}

func (c *schedulingStubClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if pod, ok := obj.(*corev1.Pod); ok {
		switch {
		case c.getErr != nil:
			return c.getErr
		case c.pod != nil:
			c.pod.DeepCopyInto(pod)
			return nil
		default:
			return apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, key.Name)
		}
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

func (c *schedulingStubClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if pod, ok := obj.(*corev1.Pod); ok {
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

func newSchedulingReconciler(c *schedulingStubClient) *LMEvalJobReconciler {
	return &LMEvalJobReconciler{Client: c}
}

func Test_ensureExecutionPod(t *testing.T) {
	job := newSchedulingTestJob()

	t.Run("creates the pod when none exists", func(t *testing.T) {
		c := &schedulingStubClient{Client: fake.NewClientBuilder().Build()}
		created, err := newSchedulingReconciler(c).ensureExecutionPod(context.Background(), job, newOwnedPod(job), logr.Discard())
		require.NoError(t, err)
		assert.True(t, created)
		require.NotNil(t, c.createdPod)
		assert.Equal(t, job.GetPodName(), c.createdPod.Name)
	})

	t.Run("accepts an existing owned pod without creating a second one", func(t *testing.T) {
		c := &schedulingStubClient{Client: fake.NewClientBuilder().Build(), pod: newOwnedPod(job)}
		created, err := newSchedulingReconciler(c).ensureExecutionPod(context.Background(), job, newOwnedPod(job), logr.Discard())
		require.NoError(t, err)
		assert.False(t, created)
		assert.Nil(t, c.createdPod, "the existing pod must be reused, not replaced")
	})

	t.Run("waits for a terminating pod instead of scheduling on top of it", func(t *testing.T) {
		terminating := newOwnedPod(job)
		now := metav1.Now()
		terminating.DeletionTimestamp = &now
		terminating.Finalizers = []string{"test/finalizer"}
		c := &schedulingStubClient{Client: fake.NewClientBuilder().Build(), pod: terminating}

		created, err := newSchedulingReconciler(c).ensureExecutionPod(context.Background(), job, newOwnedPod(job), logr.Discard())
		assert.False(t, created)
		assert.ErrorIs(t, err, errPodTerminating)
		assert.Nil(t, c.createdPod)
	})

	t.Run("treats a pod read failure as retryable", func(t *testing.T) {
		c := &schedulingStubClient{
			Client: fake.NewClientBuilder().Build(),
			getErr: errors.New("connection reset by peer"),
		}

		created, err := newSchedulingReconciler(c).ensureExecutionPod(context.Background(), job, newOwnedPod(job), logr.Discard())
		assert.False(t, created)
		assert.ErrorIs(t, err, errPodLookupFailed)
		assert.Nil(t, c.createdPod, "a failed lookup must not be followed by a blind create")
	})

	t.Run("rejects an unrelated pod with the same name", func(t *testing.T) {
		foreign := newOwnedPod(job)
		foreign.OwnerReferences = nil
		c := &schedulingStubClient{Client: fake.NewClientBuilder().Build(), pod: foreign}

		created, err := newSchedulingReconciler(c).ensureExecutionPod(context.Background(), job, newOwnedPod(job), logr.Discard())
		assert.False(t, created)
		require.Error(t, err)
		assert.NotErrorIs(t, err, errPodTerminating)
		assert.NotErrorIs(t, err, errPodLookupFailed, "a name collision must fail the job, not retry forever")
		assert.Contains(t, err.Error(), "not controlled by this LMEvalJob")
		assert.Nil(t, c.createdPod)
	})
}

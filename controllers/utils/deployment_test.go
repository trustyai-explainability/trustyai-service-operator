package utils

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCheckDeploymentReady(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, appsv1.AddToScheme(scheme))

	replicas := ptr.To(int32(1))
	base := func() *appsv1.Deployment {
		return &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "nemo-guardrails",
				Namespace:  "test-namespace",
				Generation: 2,
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: replicas,
			},
		}
	}

	tests := []struct {
		name    string
		mutate  func(*appsv1.Deployment)
		ready   bool
		wantErr bool
	}{
		{
			name: "pods still starting",
			mutate: func(d *appsv1.Deployment) {
				d.Status.ObservedGeneration = 2
				d.Status.Replicas = 1
				d.Status.UpdatedReplicas = 1
				d.Status.ReadyReplicas = 0
				d.Status.AvailableReplicas = 0
			},
			ready: false,
		},
		{
			name: "status still describes the previous generation",
			mutate: func(d *appsv1.Deployment) {
				d.Status.ObservedGeneration = 1
				d.Status.Replicas = 1
				d.Status.UpdatedReplicas = 1
				d.Status.ReadyReplicas = 1
				d.Status.AvailableReplicas = 1
				d.Status.Conditions = []appsv1.DeploymentCondition{{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionTrue,
				}}
			},
			ready: false,
		},
		{
			name: "scale-down still has more replicas than desired",
			mutate: func(d *appsv1.Deployment) {
				d.Status.ObservedGeneration = 2
				d.Status.Replicas = 2
				d.Status.UpdatedReplicas = 2
				d.Status.ReadyReplicas = 2
				d.Status.AvailableReplicas = 2
			},
			ready: false,
		},
		{
			name: "old replicas still terminating",
			mutate: func(d *appsv1.Deployment) {
				d.Status.ObservedGeneration = 2
				d.Status.Replicas = 2
				d.Status.UpdatedReplicas = 1
				d.Status.ReadyReplicas = 1
				d.Status.AvailableReplicas = 1
			},
			ready: false,
		},
		{
			name: "rollout complete",
			mutate: func(d *appsv1.Deployment) {
				d.Status.ObservedGeneration = 2
				d.Status.Replicas = 1
				d.Status.UpdatedReplicas = 1
				d.Status.ReadyReplicas = 1
				d.Status.AvailableReplicas = 1
			},
			ready: true,
		},
		{
			name: "progress deadline exceeded",
			mutate: func(d *appsv1.Deployment) {
				d.Status.ObservedGeneration = 2
				d.Status.Conditions = []appsv1.DeploymentCondition{{
					Type:   appsv1.DeploymentProgressing,
					Status: corev1.ConditionFalse,
					Reason: "ProgressDeadlineExceeded",
				}}
			},
			ready:   false,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deployment := base()
			tt.mutate(deployment)
			c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&appsv1.Deployment{}).WithObjects(deployment).Build()
			require.NoError(t, c.Status().Update(context.Background(), deployment))

			ready, err := CheckDeploymentReady(context.Background(), c, deployment.Name, deployment.Namespace)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrDeploymentProgressDeadlineExceeded)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.ready, ready)
		})
	}

	t.Run("missing deployment", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		ready, err := CheckDeploymentReady(context.Background(), c, "nemo-guardrails", "models-as-a-service")
		require.NoError(t, err)
		require.False(t, ready)
	})
}

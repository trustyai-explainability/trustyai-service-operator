package nemo_guardrails

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// reconcileStatusesFor runs reconcileStatuses against a previously Ready NemoGuardrails whose
// Deployment has the given status, and returns the resulting NemoGuardrails status.
func reconcileStatusesFor(t *testing.T, deploymentStatus appsv1.DeploymentStatus) nemoguardrailsv1alpha1.NemoGuardrailStatus {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, nemoguardrailsv1alpha1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))

	instance := &nemoguardrailsv1alpha1.NemoGuardrails{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nemo-guardrails",
			Namespace: "models-as-a-service",
		},
		Spec: nemoguardrailsv1alpha1.NemoGuardrailsSpec{
			Replicas:    ptr.To(int32(1)),
			ExposeRoute: ptr.To(false),
		},
		Status: nemoguardrailsv1alpha1.NemoGuardrailStatus{
			Phase: utils.PhaseReady,
		},
	}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:       instance.Name,
			Namespace:  instance.Namespace,
			Generation: 1,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
		},
		Status: deploymentStatus,
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(instance, deployment).
		WithObjects(instance, deployment).
		Build()
	require.NoError(t, c.Status().Update(context.Background(), instance))
	require.NoError(t, c.Status().Update(context.Background(), deployment))

	reconciler := &NemoGuardrailsReconciler{Client: c, Scheme: scheme}
	_, err := reconciler.reconcileStatuses(context.Background(), instance, instance.Namespace)
	require.NoError(t, err)

	updated := &nemoguardrailsv1alpha1.NemoGuardrails{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(instance), updated))
	return updated.Status
}

func TestReconcileStatusesStaysProgressingWhilePodsStart(t *testing.T) {
	status := reconcileStatusesFor(t, appsv1.DeploymentStatus{
		ObservedGeneration: 1,
		Replicas:           1,
		UpdatedReplicas:    1,
		ReadyReplicas:      0,
		AvailableReplicas:  0,
	})

	require.Equal(t, utils.PhaseProgressing, status.Phase)
	require.Equal(t, "http://nemo-guardrails.models-as-a-service.svc.cluster.local", status.Endpoint)
	condition := utils.GetStatusCondition(status.Conditions, "DeploymentReady")
	require.NotNil(t, condition)
	require.Equal(t, corev1.ConditionFalse, condition.Status)
	require.Equal(t, "DeploymentNotReady", condition.Reason)
}

func TestReconcileStatusesReportsErrorWhenRolloutIsStuck(t *testing.T) {
	status := reconcileStatusesFor(t, appsv1.DeploymentStatus{
		ObservedGeneration: 1,
		Replicas:           1,
		UpdatedReplicas:    1,
		Conditions: []appsv1.DeploymentCondition{{
			Type:    appsv1.DeploymentProgressing,
			Status:  corev1.ConditionFalse,
			Reason:  "ProgressDeadlineExceeded",
			Message: `ReplicaSet "nemo-guardrails-abc" has timed out progressing.`,
		}},
	})

	require.Equal(t, utils.PhaseError, status.Phase)
	deploymentCondition := utils.GetStatusCondition(status.Conditions, "DeploymentReady")
	require.NotNil(t, deploymentCondition)
	require.Equal(t, corev1.ConditionFalse, deploymentCondition.Status)
	require.Equal(t, "DeploymentReadinessCheckFailed", deploymentCondition.Reason)
	require.Contains(t, deploymentCondition.Message, "has timed out progressing")

	completeCondition := utils.GetStatusCondition(status.Conditions, utils.ConditionReconcileComplete)
	require.NotNil(t, completeCondition)
	require.Equal(t, utils.ReconcileFailed, completeCondition.Reason)
}

func TestReconcileStatusesKeepsEndpointSchemeUntilDeploymentIsReady(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, nemoguardrailsv1alpha1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))

	const (
		httpEndpoint  = "http://nemo-guardrails.models-as-a-service.svc.cluster.local"
		httpsEndpoint = "https://nemo-guardrails.models-as-a-service.svc.cluster.local"
	)
	instance := &nemoguardrailsv1alpha1.NemoGuardrails{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "nemo-guardrails",
			Namespace:   "models-as-a-service",
			Annotations: map[string]string{constants.AuthAnnotationKey: "true"},
		},
		Spec: nemoguardrailsv1alpha1.NemoGuardrailsSpec{ExposeRoute: ptr.To(false)},
		Status: nemoguardrailsv1alpha1.NemoGuardrailStatus{
			Endpoint: httpEndpoint,
		},
	}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: instance.Name, Namespace: instance.Namespace, Generation: 1},
		Spec:       appsv1.DeploymentSpec{Replicas: ptr.To(int32(1))},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 1,
			Replicas:           2,
			UpdatedReplicas:    1,
			AvailableReplicas:  1,
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(instance, deployment).WithObjects(instance, deployment).Build()
	require.NoError(t, c.Status().Update(context.Background(), instance))
	require.NoError(t, c.Status().Update(context.Background(), deployment))
	reconciler := &NemoGuardrailsReconciler{Client: c, Scheme: scheme}

	reconcile := func() nemoguardrailsv1alpha1.NemoGuardrailStatus {
		t.Helper()
		current := &nemoguardrailsv1alpha1.NemoGuardrails{}
		require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(instance), current))
		_, err := reconciler.reconcileStatuses(context.Background(), current, current.Namespace)
		require.NoError(t, err)
		updated := &nemoguardrailsv1alpha1.NemoGuardrails{}
		require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(instance), updated))
		return updated.Status
	}

	require.Equal(t, httpEndpoint, reconcile().Endpoint)

	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(deployment), deployment))
	deployment.Status = appsv1.DeploymentStatus{
		ObservedGeneration: 1,
		Replicas:           1,
		UpdatedReplicas:    1,
		AvailableReplicas:  1,
	}
	require.NoError(t, c.Status().Update(context.Background(), deployment))
	require.Equal(t, httpsEndpoint, reconcile().Endpoint)
}

func TestInternalServiceEndpointUsesPublishedWorkloadIdentity(t *testing.T) {
	instance := &nemoguardrailsv1alpha1.NemoGuardrails{}
	const (
		serviceName = "ai-tenant-marketing-team-nemo-noauth"
		namespace   = "odh-ai-gateway-infra"
	)

	require.Equal(t,
		"http://ai-tenant-marketing-team-nemo-noauth.odh-ai-gateway-infra.svc.cluster.local",
		internalServiceEndpoint(instance, serviceName, namespace),
	)

	instance.Annotations = map[string]string{"security.opendatahub.io/enable-auth": "true"}
	require.Equal(t,
		"https://ai-tenant-marketing-team-nemo-noauth.odh-ai-gateway-infra.svc.cluster.local",
		internalServiceEndpoint(instance, serviceName, namespace),
	)
}

package trustyaimodule

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestOperatorHealthCheckerReportsHealthyDeployment(t *testing.T) {
	deployment := healthyOperatorDeployment(1, 1, 1, 1, 1)
	checker := NewOperatorHealthChecker(fake.NewClientBuilder().WithObjects(deployment).Build(), "default")

	result := checker.Check(context.Background())
	if !result.Healthy || result.Degraded || result.Unknown {
		t.Fatalf("expected healthy operator deployment, got %#v", result)
	}
}

func TestOperatorHealthCheckerReportsUnavailableDeploymentAsProgressing(t *testing.T) {
	deployment := healthyOperatorDeployment(1, 1, 0, 0, 1)
	checker := NewOperatorHealthChecker(fake.NewClientBuilder().WithObjects(deployment).Build(), "default")

	result := checker.Check(context.Background())
	if result.Healthy || result.Degraded || result.Unknown {
		t.Fatalf("expected unavailable operator deployment to be progressing, got %#v", result)
	}
}

func TestOperatorHealthCheckerReportsReplicaFailureAsDegraded(t *testing.T) {
	deployment := healthyOperatorDeployment(1, 1, 0, 0, 1)
	deployment.Status.Conditions = []appsv1.DeploymentCondition{{
		Type:    appsv1.DeploymentReplicaFailure,
		Status:  corev1.ConditionTrue,
		Reason:  "FailedCreate",
		Message: "failed to create pod",
	}}
	checker := NewOperatorHealthChecker(fake.NewClientBuilder().WithObjects(deployment).Build(), "default")

	result := checker.Check(context.Background())
	if result.Healthy || !result.Degraded || result.Unknown {
		t.Fatalf("expected replica failure to degrade operator, got %#v", result)
	}
	if result.Reason != "failed to create pod" {
		t.Fatalf("expected replica failure message, got %q", result.Reason)
	}
}

func TestOperatorHealthCheckerReportsMissingDeploymentAsProgressing(t *testing.T) {
	checker := NewOperatorHealthChecker(fake.NewClientBuilder().Build(), "default")

	result := checker.Check(context.Background())
	if result.Healthy || result.Degraded || result.Unknown {
		t.Fatalf("expected missing deployment to be progressing, got %#v", result)
	}
}

func healthyOperatorDeployment(replicas, updated, ready, available int32, generation int64) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:       OperatorDeploymentName,
			Namespace:  "default",
			Generation: generation,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "trustyai"}},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "trustyai"}}},
		},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: generation,
			UpdatedReplicas:    updated,
			ReadyReplicas:      ready,
			AvailableReplicas:  available,
		},
	}
}

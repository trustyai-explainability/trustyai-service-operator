package gorch

import (
	"context"
	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"testing"
)

import (
	"os"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestMain(m *testing.M) {
	ctrl.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))
	os.Exit(m.Run())
}

func TestDeploymentTemplateRendering(t *testing.T) {
	// Prepare a minimal Orchestrator CR for testing
	orch := &gorchv1alpha1.GuardrailsOrchestrator{
		Spec: gorchv1alpha1.GuardrailsOrchestratorSpec{
			Replicas:                1,
			EnableGuardrailsGateway: false,
			EnableBuiltInDetectors:  true,
			DisableOrchestrator:     true,
		},
	}
	orch.Namespace = "test-ns"
	orch.Name = "test-orchestrator"

	// Prepare a fake client and scheme
	scheme := runtime.NewScheme()
	_ = appsv1.AddToScheme(scheme)
	_ = gorchv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Add the required configmap to the fake client
	operatorConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "trustyai-service-operator-config",
			Namespace: "test-ns",
		},
		Data: map[string]string{
			"guardrails-orchestrator-image":      "quay.io/trustyai/ta-guardrails-orchestrator:latest",
			"guardrails-sidecar-gateway-image":   "quay.io/trustyai/ta-guardrails-gateway:latest",
			"guardrails-built-in-detector-image": "quay.io/trustyai/ta-guardrails-regex:latest",
			"kube-rbac-proxy":                    "quay.io/openshift/origin-kube-rbac-proxy:4.19",
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(operatorConfigMap).Build()

	// Prepare a reconciler
	r := &GuardrailsOrchestratorReconciler{
		Client:    fakeClient,
		Scheme:    scheme,
		Namespace: "test-ns",
	}

	// Call createDeployment and check for template errors
	ctx := context.Background()
	deployment, err := r.createDeployment(ctx, orch)
	if err != nil {
		t.Fatalf("Failed to render deployment template: %v", err)
	}
	if deployment == nil {
		t.Fatalf("Deployment is nil after rendering template")
	}

}

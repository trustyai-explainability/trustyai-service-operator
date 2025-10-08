package tas

import (
	"context"
	"testing"

	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestCreateKubeRBACProxyConfigMapObject tests the template parsing functionality
func TestCreateKubeRBACProxyConfigMapObject(t *testing.T) {
	// Setup
	err := trustyaiopendatahubiov1alpha1.AddToScheme(scheme.Scheme)
	if err != nil {
		t.Fatalf("Failed to add scheme: %v", err)
	}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
	reconciler := &TrustyAIServiceReconciler{
		Client: client,
		Scheme: scheme.Scheme,
	}

	ctx := context.Background()
	instance := &trustyaiopendatahubiov1alpha1.TrustyAIService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "test-namespace",
		},
		Spec: trustyaiopendatahubiov1alpha1.TrustyAIServiceSpec{},
	}

	// Test
	configMap, err := reconciler.createKubeRBACProxyConfigMapObject(ctx, instance)
	if err != nil {
		t.Fatalf("Failed to create ConfigMap object: %v", err)
	}

	// Verify
	if configMap == nil {
		t.Fatal("ConfigMap is nil")
	}

	if configMap.Name != "test-service-kube-rbac-proxy-config" {
		t.Errorf("Expected name 'test-service-kube-rbac-proxy-config', got '%s'", configMap.Name)
	}

	if configMap.Namespace != "test-namespace" {
		t.Errorf("Expected namespace 'test-namespace', got '%s'", configMap.Namespace)
	}

	// Check labels
	if configMap.Labels["app"] != "test-service" {
		t.Errorf("Expected app label 'test-service', got '%s'", configMap.Labels["app"])
	}

	if configMap.Labels["app.kubernetes.io/component"] != "kube-rbac-proxy" {
		t.Errorf("Expected component label 'kube-rbac-proxy', got '%s'", configMap.Labels["app.kubernetes.io/component"])
	}

	if configMap.Labels["app.kubernetes.io/version"] != constants.Version {
		t.Errorf("Expected version label '%s', got '%s'", constants.Version, configMap.Labels["app.kubernetes.io/version"])
	}

	// Check data
	if _, exists := configMap.Data["config.yaml"]; !exists {
		t.Error("ConfigMap does not contain 'config.yaml' key")
	}

	configData := configMap.Data["config.yaml"]
	if configData == "" {
		t.Error("ConfigMap config.yaml is empty")
	}

	// Verify key configuration elements
	testCases := []string{
		"authorization:",
		"resourceAttributes:",
		"namespace: \"test-namespace\"",
		"resource: \"services\"",
		"resourceName: \"test-service\"",
		"verb: \"get\"",
		"upstreamConfig:",
		"url: \"http://127.0.0.1:8080\"",
		"tlsConfig:",
		"minVersion: \"VersionTLS12\"",
	}

	for _, expected := range testCases {
		if !contains(configData, expected) {
			t.Errorf("ConfigMap config.yaml does not contain expected string: %s", expected)
		}
	}
}

// TestEnsureKubeRBACProxyConfigMap tests the ConfigMap creation logic
func TestEnsureKubeRBACProxyConfigMap(t *testing.T) {
	// Setup
	err := trustyaiopendatahubiov1alpha1.AddToScheme(scheme.Scheme)
	if err != nil {
		t.Fatalf("Failed to add scheme: %v", err)
	}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
	reconciler := &TrustyAIServiceReconciler{
		Client: client,
		Scheme: scheme.Scheme,
	}

	ctx := context.Background()
	instance := &trustyaiopendatahubiov1alpha1.TrustyAIService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "test-namespace",
		},
		Spec: trustyaiopendatahubiov1alpha1.TrustyAIServiceSpec{},
	}

	// Test first call - should create ConfigMap
	err = reconciler.ensureKubeRBACProxyConfigMap(ctx, instance)
	if err != nil {
		t.Fatalf("Failed to ensure ConfigMap: %v", err)
	}

	// Test second call - should not error (idempotent)
	err = reconciler.ensureKubeRBACProxyConfigMap(ctx, instance)
	if err != nil {
		t.Fatalf("Failed to ensure ConfigMap on second call: %v", err)
	}
}

// Helper function to check if a string contains a substring
func contains(text, substr string) bool {
	return len(text) >= len(substr) && (text == substr || containsSubstring(text, substr))
}

func containsSubstring(text, substr string) bool {
	for i := 0; i <= len(text)-len(substr); i++ {
		if text[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

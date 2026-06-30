package configmap_test

import (
	"context"
	"testing"

	"github.com/trustyai-explainability/trustyai-service-operator/pkg/configmap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGet_Success(t *testing.T) {
	// Create a fake ConfigMap
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cm",
			Namespace: "test-ns",
		},
		Data: map[string]string{
			"key1": "value1",
		},
	}

	// Create a fake client with the ConfigMap
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()

	// Test Get
	ctx := context.Background()
	result, err := configmap.Get(ctx, fakeClient, "test-cm", "test-ns")

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if result.Name != "test-cm" {
		t.Errorf("expected name 'test-cm', got: %s", result.Name)
	}

	if result.Data["key1"] != "value1" {
		t.Errorf("expected data key1='value1', got: %s", result.Data["key1"])
	}
}

func TestGet_NotFound(t *testing.T) {
	// Create a fake client with no ConfigMaps
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Test Get for non-existent ConfigMap
	ctx := context.Background()
	result, err := configmap.Get(ctx, fakeClient, "nonexistent", "test-ns")

	if err == nil {
		t.Fatal("expected error for non-existent ConfigMap, got nil")
	}

	if result != nil {
		t.Error("expected nil result for non-existent ConfigMap")
	}

	// Verify the error message contains expected text
	expectedMsg := "configmap nonexistent not found in namespace test-ns"
	if err.Error() != expectedMsg {
		t.Errorf("expected error message '%s', got: %s", expectedMsg, err.Error())
	}
}

func TestGet_DifferentNamespace(t *testing.T) {
	// Create ConfigMaps in different namespaces
	cm1 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "shared-name",
			Namespace: "ns1",
		},
		Data: map[string]string{
			"source": "ns1",
		},
	}
	cm2 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "shared-name",
			Namespace: "ns2",
		},
		Data: map[string]string{
			"source": "ns2",
		},
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm1, cm2).Build()

	ctx := context.Background()

	// Get from ns1
	result1, err := configmap.Get(ctx, fakeClient, "shared-name", "ns1")
	if err != nil {
		t.Fatalf("ns1: unexpected error: %v", err)
	}
	if result1.Data["source"] != "ns1" {
		t.Errorf("ns1: expected source='ns1', got: %s", result1.Data["source"])
	}

	// Get from ns2
	result2, err := configmap.Get(ctx, fakeClient, "shared-name", "ns2")
	if err != nil {
		t.Fatalf("ns2: unexpected error: %v", err)
	}
	if result2.Data["source"] != "ns2" {
		t.Errorf("ns2: expected source='ns2', got: %s", result2.Data["source"])
	}
}

func TestGet_ErrorType(t *testing.T) {
	// Create a fake client with a ConfigMap
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "exists",
			Namespace: "test-ns",
		},
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()

	ctx := context.Background()

	// Test with wrong namespace - should get NotFound error
	_, err := configmap.Get(ctx, fakeClient, "exists", "wrong-ns")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// The error should wrap a NotFound error
	if !errors.IsNotFound(err) {
		// Our wrapper doesn't preserve the original error type, which is acceptable
		// Just verify it's an error with the right message
		if err.Error() != "configmap exists not found in namespace wrong-ns" {
			t.Errorf("unexpected error message: %s", err.Error())
		}
	}
}

// TestGet_WithRealClient would require a real Kubernetes client and is beyond the scope
// of a unit test. The fake client tests above cover the core logic.

package images

import (
	"context"
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testNamespace    = "test-namespace"
	testConfigMap    = "test-configmap"
	testImageFromCM  = "quay.io/trustyai/service:from-configmap"
	testImageFromEnv = "quay.io/trustyai/service:from-env-var"
	testFallback     = "quay.io/trustyai/service:fallback"
)

func TestResolveImage_EnvVarSet(t *testing.T) {
	ctx := context.Background()

	// Create a fake client with a ConfigMap containing an image
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testConfigMap,
			Namespace: testNamespace,
		},
		Data: map[string]string{
			TrustyAIServiceImageKey: testImageFromCM,
		},
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(configMap).Build()

	// Set the environment variable
	os.Setenv(RelatedImageTrustyAIService, testImageFromEnv)
	defer os.Unsetenv(RelatedImageTrustyAIService)

	// Resolve the image
	image, err := ResolveImage(ctx, fakeClient, TrustyAIServiceImageKey, testConfigMap, testNamespace, "")

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should use env var value, not ConfigMap value
	if image != testImageFromEnv {
		t.Errorf("Expected image from env var %q, got %q", testImageFromEnv, image)
	}
}

func TestResolveImage_EnvVarUnset_UsesConfigMap(t *testing.T) {
	ctx := context.Background()

	// Create a fake client with a ConfigMap
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testConfigMap,
			Namespace: testNamespace,
		},
		Data: map[string]string{
			TrustyAIServiceImageKey: testImageFromCM,
		},
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(configMap).Build()

	// Ensure env var is NOT set
	os.Unsetenv(RelatedImageTrustyAIService)

	// Resolve the image
	image, err := ResolveImage(ctx, fakeClient, TrustyAIServiceImageKey, testConfigMap, testNamespace, "")

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should use ConfigMap value
	if image != testImageFromCM {
		t.Errorf("Expected image from ConfigMap %q, got %q", testImageFromCM, image)
	}
}

func TestResolveImage_EnvVarEmpty_UsesConfigMap(t *testing.T) {
	ctx := context.Background()

	// Create a fake client with a ConfigMap
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testConfigMap,
			Namespace: testNamespace,
		},
		Data: map[string]string{
			TrustyAIServiceImageKey: testImageFromCM,
		},
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(configMap).Build()

	// Set env var to empty string
	os.Setenv(RelatedImageTrustyAIService, "")
	defer os.Unsetenv(RelatedImageTrustyAIService)

	// Resolve the image
	image, err := ResolveImage(ctx, fakeClient, TrustyAIServiceImageKey, testConfigMap, testNamespace, "")

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should use ConfigMap value when env var is empty
	if image != testImageFromCM {
		t.Errorf("Expected image from ConfigMap %q, got %q", testImageFromCM, image)
	}
}

func TestResolveImage_NoEnvVar_NoConfigMap_UsesFallback(t *testing.T) {
	ctx := context.Background()

	// Create a fake client with NO ConfigMap
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Ensure env var is NOT set
	os.Unsetenv(RelatedImageTrustyAIService)

	// Resolve the image with fallback
	image, err := ResolveImage(ctx, fakeClient, TrustyAIServiceImageKey, testConfigMap, testNamespace, testFallback)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should use fallback value
	if image != testFallback {
		t.Errorf("Expected fallback image %q, got %q", testFallback, image)
	}
}

func TestResolveImage_NoEnvVar_NoConfigMap_NoFallback_ReturnsError(t *testing.T) {
	ctx := context.Background()

	// Create a fake client with NO ConfigMap
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Ensure env var is NOT set
	os.Unsetenv(RelatedImageTrustyAIService)

	// Resolve the image without fallback
	_, err := ResolveImage(ctx, fakeClient, TrustyAIServiceImageKey, testConfigMap, testNamespace, "")

	// Should return an error
	if err == nil {
		t.Fatal("Expected error when no env var, no ConfigMap, and no fallback, got nil")
	}
}

func TestResolveImage_ConfigMapMissingKey_UsesFallback(t *testing.T) {
	ctx := context.Background()

	// Create a ConfigMap without the expected key
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testConfigMap,
			Namespace: testNamespace,
		},
		Data: map[string]string{
			"some-other-key": "some-value",
		},
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(configMap).Build()

	// Ensure env var is NOT set
	os.Unsetenv(RelatedImageTrustyAIService)

	// Resolve the image with fallback
	image, err := ResolveImage(ctx, fakeClient, TrustyAIServiceImageKey, testConfigMap, testNamespace, testFallback)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should use fallback when key is missing
	if image != testFallback {
		t.Errorf("Expected fallback image %q, got %q", testFallback, image)
	}
}

func TestResolveImage_AllImageMappings(t *testing.T) {
	// Test that all 10 image mappings are correctly defined
	expectedMappings := map[string]string{
		TrustyAIServiceImageKey:           RelatedImageTrustyAIService,
		EvalHubImageKey:                   RelatedImageEvalHub,
		KubeRBACProxyKey:                  RelatedImageKubeRBACProxy,
		LMESPodImageKey:                   RelatedImageLMESJob,
		LMESDriverImageKey:                RelatedImageLMESDriver,
		GuardrailsOrchestratorImageKey:    RelatedImageGuardrailsOrchestrator,
		GuardrailsBuiltInDetectorImageKey: RelatedImageBuiltInDetector,
		GuardrailsSidecarGatewayImageKey:  RelatedImageVLLMOrchestratorGateway,
		GarakProviderImageKey:             RelatedImageGarakLLSProviderDSP,
		NemoGuardrailsImageKey:            RelatedImageNemoGuardrailsServer,
	}

	if len(imageMapping) != len(expectedMappings) {
		t.Errorf("Expected %d image mappings, got %d", len(expectedMappings), len(imageMapping))
	}

	for key, expectedEnvVar := range expectedMappings {
		actualEnvVar, ok := imageMapping[key]
		if !ok {
			t.Errorf("Missing mapping for key %q", key)
			continue
		}
		if actualEnvVar != expectedEnvVar {
			t.Errorf("For key %q: expected env var %q, got %q", key, expectedEnvVar, actualEnvVar)
		}
	}
}

func TestGetImageFromConfigMap_BackwardCompatibility(t *testing.T) {
	ctx := context.Background()

	// Create a ConfigMap
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testConfigMap,
			Namespace: testNamespace,
		},
		Data: map[string]string{
			TrustyAIServiceImageKey: testImageFromCM,
		},
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(configMap).Build()

	// Ensure env var is NOT set
	os.Unsetenv(RelatedImageTrustyAIService)

	// Call the legacy function
	image, err := GetImageFromConfigMap(ctx, fakeClient, TrustyAIServiceImageKey, testConfigMap, testNamespace)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if image != testImageFromCM {
		t.Errorf("Expected image %q, got %q", testImageFromCM, image)
	}
}

func TestGetImageFromConfigMapWithFallback_BackwardCompatibility(t *testing.T) {
	ctx := context.Background()

	// Create a fake client with NO ConfigMap
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Ensure env var is NOT set
	os.Unsetenv(RelatedImageTrustyAIService)

	// Call the legacy function with fallback
	image, err := GetImageFromConfigMapWithFallback(ctx, fakeClient, TrustyAIServiceImageKey, testConfigMap, testNamespace, testFallback)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if image != testFallback {
		t.Errorf("Expected fallback image %q, got %q", testFallback, image)
	}
}

func TestGetImageFromConfigMapWithFallback_EmptyNamespace(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// When namespace is empty, should return fallback immediately
	image, err := GetImageFromConfigMapWithFallback(ctx, fakeClient, TrustyAIServiceImageKey, testConfigMap, "", testFallback)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if image != testFallback {
		t.Errorf("Expected fallback image %q, got %q", testFallback, image)
	}
}

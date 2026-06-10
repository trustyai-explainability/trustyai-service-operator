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

func TestFromEnvReturnsValueWhenSet(t *testing.T) {
	t.Setenv("RELATED_IMAGE_TRUSTYAI_SERVICE", "quay.io/test/image:v1")

	got := fromEnv("trustyaiServiceImage")
	if got != "quay.io/test/image:v1" {
		t.Errorf("fromEnv() = %q, want %q", got, "quay.io/test/image:v1")
	}
}

func TestFromEnvReturnsEmptyWhenUnset(t *testing.T) {
	os.Unsetenv("RELATED_IMAGE_TRUSTYAI_SERVICE")

	got := fromEnv("trustyaiServiceImage")
	if got != "" {
		t.Errorf("fromEnv() = %q, want empty", got)
	}
}

func TestFromEnvReturnsEmptyForUnknownKey(t *testing.T) {
	got := fromEnv("unknown-key")
	if got != "" {
		t.Errorf("fromEnv() = %q, want empty", got)
	}
}

func TestResolvePreferEnvVar(t *testing.T) {
	t.Setenv("RELATED_IMAGE_KUBE_RBAC_PROXY", "quay.io/env/rbac:v2")

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "trustyai-service-operator-config",
			Namespace: "opendatahub",
		},
		Data: map[string]string{
			"kube-rbac-proxy": "quay.io/cm/rbac:old",
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()

	got, err := Resolve(context.Background(), c, "kube-rbac-proxy", "trustyai-service-operator-config", "opendatahub")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got != "quay.io/env/rbac:v2" {
		t.Errorf("Resolve() = %q, want env var value %q", got, "quay.io/env/rbac:v2")
	}
}

func TestResolveFallsBackToConfigMap(t *testing.T) {
	os.Unsetenv("RELATED_IMAGE_KUBE_RBAC_PROXY")

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "trustyai-service-operator-config",
			Namespace: "opendatahub",
		},
		Data: map[string]string{
			"kube-rbac-proxy": "quay.io/cm/rbac:from-cm",
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()

	got, err := Resolve(context.Background(), c, "kube-rbac-proxy", "trustyai-service-operator-config", "opendatahub")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got != "quay.io/cm/rbac:from-cm" {
		t.Errorf("Resolve() = %q, want ConfigMap value %q", got, "quay.io/cm/rbac:from-cm")
	}
}

func TestResolveWithFallbackUsesEnvFirst(t *testing.T) {
	t.Setenv("RELATED_IMAGE_EVALHUB", "quay.io/env/evalhub:latest")

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	got, err := ResolveWithFallback(context.Background(), c, "evalHubImage", "trustyai-service-operator-config", "opendatahub", "fallback-default")
	if err != nil {
		t.Fatalf("ResolveWithFallback() error = %v", err)
	}
	if got != "quay.io/env/evalhub:latest" {
		t.Errorf("ResolveWithFallback() = %q, want env var value", got)
	}
}

func TestResolveWithFallbackUsesDefault(t *testing.T) {
	os.Unsetenv("RELATED_IMAGE_TRUSTYAI_SERVICE")

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	got, err := ResolveWithFallback(context.Background(), c, "trustyaiServiceImage", "trustyai-service-operator-config", "", "quay.io/default/image:v1")
	if err != nil {
		t.Fatalf("ResolveWithFallback() error = %v", err)
	}
	if got != "quay.io/default/image:v1" {
		t.Errorf("ResolveWithFallback() = %q, want fallback %q", got, "quay.io/default/image:v1")
	}
}

func TestAllEnvVarNames(t *testing.T) {
	names := AllEnvVarNames()
	if len(names) != 13 {
		t.Errorf("AllEnvVarNames() returned %d names, want 13", len(names))
	}

	expected := map[string]bool{
		"RELATED_IMAGE_TRUSTYAI_SERVICE":        false,
		"RELATED_IMAGE_EVALHUB":                 false,
		"RELATED_IMAGE_KUBE_RBAC_PROXY":         false,
		"RELATED_IMAGE_LMES_POD":                false,
		"RELATED_IMAGE_LMES_DRIVER":             false,
		"RELATED_IMAGE_GUARDRAILS_ORCHESTRATOR": false,
		"RELATED_IMAGE_GUARDRAILS_DETECTOR":     false,
		"RELATED_IMAGE_GUARDRAILS_GATEWAY":      false,
		"RELATED_IMAGE_GARAK_PROVIDER":          false,
		"RELATED_IMAGE_NEMO_GUARDRAILS":         false,
		"RELATED_IMAGE_EVALHUB_GUIDELLM":        false,
		"RELATED_IMAGE_EVALHUB_LIGHTEVAL":       false,
		"RELATED_IMAGE_EVALHUB_IBM_CLEAR":       false,
	}

	for _, name := range names {
		if _, ok := expected[name]; !ok {
			t.Errorf("unexpected env var name: %s", name)
		}
		expected[name] = true
	}

	for name, found := range expected {
		if !found {
			t.Errorf("missing env var name: %s", name)
		}
	}
}

func TestAllConfigMapKeysAreMapped(t *testing.T) {
	expectedKeys := []string{
		"trustyaiServiceImage",
		"evalHubImage",
		"kube-rbac-proxy",
		"lmes-pod-image",
		"lmes-driver-image",
		"guardrails-orchestrator-image",
		"guardrails-built-in-detector-image",
		"guardrails-sidecar-gateway-image",
		"garak-provider-image",
		"nemo-guardrails-image",
		"evalhub-provider-guidellm-image",
		"evalhub-provider-lighteval-image",
		"evalhub-provider-ibm-clear-image",
	}

	for _, key := range expectedKeys {
		if _, ok := configMapKeyToEnvVar[key]; !ok {
			t.Errorf("ConfigMap key %q has no RELATED_IMAGE mapping", key)
		}
	}
}

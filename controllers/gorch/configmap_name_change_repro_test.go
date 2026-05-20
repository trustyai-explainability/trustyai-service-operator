package gorch

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// newTestReconciler creates a reconciler with a fake client pre-loaded with the given objects.
func newTestReconciler(objs ...corev1.ConfigMap) *GuardrailsOrchestratorReconciler {
	s := runtime.NewScheme()
	_ = gorchv1alpha1.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	clientObjs := make([]corev1.ConfigMap, len(objs))
	copy(clientObjs, objs)

	builder := fake.NewClientBuilder().WithScheme(s)
	for i := range clientObjs {
		builder = builder.WithObjects(&clientObjs[i])
	}
	return &GuardrailsOrchestratorReconciler{
		Client: builder.Build(),
		Scheme: s,
	}
}

func makeConfigMap(name, ns, content string) corev1.ConfigMap {
	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Data:       map[string]string{"config.yaml": content},
	}
}

// Regression test for RHOAIENG-34953: changing the orchestrator ConfigMap name
// (with identical content) must be detected as a change so the Deployment
// volume sources are updated.
func TestConfigMapNameChange_OrchestratorSameContent_RHOAIENG34953(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"
	content := "chat_generation:\n  service:\n    hostname: test\n    port: 8032\n"

	oldName := "config-old"
	newName := "config-new"

	reconciler := newTestReconciler(
		makeConfigMap(oldName, ns, content),
		makeConfigMap(newName, ns, content),
	)

	orchestrator := &gorchv1alpha1.GuardrailsOrchestrator{
		ObjectMeta: metav1.ObjectMeta{Name: "test-orch", Namespace: ns},
		Spec: gorchv1alpha1.GuardrailsOrchestratorSpec{
			OrchestratorConfig: &oldName,
		},
	}

	// Set initial annotations (simulates initial deployment creation)
	annotations := map[string]string{}
	changed := reconciler.setConfigMapHashAnnotations(ctx, orchestrator, annotations)
	assert.NotEmpty(t, changed, "Initial annotations should be set")
	assert.Equal(t, oldName, annotations["trustyai.opendatahub.io/orchestrator-config-name"],
		"Initial name annotation should match old ConfigMap name")

	// Change the ConfigMap name in the CR spec (same content)
	orchestrator.Spec.OrchestratorConfig = &newName
	changed = reconciler.setConfigMapHashAnnotations(ctx, orchestrator, annotations)

	assert.NotEmpty(t, changed,
		"ConfigMap name change must be detected even when content is identical")
	assert.Contains(t, changed, "Orchestrator ConfigMap")
	assert.Equal(t, newName, annotations["trustyai.opendatahub.io/orchestrator-config-name"],
		"Name annotation should be updated to the new ConfigMap name")

	expectedHash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
	assert.Equal(t, expectedHash, annotations["trustyai.opendatahub.io/orchestrator-config-hash"],
		"Content hash should remain correct after name change")
}

// Regression test for RHOAIENG-34953: changing the gateway ConfigMap name
// (with identical content) must be detected as a change.
func TestConfigMapNameChange_GatewaySameContent_RHOAIENG34953(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"
	orchContent := "chat_generation:\n  service:\n    hostname: test\n    port: 8032\n"
	gwContent := "gateway:\n  host: 0.0.0.0\n  port: 8033\n"

	orchName := "orch-config"
	oldGWName := "gateway-old"
	newGWName := "gateway-new"

	reconciler := newTestReconciler(
		makeConfigMap(orchName, ns, orchContent),
		makeConfigMap(oldGWName, ns, gwContent),
		makeConfigMap(newGWName, ns, gwContent),
	)

	orchestrator := &gorchv1alpha1.GuardrailsOrchestrator{
		ObjectMeta: metav1.ObjectMeta{Name: "test-orch", Namespace: ns},
		Spec: gorchv1alpha1.GuardrailsOrchestratorSpec{
			OrchestratorConfig:      &orchName,
			SidecarGatewayConfig:    &oldGWName,
			EnableGuardrailsGateway: true,
		},
	}

	// Set initial annotations
	annotations := map[string]string{}
	changed := reconciler.setConfigMapHashAnnotations(ctx, orchestrator, annotations)
	assert.NotEmpty(t, changed, "Initial annotations should be set")
	assert.Equal(t, oldGWName, annotations["trustyai.opendatahub.io/orchestrator-gateway-config-name"])

	// Change only the gateway ConfigMap name (same content)
	orchestrator.Spec.SidecarGatewayConfig = &newGWName
	changed = reconciler.setConfigMapHashAnnotations(ctx, orchestrator, annotations)

	assert.NotEmpty(t, changed,
		"Gateway ConfigMap name change must be detected even when content is identical")
	assert.Contains(t, changed, "Gateway ConfigMap")
	assert.Equal(t, newGWName, annotations["trustyai.opendatahub.io/orchestrator-gateway-config-name"],
		"Gateway name annotation should be updated")
	// Orchestrator should NOT be flagged as changed
	assert.NotContains(t, changed, "Orchestrator ConfigMap",
		"Orchestrator should not be flagged when only gateway name changes")
}

// Verifies that changing both name AND content is detected (existing behavior
// that must continue to work).
func TestConfigMapNameChange_DifferentContent(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"
	oldContent := "chat_generation:\n  service:\n    hostname: old-host\n    port: 8032\n"
	newContent := "chat_generation:\n  service:\n    hostname: new-host\n    port: 9090\n"

	oldName := "config-old"
	newName := "config-new"

	reconciler := newTestReconciler(
		makeConfigMap(oldName, ns, oldContent),
		makeConfigMap(newName, ns, newContent),
	)

	orchestrator := &gorchv1alpha1.GuardrailsOrchestrator{
		ObjectMeta: metav1.ObjectMeta{Name: "test-orch", Namespace: ns},
		Spec: gorchv1alpha1.GuardrailsOrchestratorSpec{
			OrchestratorConfig: &oldName,
		},
	}

	annotations := map[string]string{}
	reconciler.setConfigMapHashAnnotations(ctx, orchestrator, annotations)

	orchestrator.Spec.OrchestratorConfig = &newName
	changed := reconciler.setConfigMapHashAnnotations(ctx, orchestrator, annotations)

	assert.NotEmpty(t, changed, "Name + content change must be detected")
	assert.Equal(t, newName, annotations["trustyai.opendatahub.io/orchestrator-config-name"])

	expectedHash := fmt.Sprintf("%x", sha256.Sum256([]byte(newContent)))
	assert.Equal(t, expectedHash, annotations["trustyai.opendatahub.io/orchestrator-config-hash"])
}

// Idempotency: calling setConfigMapHashAnnotations with the same name and content
// must NOT report changes. This prevents spurious redeployments on every
// reconciliation cycle.
func TestConfigMapHashAnnotations_Idempotent(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"
	content := "chat_generation:\n  service:\n    hostname: test\n    port: 8032\n"
	cmName := "my-config"

	reconciler := newTestReconciler(makeConfigMap(cmName, ns, content))

	orchestrator := &gorchv1alpha1.GuardrailsOrchestrator{
		ObjectMeta: metav1.ObjectMeta{Name: "test-orch", Namespace: ns},
		Spec: gorchv1alpha1.GuardrailsOrchestratorSpec{
			OrchestratorConfig: &cmName,
		},
	}

	// First call sets annotations
	annotations := map[string]string{}
	changed := reconciler.setConfigMapHashAnnotations(ctx, orchestrator, annotations)
	assert.NotEmpty(t, changed, "First call should detect initial annotation setup")

	// Second call with no changes must return empty (idempotent)
	changed = reconciler.setConfigMapHashAnnotations(ctx, orchestrator, annotations)
	assert.Empty(t, changed,
		"Calling setConfigMapHashAnnotations again with same name+content must not report changes")

	// Third call — still idempotent
	changed = reconciler.setConfigMapHashAnnotations(ctx, orchestrator, annotations)
	assert.Empty(t, changed, "Must remain idempotent across multiple calls")
}

// Idempotency for gateway config: same name+content must not trigger changes.
func TestConfigMapHashAnnotations_GatewayIdempotent(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"
	orchContent := "chat_generation:\n  service:\n    hostname: test\n    port: 8032\n"
	gwContent := "gateway:\n  host: 0.0.0.0\n  port: 8033\n"
	orchName := "orch-config"
	gwName := "gw-config"

	reconciler := newTestReconciler(
		makeConfigMap(orchName, ns, orchContent),
		makeConfigMap(gwName, ns, gwContent),
	)

	orchestrator := &gorchv1alpha1.GuardrailsOrchestrator{
		ObjectMeta: metav1.ObjectMeta{Name: "test-orch", Namespace: ns},
		Spec: gorchv1alpha1.GuardrailsOrchestratorSpec{
			OrchestratorConfig:      &orchName,
			SidecarGatewayConfig:    &gwName,
			EnableGuardrailsGateway: true,
		},
	}

	annotations := map[string]string{}
	changed := reconciler.setConfigMapHashAnnotations(ctx, orchestrator, annotations)
	assert.NotEmpty(t, changed, "First call should set initial annotations")
	assert.Len(t, changed, 2, "Both orchestrator and gateway should be flagged on first call")

	changed = reconciler.setConfigMapHashAnnotations(ctx, orchestrator, annotations)
	assert.Empty(t, changed, "Second call must not report changes (idempotent)")
}

// Verifies that content-only changes (same name, different content) are still
// detected correctly — the original behavior must be preserved.
func TestConfigMapContentChange_SameName(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"
	oldContent := "chat_generation:\n  service:\n    hostname: old-host\n    port: 8032\n"
	newContent := "chat_generation:\n  service:\n    hostname: new-host\n    port: 9090\n"
	cmName := "my-config"

	// Start with old content
	reconciler := newTestReconciler(makeConfigMap(cmName, ns, oldContent))

	orchestrator := &gorchv1alpha1.GuardrailsOrchestrator{
		ObjectMeta: metav1.ObjectMeta{Name: "test-orch", Namespace: ns},
		Spec: gorchv1alpha1.GuardrailsOrchestratorSpec{
			OrchestratorConfig: &cmName,
		},
	}

	annotations := map[string]string{}
	reconciler.setConfigMapHashAnnotations(ctx, orchestrator, annotations)

	// Now create a new reconciler with updated content in the same-named ConfigMap
	reconciler = newTestReconciler(makeConfigMap(cmName, ns, newContent))
	changed := reconciler.setConfigMapHashAnnotations(ctx, orchestrator, annotations)

	assert.NotEmpty(t, changed, "Content change in same-named ConfigMap must be detected")
	assert.Contains(t, changed, "Orchestrator ConfigMap")
	assert.Equal(t, cmName, annotations["trustyai.opendatahub.io/orchestrator-config-name"],
		"Name annotation should remain the same")

	expectedHash := fmt.Sprintf("%x", sha256.Sum256([]byte(newContent)))
	assert.Equal(t, expectedHash, annotations["trustyai.opendatahub.io/orchestrator-config-hash"],
		"Hash should be updated to reflect new content")
}

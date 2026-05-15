package nemo_guardrails

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func setupTestObjects(ns string, mcpGatewayName string, mcpGatewayNamespace string) (client.Object, client.Object) {
	var mcpGatewayExtension client.Object
	var mcpGateway client.Object
	mcpGatewayExtension = &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "MCPGatewayExtension",
			"apiVersion": "mcp.kuadrant.io/v1alpha1",
			"metadata": map[string]interface{}{
				"name":      "test-mcp-gateway",
				"namespace": ns,
			},
			"spec": map[string]interface{}{
				"targetRef": map[string]interface{}{
					"name":      mcpGatewayName,
					"namespace": ns,
				},
			},
		},
	}
	mcpGateway = &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "Gateway",
			"apiVersion": "gateway.networking.k8s.io/v1",
			"metadata": map[string]interface{}{
				"name":      mcpGatewayName,
				"namespace": mcpGatewayNamespace,
			},
		},
	}
	return mcpGatewayExtension, mcpGateway
}

func setupTestReconciler(ns string, mcpGatewayExtension client.Object, mcpGateway client.Object) (*NemoGuardrailsReconciler, *nemoguardrailsv1alpha1.NemoGuardrails) {
	s := runtime.NewScheme()
	_ = nemoguardrailsv1alpha1.AddToScheme(s)
	gv := schema.GroupVersion{Group: "mcp.kuadrant.io", Version: "v1alpha1"}
	// Register the unstructured kinds with the scheme
	s.AddKnownTypeWithName(gv.WithKind("MCPGatewayExtension"), &unstructured.Unstructured{})
	s.AddKnownTypeWithName(gv.WithKind("MCPGatewayExtensionList"), &unstructured.UnstructuredList{})
	// Provide a REST mapper so the fake client can map GVK -> GVR
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{gv})
	mapper.Add(gv.WithKind("MCPGatewayExtension"), meta.RESTScopeNamespace)

	gatewayGV := schema.GroupVersion{Group: "gateway.networking.k8s.io", Version: "v1"}
	s.AddKnownTypeWithName(gatewayGV.WithKind("Gateway"), &unstructured.Unstructured{})
	s.AddKnownTypeWithName(gatewayGV.WithKind("GatewayList"), &unstructured.UnstructuredList{})
	mapper.Add(gatewayGV.WithKind("Gateway"), meta.RESTScopeNamespace)

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithRESTMapper(mapper).
		WithObjects(mcpGatewayExtension, mcpGateway).
		Build()

	reconciler := &NemoGuardrailsReconciler{
		Client:    fakeClient,
		Scheme:    s,
		Namespace: ns,
	}
	nemoGuardrails := &nemoguardrailsv1alpha1.NemoGuardrails{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-nemo-guardrails",
			Namespace: ns,
		},
		Spec: nemoguardrailsv1alpha1.NemoGuardrailsSpec{
			MCPGateway: &nemoguardrailsv1alpha1.MCPGatewayConfig{
				Name:      "test-mcp-gateway",
				Namespace: ns,
			},
		},
	}
	return reconciler, nemoGuardrails
}

func TestIsMCPGatewayPresentTrue(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"

	mcpGatewayExtension, mcpGateway := setupTestObjects(ns, "test-mcp-gateway", ns)
	reconciler, _ := setupTestReconciler(ns, mcpGatewayExtension, mcpGateway)
	found, err := reconciler.isMCPGatewayPresent(ctx, ns, "test-mcp-gateway")
	assert.True(t, found)
	assert.NoError(t, err)
}

func TestIsMCPGatewayPresentFalse(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"

	mcpGatewayExtension, mcpGateway := setupTestObjects(ns, "test-mcp-gateway", ns)
	reconciler, _ := setupTestReconciler(ns, mcpGatewayExtension, mcpGateway)
	found, err := reconciler.isMCPGatewayPresent(ctx, ns, "does-not-exist")

	assert.False(t, found)
	assert.NoError(t, err)
}

func TestDiscoverMCPGateway(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"

	mcpGatewayExtension, mcpGateway := setupTestObjects(ns, "test-mcp-gateway", ns)
	reconciler, nemoGuardrails := setupTestReconciler(ns, mcpGatewayExtension, mcpGateway)
	mcpGatewayRef, mcpGatewayStatus := reconciler.discoverMCPGateway(ctx, ns, "", nemoGuardrails)
	assert.NotNil(t, mcpGatewayRef)
	assert.True(t, mcpGatewayStatus.MCPGatewayFound)
	assert.Equal(t, mcpGatewayRef.Name, "test-mcp-gateway")
	assert.Equal(t, mcpGatewayRef.Namespace, ns)
}

func TestDiscoverMCPGatewayWithGatewayNotFound(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"
	name := "test-mcp-gateway"

	gatewayNameOverride := "test-mcp-gateway-not-found"
	mcpGatewayExtension, mcpGateway := setupTestObjects(ns, name, ns)
	reconciler, nemoGuardrails := setupTestReconciler(ns, mcpGatewayExtension, mcpGateway)
	mcpGatewayRef, mcpGatewayStatus := reconciler.discoverMCPGateway(ctx, ns, gatewayNameOverride, nemoGuardrails)
	assert.Nil(t, mcpGatewayRef)
	assert.False(t, mcpGatewayStatus.MCPGatewayFound)
	assert.Equal(t, mcpGatewayStatus.MCPGatewayError, fmt.Sprintf("MCP gateway not found: %s", gatewayNameOverride))
}

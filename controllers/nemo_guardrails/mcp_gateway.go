package nemo_guardrails

import (
	"context"
	"fmt"

	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var mcpGatewayExtensionGVK = schema.GroupVersionKind{
	Group:   "mcp.kuadrant.io",
	Version: "v1alpha1",
	Kind:    "MCPGatewayExtensionList",
}

// MCPGatewayRef holds the resolved Gateway reference from an MCPGatewayExtension.
type MCPGatewayRef struct {
	Name      string
	Namespace string
}

// discoverMCPGateway searches for MCPGatewayExtension resources in the given
// namespace to find the gateway they target. If gatewayNameOverride is
// non-empty, only extensions whose spec.targetRef.name matches are considered;
// otherwise the first extension found is used.
func (r *NemoGuardrailsReconciler) discoverMCPGateway(ctx context.Context, namespace string, gatewayNameOverride string) (*MCPGatewayRef, *nemoguardrailsv1alpha1.MCPGatewayStatus) {
	logger := log.FromContext(ctx)

	mcpGatewayStatus := &nemoguardrailsv1alpha1.MCPGatewayStatus{
		MCPGatewayFound: false,
	}

	extensionList := &unstructured.UnstructuredList{}
	extensionList.SetGroupVersionKind(mcpGatewayExtensionGVK)

	if err := r.List(ctx, extensionList, &client.ListOptions{Namespace: namespace}); err != nil {
		if meta.IsNoMatchError(err) {
			logger.Info("MCPGatewayExtension CRD not installed, skipping MCP gateway discovery")
			mcpGatewayStatus.MCPGatewayError = err.Error()
			return nil, mcpGatewayStatus
		}
		mcpGatewayStatus.MCPGatewayError = err.Error()
		return nil, mcpGatewayStatus
	}

	for _, ext := range extensionList.Items {
		targetRef, found, err := unstructured.NestedMap(ext.Object, "spec", "targetRef")
		if err != nil || !found {
			continue
		}

		refName, _, _ := unstructured.NestedString(targetRef, "name")
		if refName == "" {
			logger.V(1).Info(
				"MCPGatewayExtension targetRef.name is empty, skipping",
				"extension", ext.GetName(),
			)
			continue
		}
		if gatewayNameOverride != "" && refName != gatewayNameOverride {
			continue
		}

		refNamespace, _, _ := unstructured.NestedString(targetRef, "namespace")
		if refNamespace == "" {
			refNamespace = ext.GetNamespace()
			logger.Info(
				"MCPGatewayExtension targetRef namespace is empty, using extension namespace",
				"extension", ext.GetName(),
				"namespace", refNamespace,
			)
		}

		found, err = r.isMCPGatewayPresent(ctx, refNamespace, refName)
		if err != nil {
			mcpGatewayStatus.MCPGatewayError = err.Error()
			return nil, mcpGatewayStatus
		}
		if !found {
			continue
		}

		logger.Info("Discovered MCP gateway via MCPGatewayExtension",
			"extension", ext.GetName(),
			"gatewayName", refName,
			"gatewayNamespace", refNamespace,
		)
		mcpGatewayStatus.MCPGatewayFound = true
		return &MCPGatewayRef{
			Name:      refName,
			Namespace: refNamespace,
		}, mcpGatewayStatus
	}

	if gatewayNameOverride != "" {
		logger.Info("No MCPGatewayExtension found targeting gateway", "gatewayName", gatewayNameOverride)
		mcpGatewayStatus.MCPGatewayError = fmt.Sprintf("MCP gateway not found: %s", gatewayNameOverride)
	} else {
		logger.V(1).Info("No MCPGatewayExtension found in namespace", "namespace", namespace)
		mcpGatewayStatus.MCPGatewayError = fmt.Sprintf("MCP gateway not found in namespace: %s", namespace)
	}
	return nil, mcpGatewayStatus
}

func (r *NemoGuardrailsReconciler) isMCPGatewayPresent(ctx context.Context, namespace string, name string) (bool, error) {
	mcpGateway := &unstructured.Unstructured{}
	mcpGateway.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "gateway.networking.k8s.io",
		Version: "v1",
		Kind:    "Gateway",
	})
	if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, mcpGateway); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

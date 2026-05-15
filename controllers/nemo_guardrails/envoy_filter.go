package nemo_guardrails

import (
	"context"
	"fmt"
	"reflect"

	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/nemo_guardrails/templates"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	bbrSubFilterName        = "envoy.filters.http.ext_proc.bbr"
	envoyFilterTemplatePath = "envoy-filter.tmpl.yaml"
)

type EnvoyFilterConfig struct {
	Name            string
	TargetName      string
	TargetNamespace string
}

// isBBRPluginPresent lists EnvoyFilter resources in the given namespace and
// returns true if any of them contain a configPatch whose patch.value.name
// matches the BBR ext_proc filter name.
func (r *NemoGuardrailsReconciler) isBBRPluginPresent(ctx context.Context, gatewayNamespace string) *nemoguardrailsv1alpha1.BBRPluginStatus {
	logger := log.FromContext(ctx)

	bbrPluginStatus := &nemoguardrailsv1alpha1.BBRPluginStatus{
		BBRPluginFound: false,
	}

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1alpha3",
		Kind:    "EnvoyFilterList",
	})

	if err := r.List(ctx, list, &client.ListOptions{Namespace: gatewayNamespace}); err != nil {
		if meta.IsNoMatchError(err) {
			logger.Info("EnvoyFilter CRD not installed, skipping BBR check")
			bbrPluginStatus.BBRPluginError = err.Error()
			return bbrPluginStatus
		}
		bbrPluginStatus.BBRPluginError = err.Error()
		return bbrPluginStatus
	}

	for _, ef := range list.Items {
		patches, found, _ := unstructured.NestedSlice(ef.Object, "spec", "configPatches")
		if !found {
			continue
		}
		for _, p := range patches {
			patch, ok := p.(map[string]interface{})
			if !ok {
				continue
			}
			name, _, _ := unstructured.NestedString(patch, "patch", "value", "name")
			if name == bbrSubFilterName {
				bbrPluginStatus.BBRPluginFound = true
				return bbrPluginStatus
			}
		}
	}
	return bbrPluginStatus
}

const envoyFilterName = "mcp-sse-strip"

func (r *NemoGuardrailsReconciler) deleteEnvoyFilter(ctx context.Context, namespace string) error {
	logger := log.FromContext(ctx)

	existing := &unstructured.Unstructured{}
	existing.SetKind("EnvoyFilter")
	existing.SetAPIVersion("networking.istio.io/v1alpha3")

	err := r.Get(ctx, types.NamespacedName{Name: envoyFilterName, Namespace: namespace}, existing)
	if errors.IsNotFound(err) || meta.IsNoMatchError(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to look up EnvoyFilter for deletion: %v", err)
	}

	logger.Info("Deleting managed EnvoyFilter", "name", envoyFilterName, "namespace", namespace)
	if err := r.Delete(ctx, existing); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete EnvoyFilter: %v", err)
	}
	return nil
}

func (r *NemoGuardrailsReconciler) ensureEnvoyFilter(ctx context.Context, instance *nemoguardrailsv1alpha1.NemoGuardrails, mcpGatewayName string, mcpGatewayNamespace string) error {
	logger := log.FromContext(ctx)

	existing := &unstructured.Unstructured{}
	existing.SetKind("EnvoyFilter")
	existing.SetAPIVersion("networking.istio.io/v1alpha3")

	err := r.Get(ctx, types.NamespacedName{Name: envoyFilterName, Namespace: mcpGatewayNamespace}, existing)
	if err == nil {
		return nil
	}
	if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check for existing EnvoyFilter: %v", err)
	}

	config := EnvoyFilterConfig{
		Name:            envoyFilterName,
		TargetName:      mcpGatewayName,
		TargetNamespace: mcpGatewayNamespace,
	}

	envoyFilter, err := templateParser.ParseResource[*unstructured.Unstructured](envoyFilterTemplatePath, config, reflect.TypeOf(&unstructured.Unstructured{}))
	if err != nil {
		logger.Error(err, "could not parse the EnvoyFilter template")
		return err
	}

	if instance.Namespace == mcpGatewayNamespace {
		if err := ctrl.SetControllerReference(instance, envoyFilter, r.Scheme); err != nil {
			return err
		}
	} else {
		logger.Info("Skipping ownerReference for cross-namespace EnvoyFilter",
			"crNamespace", instance.Namespace, "envoyFilterNamespace", mcpGatewayNamespace)
	}

	logger.Info("Creating EnvoyFilter", "name", envoyFilterName, "namespace", mcpGatewayNamespace)
	if err := r.Create(ctx, envoyFilter); err != nil {
		return fmt.Errorf("failed to create EnvoyFilter: %v", err)
	}

	return nil
}

package trustyaimodule

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// cleanupModuleResources tears down everything the module operator deployed:
// namespace-scoped operands, the DSC ConfigMap, and cluster-scoped RBAC that
// cannot rely on owner-reference GC.
func (r *TrustyAIModuleReconciler) cleanupModuleResources(ctx context.Context) error {
	if err := r.deleteDeployedOperands(ctx); err != nil {
		return err
	}
	if err := r.deleteDSCConfigMap(ctx); err != nil {
		return err
	}
	if err := r.deleteClusterScopedRBAC(ctx); err != nil {
		return err
	}
	return nil
}

func (r *TrustyAIModuleReconciler) deleteDeployedOperands(ctx context.Context) error {
	if r.Deployer == nil {
		return nil
	}

	logger := log.FromContext(ctx)

	resources, err := RenderManifests(ctx, r.ManifestsTemplatePath, r.Namespace)
	if err != nil {
		return fmt.Errorf("rendering manifests for operand cleanup: %w", err)
	}

	for i := len(resources) - 1; i >= 0; i-- {
		resource := &resources[i]
		if shouldSkipOperandDeletion(resource) {
			continue
		}

		lookup := &unstructured.Unstructured{}
		lookup.SetGroupVersionKind(resource.GroupVersionKind())
		key := types.NamespacedName{Name: resource.GetName(), Namespace: resource.GetNamespace()}
		if err := r.Get(ctx, key, lookup); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("failed to get %s %s for deletion: %w", resource.GetKind(), resource.GetName(), err)
		}

		logger.Info("Deleting deployed operand",
			"kind", resource.GetKind(),
			"name", resource.GetName(),
			"namespace", resource.GetNamespace(),
		)
		if err := r.Delete(ctx, lookup); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete %s %s: %w", resource.GetKind(), resource.GetName(), err)
		}
	}

	return nil
}

func shouldSkipOperandDeletion(resource *unstructured.Unstructured) bool {
	switch resource.GetKind() {
	case "CustomResourceDefinition", "ClusterRole", "ClusterRoleBinding":
		return true
	default:
		return false
	}
}

package trustyaimodule

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// deleteClusterScopedRBAC deletes the ClusterRole/ClusterRoleBinding objects
// rendered from config/manifests-template/base/rbac. These are excluded from
// owner-reference-based ownership (see cmd/trustyai-operator-module/main.go),
// so they are not garbage collected when the TrustyAI CR is deleted and must
// be removed explicitly here.
func (r *TrustyAIModuleReconciler) deleteClusterScopedRBAC(ctx context.Context) error {
	logger := log.FromContext(ctx)

	// Cleanup must cover the complete normal manifest set, including resources
	// that are absent from the MCP-only overlay.
	resources, err := RenderManifests(ctx, r.ManifestsTemplatePath, r.Namespace, false)
	if err != nil {
		return fmt.Errorf("rendering manifests for cluster-scoped RBAC cleanup: %w", err)
	}

	for i := range resources {
		resource := &resources[i]
		if resource.GetKind() != "ClusterRole" && resource.GetKind() != "ClusterRoleBinding" {
			continue
		}

		lookup := &unstructured.Unstructured{}
		lookup.SetGroupVersionKind(resource.GroupVersionKind())
		if err := r.Get(ctx, types.NamespacedName{Name: resource.GetName()}, lookup); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("failed to get %s %s for deletion: %w", resource.GetKind(), resource.GetName(), err)
		}
		logger.Info("Deleting cluster-scoped RBAC", "kind", resource.GetKind(), "name", resource.GetName())
		if err := r.Delete(ctx, lookup); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete %s %s: %w", resource.GetKind(), resource.GetName(), err)
		}
	}

	return nil
}

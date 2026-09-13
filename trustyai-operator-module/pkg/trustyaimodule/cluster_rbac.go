package trustyaimodule

import (
	"context"
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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

	for _, name := range clusterRoleBindingNames {
		crb := &rbacv1.ClusterRoleBinding{}
		if err := r.Get(ctx, types.NamespacedName{Name: name}, crb); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("failed to get ClusterRoleBinding %s for deletion: %w", name, err)
		}
		logger.Info("Deleting ClusterRoleBinding", "name", name)
		if err := r.Delete(ctx, crb); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete ClusterRoleBinding %s: %w", name, err)
		}
	}

	for _, name := range clusterRoleNames {
		cr := &rbacv1.ClusterRole{}
		if err := r.Get(ctx, types.NamespacedName{Name: name}, cr); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("failed to get ClusterRole %s for deletion: %w", name, err)
		}
		logger.Info("Deleting ClusterRole", "name", name)
		if err := r.Delete(ctx, cr); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete ClusterRole %s: %w", name, err)
		}
	}

	return nil
}

package gorch

import (
	"context"
	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// createClusterRoleBinding creates a cluster role binding for the orchestrator oauth service account
func (r *GuardrailsOrchestratorReconciler) createClusterRoleBinding(orchestrator *gorchv1alpha1.GuardrailsOrchestrator) *rbacv1.ClusterRoleBinding {
	crb := utils.CreateAuthDelegatorClusterRoleBinding(orchestrator)
	crb.ObjectMeta.Labels["guardrailsorchestrator"] = orchestrator.Name
	crb.ObjectMeta.Labels["guardrailsorchestrator-ns"] = orchestrator.Namespace
	return crb
}

// cleanupClusterRoleBinding deletes the oauth cluster role upon orchestrator deletion
func (r *GuardrailsOrchestratorReconciler) cleanupClusterRoleBinding(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) error {
	crbName := utils.GetAuthDelegatorClusterRoleName(orchestrator)
	crb := &rbacv1.ClusterRoleBinding{}
	err := r.Get(ctx, types.NamespacedName{Name: crbName}, crb)
	if err == nil {
		if delErr := r.Delete(ctx, crb); delErr != nil {
			log.FromContext(ctx).Error(delErr, "Failed to delete ClusterRoleBinding", "ClusterRoleBinding.Name", crbName)
			return err
		}
		return nil
	} else if !errors.IsNotFound(err) {
		log.FromContext(ctx).Error(err, "Failed to get ClusterRoleBinding for deletion", "ClusterRoleBinding.Name", crbName)
		return err
	} else {
		return err
	}
}

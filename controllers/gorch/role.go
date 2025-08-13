package gorch

import (
	"context"
	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// getClusterRoleName creates an auth cluster role name from the orchestrator name and namespace
func getClusterRoleName(orchestrator *gorchv1alpha1.GuardrailsOrchestrator) string {
	return orchestrator.Name + "-" + orchestrator.Namespace + "-auth-delegator"
}

// createClusterRoleBinding creates a cluster role binding for the orchestrator oauth service account
func (r *GuardrailsOrchestratorReconciler) createClusterRoleBinding(orchestrator *gorchv1alpha1.GuardrailsOrchestrator, serviceAccountName string) *rbacv1.ClusterRoleBinding {
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: getClusterRoleName(orchestrator),
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "trustyai-service-operator",
				"guardrailsorchestrator":       orchestrator.Name,
				"guardrailsorchestrator-ns":    orchestrator.Namespace,
			},
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      serviceAccountName,
				Namespace: orchestrator.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     "system:auth-delegator",
			APIGroup: rbacv1.GroupName,
		},
	}
	return crb
}

// cleanupClusterRoleBinding deletes the oauth cluster role upon orchestrator deletion
func (r *GuardrailsOrchestratorReconciler) cleanupClusterRoleBinding(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) error {
	crbName := getClusterRoleName(orchestrator)
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

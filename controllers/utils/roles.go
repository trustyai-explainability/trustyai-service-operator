package utils

import (
	"context"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	clusterRoleBindingResourceKind = "ClusterRoleBinding"
)

// getClusterRoleName creates an auth cluster role name from the orchestrator name and namespace
func GetAuthDelegatorClusterRoleName(owner metav1.Object) string {
	return owner.GetName() + "-" + owner.GetNamespace() + "-auth-delegator"
}

// createClusterRoleBinding creates a cluster role binding for the orchestrator oauth service account
func CreateAuthDelegatorClusterRoleBinding(owner metav1.Object) *rbacv1.ClusterRoleBinding {
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: GetAuthDelegatorClusterRoleName(owner),
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "trustyai-service-operator",
			},
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      GetServiceAccountName(owner),
				Namespace: owner.GetNamespace(),
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

func ReconcileClusterRoleBinding(ctx context.Context, c client.Client, crb *rbacv1.ClusterRoleBinding) error {
	_, _, err := ReconcileGenericManuallyDefined[*rbacv1.ClusterRoleBinding](ctx, c, clusterRoleBindingResourceKind, crb)
	return err
}

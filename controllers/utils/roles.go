package utils

import (
	"context"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
func createAuthDelegatorClusterRoleBinding(owner metav1.Object) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetAuthDelegatorClusterRoleName(owner),
			Namespace: owner.GetNamespace(),
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "trustyai-service-operator",
				"app":                          owner.GetName(),
				"component":                    owner.GetNamespace(),
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
}

func ReconcileClusterRoleBinding(ctx context.Context, c client.Client, owner metav1.Object, crb *rbacv1.ClusterRoleBinding) error {
	_, _, err := ReconcileGenericManuallyDefined[*rbacv1.ClusterRoleBinding](ctx, c, clusterRoleBindingResourceKind, owner, crb)
	return err
}

func ReconcileAuthDelegatorClusterRoleBinding(ctx context.Context, c client.Client, owner metav1.Object) error {
	return ReconcileClusterRoleBinding(ctx, c, owner, createAuthDelegatorClusterRoleBinding(owner))
}

// cleanupClusterRoleBinding deletes the oauth cluster role upon orchestrator deletion
func CleanupClusterRoleBinding(ctx context.Context, c client.Client, owner metav1.Object) error {
	crbName := GetAuthDelegatorClusterRoleName(owner)
	crb := &rbacv1.ClusterRoleBinding{}
	LogInfoVerb(ctx, "deleting", clusterRoleBindingResourceKind, crbName, owner.GetName())
	err := c.Get(ctx, types.NamespacedName{Name: crbName}, crb)
	if err == nil {
		if delErr := c.Delete(ctx, crb); delErr != nil {
			LogErrorVerb(ctx, err, "deleting", clusterRoleBindingResourceKind, crbName, crb.GetName())
			return err
		}
		return nil
	} else if !errors.IsNotFound(err) {
		LogErrorRetrieving(ctx, err, clusterRoleBindingResourceKind, crbName, crb.GetName())
		return err
	} else {
		// ignore if not found
		return nil
	}
}

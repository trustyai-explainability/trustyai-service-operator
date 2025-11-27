package utils

import (
	"context"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	clusterRoleBindingResourceKind = "ClusterRoleBinding"
)

func ReconcileClusterRoleBinding(ctx context.Context, c client.Client, crb *rbacv1.ClusterRoleBinding) error {
	_, _, err := ReconcileGenericManuallyDefined[*rbacv1.ClusterRoleBinding](ctx, c, clusterRoleBindingResourceKind, crb)
	return err
}

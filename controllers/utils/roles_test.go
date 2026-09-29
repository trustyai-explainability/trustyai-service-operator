package utils

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestReconcileAuthDelegatorUpdatesSubjectNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, rbacv1.AddToScheme(scheme))
	owner := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "nemo-guardrails", Namespace: "control-plane", UID: "cr-uid"}}
	existing := createAuthDelegatorClusterRoleBindingInNamespace(owner, "models")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()

	require.NoError(t, ReconcileAuthDelegatorClusterRoleBindingInNamespace(context.Background(), c, owner, "other"))

	got := &rbacv1.ClusterRoleBinding{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: existing.Name, Namespace: existing.Namespace}, got))
	require.Equal(t, "other", got.Subjects[0].Namespace)
	require.Equal(t, "system:auth-delegator", got.RoleRef.Name)
	require.Equal(t, "ClusterRole", got.RoleRef.Kind)
}

package evalhub

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	evalhubv1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func singleTenantEvalHub(name, ns string) *evalhubv1.EvalHub {
	return &evalhubv1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       evalhubv1.EvalHubSpec{Tenancy: evalhubv1.TenancySingle},
	}
}

func multiTenantEvalHub(name, ns string) *evalhubv1.EvalHub {
	return &evalhubv1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       evalhubv1.EvalHubSpec{Tenancy: evalhubv1.TenancyMulti},
	}
}

func newRBACScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, rbacv1.AddToScheme(s))
	require.NoError(t, evalhubv1.AddToScheme(s))
	return s
}

func TestReconcileSingleTenancyRoles_SingleMode(t *testing.T) {
	ctx := context.Background()
	s := newRBACScheme(t)
	instance := singleTenantEvalHub("my-evalhub", "team-a")

	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(instance).Build()
	r := &EvalHubReconciler{Client: fc, Scheme: s, EventRecorder: record.NewFakeRecorder(10)}

	err := r.reconcileSingleTenancyRoles(ctx, instance)
	require.NoError(t, err)

	t.Run("creates admin Role", func(t *testing.T) {
		role := &rbacv1.Role{}
		require.NoError(t, fc.Get(ctx, types.NamespacedName{Name: tenantAdminRoleName, Namespace: "team-a"}, role))
		assert.NotEmpty(t, role.Rules)
	})

	t.Run("creates user Role", func(t *testing.T) {
		role := &rbacv1.Role{}
		require.NoError(t, fc.Get(ctx, types.NamespacedName{Name: tenantUserRoleName, Namespace: "team-a"}, role))
		assert.NotEmpty(t, role.Rules)
	})

	t.Run("creates admin RoleBinding", func(t *testing.T) {
		rb := &rbacv1.RoleBinding{}
		require.NoError(t, fc.Get(ctx, types.NamespacedName{Name: tenantAdminBindingName, Namespace: "team-a"}, rb))
		assert.Equal(t, tenantAdminRoleName, rb.RoleRef.Name)
		require.Len(t, rb.Subjects, 1)
		assert.Equal(t, "system:serviceaccounts:team-a", rb.Subjects[0].Name)
	})
}

func TestReconcileSingleTenancyRoles_MultiMode(t *testing.T) {
	ctx := context.Background()
	s := newRBACScheme(t)
	instance := multiTenantEvalHub("my-evalhub", "ctrl-plane")

	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(instance).Build()
	r := &EvalHubReconciler{Client: fc, Scheme: s, EventRecorder: record.NewFakeRecorder(10)}

	require.NoError(t, r.reconcileSingleTenancyRoles(ctx, instance))

	t.Run("does not create Roles in multi mode", func(t *testing.T) {
		role := &rbacv1.Role{}
		err := fc.Get(ctx, types.NamespacedName{Name: tenantAdminRoleName, Namespace: "ctrl-plane"}, role)
		assert.True(t, errors.IsNotFound(err))
	})
}

func TestReconcileSingleTenancyRoles_Idempotent(t *testing.T) {
	ctx := context.Background()
	s := newRBACScheme(t)
	instance := singleTenantEvalHub("my-evalhub", "team-a")

	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(instance).Build()
	r := &EvalHubReconciler{Client: fc, Scheme: s, EventRecorder: record.NewFakeRecorder(10)}

	require.NoError(t, r.reconcileSingleTenancyRoles(ctx, instance))
	require.NoError(t, r.reconcileSingleTenancyRoles(ctx, instance), "second reconcile must not error")
}

func TestReconcileSingleTenancyRoles_SwitchSingleToMulti(t *testing.T) {
	ctx := context.Background()
	s := newRBACScheme(t)
	instance := singleTenantEvalHub("my-evalhub", "team-a")

	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(instance).Build()
	r := &EvalHubReconciler{Client: fc, Scheme: s, EventRecorder: record.NewFakeRecorder(10)}

	// Create Roles in single mode.
	require.NoError(t, r.reconcileSingleTenancyRoles(ctx, instance))

	// Switch to multi mode.
	instance.Spec.Tenancy = evalhubv1.TenancyMulti
	require.NoError(t, r.reconcileSingleTenancyRoles(ctx, instance))

	t.Run("removes Roles after switch to multi", func(t *testing.T) {
		role := &rbacv1.Role{}
		err := fc.Get(ctx, types.NamespacedName{Name: tenantAdminRoleName, Namespace: "team-a"}, role)
		assert.True(t, errors.IsNotFound(err))
		err = fc.Get(ctx, types.NamespacedName{Name: tenantUserRoleName, Namespace: "team-a"}, role)
		assert.True(t, errors.IsNotFound(err))
	})

	t.Run("removes RoleBinding after switch to multi", func(t *testing.T) {
		rb := &rbacv1.RoleBinding{}
		err := fc.Get(ctx, types.NamespacedName{Name: tenantAdminBindingName, Namespace: "team-a"}, rb)
		assert.True(t, errors.IsNotFound(err))
	})
}

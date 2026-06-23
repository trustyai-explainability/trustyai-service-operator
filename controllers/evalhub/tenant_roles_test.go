package evalhub

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	evalhubv1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTenantRolesReconciler(t *testing.T, instance *evalhubv1.EvalHub) *EvalHubReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, evalhubv1.AddToScheme(scheme))
	require.NoError(t, rbacv1.AddToScheme(scheme))

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(instance).
		Build()

	return &EvalHubReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		Namespace:     instance.Namespace,
		EventRecorder: record.NewFakeRecorder(10),
	}
}

func singleTenantInstance(name, namespace string) *evalhubv1.EvalHub {
	return &evalhubv1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: evalhubv1.EvalHubSpec{
			Tenancy: evalhubv1.TenancySingle,
		},
	}
}

func multiTenantInstance(name, namespace string) *evalhubv1.EvalHub {
	return &evalhubv1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: evalhubv1.EvalHubSpec{
			Tenancy: evalhubv1.TenancyMulti,
		},
	}
}

func TestReconcileSingleTenancyRolesCreatesRolesInSingleMode(t *testing.T) {
	ctx := context.Background()
	instance := singleTenantInstance("my-evalhub", "team-a")
	r := newTenantRolesReconciler(t, instance)

	err := r.reconcileSingleTenancyRoles(ctx, instance)
	require.NoError(t, err)

	// Verify tenant-admin Role
	adminRole := &rbacv1.Role{}
	err = r.Get(ctx, types.NamespacedName{Name: tenantAdminRoleName, Namespace: "team-a"}, adminRole)
	require.NoError(t, err)
	assert.Equal(t, tenantAdminRoleName, adminRole.Name)
	assert.True(t, len(adminRole.Rules) > 0, "tenant-admin Role should have rules")

	// Verify tenant-user Role
	userRole := &rbacv1.Role{}
	err = r.Get(ctx, types.NamespacedName{Name: tenantUserRoleName, Namespace: "team-a"}, userRole)
	require.NoError(t, err)
	assert.Equal(t, tenantUserRoleName, userRole.Name)
	assert.True(t, len(userRole.Rules) > 0, "tenant-user Role should have rules")

	// Verify tenant-admin-binding RoleBinding
	rb := &rbacv1.RoleBinding{}
	err = r.Get(ctx, types.NamespacedName{Name: tenantAdminBindingName, Namespace: "team-a"}, rb)
	require.NoError(t, err)
	assert.Equal(t, tenantAdminRoleName, rb.RoleRef.Name)
	assert.Equal(t, "Role", rb.RoleRef.Kind)
	require.Len(t, rb.Subjects, 1)
	assert.Equal(t, "Group", rb.Subjects[0].Kind)
	assert.Equal(t, "system:serviceaccounts:team-a", rb.Subjects[0].Name)
}

func TestReconcileSingleTenancyRolesSkipsInMultiMode(t *testing.T) {
	ctx := context.Background()
	instance := multiTenantInstance("my-evalhub", "team-b")
	r := newTenantRolesReconciler(t, instance)

	err := r.reconcileSingleTenancyRoles(ctx, instance)
	require.NoError(t, err)

	// Verify no Roles or RoleBindings were created
	adminRole := &rbacv1.Role{}
	err = r.Get(ctx, types.NamespacedName{Name: tenantAdminRoleName, Namespace: "team-b"}, adminRole)
	assert.True(t, err != nil, "tenant-admin Role should not exist in multi mode")

	userRole := &rbacv1.Role{}
	err = r.Get(ctx, types.NamespacedName{Name: tenantUserRoleName, Namespace: "team-b"}, userRole)
	assert.True(t, err != nil, "tenant-user Role should not exist in multi mode")

	rb := &rbacv1.RoleBinding{}
	err = r.Get(ctx, types.NamespacedName{Name: tenantAdminBindingName, Namespace: "team-b"}, rb)
	assert.True(t, err != nil, "tenant-admin-binding should not exist in multi mode")
}

func TestReconcileSingleTenancyRolesCleanupOnModeSwitch(t *testing.T) {
	ctx := context.Background()
	instance := singleTenantInstance("my-evalhub", "team-c")
	r := newTenantRolesReconciler(t, instance)

	// First: create Roles in single mode
	err := r.reconcileSingleTenancyRoles(ctx, instance)
	require.NoError(t, err)

	// Verify they exist
	adminRole := &rbacv1.Role{}
	require.NoError(t, r.Get(ctx, types.NamespacedName{Name: tenantAdminRoleName, Namespace: "team-c"}, adminRole))

	// Switch to multi mode
	instance.Spec.Tenancy = evalhubv1.TenancyMulti
	err = r.reconcileSingleTenancyRoles(ctx, instance)
	require.NoError(t, err)

	// Verify cleanup
	err = r.Get(ctx, types.NamespacedName{Name: tenantAdminRoleName, Namespace: "team-c"}, adminRole)
	assert.True(t, err != nil, "tenant-admin Role should be deleted after switch to multi")

	userRole := &rbacv1.Role{}
	err = r.Get(ctx, types.NamespacedName{Name: tenantUserRoleName, Namespace: "team-c"}, userRole)
	assert.True(t, err != nil, "tenant-user Role should be deleted after switch to multi")

	rb := &rbacv1.RoleBinding{}
	err = r.Get(ctx, types.NamespacedName{Name: tenantAdminBindingName, Namespace: "team-c"}, rb)
	assert.True(t, err != nil, "tenant-admin-binding should be deleted after switch to multi")
}

func TestTenantAdminRoleRulesAreMinimalAndSufficient(t *testing.T) {
	rules := tenantAdminRules()

	ruleMap := make(map[string][]string)
	for _, rule := range rules {
		for _, res := range rule.Resources {
			key := rule.APIGroups[0] + "/" + res
			ruleMap[key] = rule.Verbs
		}
	}

	// Verify expected resources are present
	assert.Contains(t, ruleMap, "trustyai.opendatahub.io/evalhubs")
	assert.Contains(t, ruleMap, "trustyai.opendatahub.io/evalhubs/proxy")
	assert.Contains(t, ruleMap, "trustyai.opendatahub.io/providers")
	assert.Contains(t, ruleMap, "trustyai.opendatahub.io/collections")
	assert.Contains(t, ruleMap, "trustyai.opendatahub.io/status-events")
	assert.Contains(t, ruleMap, "mlflow.kubeflow.org/experiments")

	// Verify collections has full CRUD
	assert.ElementsMatch(t, []string{"get", "list", "create", "update", "patch", "delete"}, ruleMap["trustyai.opendatahub.io/collections"])

	// Verify providers is read-only
	assert.ElementsMatch(t, []string{"get", "list"}, ruleMap["trustyai.opendatahub.io/providers"])

	// Verify no unexpected resources
	assert.Len(t, ruleMap, 6, "tenant-admin should have exactly 6 resource rules")
}

func TestTenantUserRoleRulesAreMinimalAndSufficient(t *testing.T) {
	rules := tenantUserRules()

	ruleMap := make(map[string][]string)
	for _, rule := range rules {
		for _, res := range rule.Resources {
			key := rule.APIGroups[0] + "/" + res
			ruleMap[key] = rule.Verbs
		}
	}

	// Verify collections is read-only (no create/update/patch/delete)
	assert.ElementsMatch(t, []string{"get", "list"}, ruleMap["trustyai.opendatahub.io/collections"])

	// Verify proxy includes create (for submitting evaluations)
	assert.ElementsMatch(t, []string{"get", "create"}, ruleMap["trustyai.opendatahub.io/evalhubs/proxy"])

	// Verify MLflow includes create
	assert.ElementsMatch(t, []string{"get", "list", "create"}, ruleMap["mlflow.kubeflow.org/experiments"])

	// Verify no unexpected resources
	assert.Len(t, ruleMap, 6, "tenant-user should have exactly 6 resource rules")
}

func TestReconcileSingleTenancyRolesIdempotent(t *testing.T) {
	ctx := context.Background()
	instance := singleTenantInstance("my-evalhub", "team-d")
	r := newTenantRolesReconciler(t, instance)

	// Run twice — second call should be a no-op
	require.NoError(t, r.reconcileSingleTenancyRoles(ctx, instance))
	require.NoError(t, r.reconcileSingleTenancyRoles(ctx, instance))

	// Verify resources still exist and are correct
	adminRole := &rbacv1.Role{}
	require.NoError(t, r.Get(ctx, types.NamespacedName{Name: tenantAdminRoleName, Namespace: "team-d"}, adminRole))
	assert.True(t, equalPolicyRules(adminRole.Rules, tenantAdminRules()))
}

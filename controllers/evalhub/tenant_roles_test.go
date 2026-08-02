package evalhub

import (
	"context"
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	evalhubv1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	corev1 "k8s.io/api/core/v1"
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

	t.Run("admin Role grants evalhubs read for CR discovery", func(t *testing.T) {
		role := &rbacv1.Role{}
		require.NoError(t, fc.Get(ctx, types.NamespacedName{Name: tenantAdminRoleName, Namespace: "team-a"}, role))
		found := false
		for _, rule := range role.Rules {
			if len(rule.Resources) == 1 && rule.Resources[0] == "evalhubs" {
				assert.Contains(t, rule.Verbs, "get")
				assert.Contains(t, rule.Verbs, "list")
				assert.Empty(t, rule.ResourceNames, "must not scope by resourceNames so list works")
				found = true
			}
		}
		assert.True(t, found, "admin Role must include an evalhubs rule for BFF discovery")
	})

	t.Run("creates user Role", func(t *testing.T) {
		role := &rbacv1.Role{}
		require.NoError(t, fc.Get(ctx, types.NamespacedName{Name: tenantUserRoleName, Namespace: "team-a"}, role))
		assert.NotEmpty(t, role.Rules)
	})

	t.Run("user Role grants evalhubs read for CR discovery", func(t *testing.T) {
		role := &rbacv1.Role{}
		require.NoError(t, fc.Get(ctx, types.NamespacedName{Name: tenantUserRoleName, Namespace: "team-a"}, role))
		found := false
		for _, rule := range role.Rules {
			if len(rule.Resources) == 1 && rule.Resources[0] == "evalhubs" {
				assert.Contains(t, rule.Verbs, "get")
				assert.Contains(t, rule.Verbs, "list")
				assert.Empty(t, rule.ResourceNames, "must not scope by resourceNames so list works")
				found = true
			}
		}
		assert.True(t, found, "user Role must include an evalhubs rule for BFF discovery")
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

var _ = Describe("reconcileSingleTenancyRoles evaluations access", func() {
	const evalHubName = "tenant-roles-evalhub"

	var (
		testNamespace string
		namespace     *corev1.Namespace
		evalHub       *evalhubv1.EvalHub
		reconciler    *EvalHubReconciler
	)

	BeforeEach(func() {
		testNamespace = fmt.Sprintf("evalhub-tenant-roles-%d", time.Now().UnixNano())
		namespace = createNamespace(testNamespace)
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

		evalHub = createEvalHubInstanceWithSQLite(evalHubName, testNamespace)
		evalHub.Spec.Tenancy = evalhubv1.TenancySingle
		Expect(k8sClient.Create(ctx, evalHub)).To(Succeed())

		reconciler, _ = setupReconciler(testNamespace)
	})

	AfterEach(func() {
		cleanupResourcesInNamespace(testNamespace, evalHub, nil)
		_ = k8sClient.Delete(ctx, &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{Name: tenantUserRoleName, Namespace: testNamespace},
		})
		_ = k8sClient.Delete(ctx, &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{Name: tenantAdminRoleName, Namespace: testNamespace},
		})
		_ = k8sClient.Delete(ctx, &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: tenantAdminBindingName, Namespace: testNamespace},
		})
		deleteNamespace(namespace)
		evalHub, namespace = nil, nil
	})

	It("user Role grants evaluations job lifecycle", func() {
		Expect(reconciler.reconcileSingleTenancyRoles(ctx, evalHub)).To(Succeed())

		role := &rbacv1.Role{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{
				Name:      tenantUserRoleName,
				Namespace: testNamespace,
			}, role)
		}, timeout, interval).Should(Succeed())

		found := false
		for _, rule := range role.Rules {
			if len(rule.Resources) == 1 && rule.Resources[0] == "evaluations" {
				Expect(rule.APIGroups).To(Equal([]string{"trustyai.opendatahub.io"}))
				Expect(rule.ResourceNames).To(BeEmpty())
				Expect(rule.Verbs).To(ConsistOf("get", "list", "create", "update", "patch", "delete"))
				found = true
			}
		}
		Expect(found).To(BeTrue(), "user Role must grant evaluations for ListEvaluationJobs and job lifecycle")
	})
})

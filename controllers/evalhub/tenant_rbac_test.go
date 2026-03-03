package evalhub

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = rbacv1.AddToScheme(s)
	_ = evalhubv1alpha1.AddToScheme(s)
	return s
}

func newTestEvalHub(name, namespace string) *evalhubv1alpha1.EvalHub {
	replicas := int32(1)
	return &evalhubv1alpha1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: evalhubv1alpha1.EvalHubSpec{
			Replicas: &replicas,
		},
	}
}

func TestReconcileTenantNamespace_CreatesResources(t *testing.T) {
	scheme := newTestScheme()
	ctx := context.Background()

	controlPlaneNS := "operator-system"
	tenantNS := "team-a"
	evalHubName := "my-evalhub"

	instance := newTestEvalHub(evalHubName, controlPlaneNS)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(instance).
		Build()

	reconciler := &EvalHubReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		Namespace:     controlPlaneNS,
		EventRecorder: record.NewFakeRecorder(10),
	}

	err := reconciler.reconcileTenantNamespace(ctx, instance, tenantNS)
	require.NoError(t, err)

	// Verify jobs SA was created in tenant namespace
	sa := &corev1.ServiceAccount{}
	err = fakeClient.Get(ctx, types.NamespacedName{
		Name:      evalHubName + "-jobs",
		Namespace: tenantNS,
	}, sa)
	require.NoError(t, err)
	assert.Equal(t, evalHubName, sa.Labels[tenantLabel])
	assert.Equal(t, "eval-hub", sa.Annotations[tenantOwnerAnnotation])

	// Verify jobs-writer RoleBinding
	rb := &rbacv1.RoleBinding{}
	err = fakeClient.Get(ctx, types.NamespacedName{
		Name:      evalHubName + "-tenant-jobs-writer",
		Namespace: tenantNS,
	}, rb)
	require.NoError(t, err)
	assert.Equal(t, jobsWriterClusterRoleName, rb.RoleRef.Name)
	assert.Equal(t, "ClusterRole", rb.RoleRef.Kind)
	assert.Equal(t, "eval-hub", rb.Annotations[tenantOwnerAnnotation])
	require.Len(t, rb.Subjects, 1)
	assert.Equal(t, evalHubName+"-api", rb.Subjects[0].Name)
	assert.Equal(t, controlPlaneNS, rb.Subjects[0].Namespace)

	// Verify job-config RoleBinding
	err = fakeClient.Get(ctx, types.NamespacedName{
		Name:      evalHubName + "-tenant-job-config",
		Namespace: tenantNS,
	}, rb)
	require.NoError(t, err)
	assert.Equal(t, jobConfigClusterRoleName, rb.RoleRef.Name)
	require.Len(t, rb.Subjects, 1)
	assert.Equal(t, evalHubName+"-api", rb.Subjects[0].Name)
	assert.Equal(t, controlPlaneNS, rb.Subjects[0].Namespace)

	// Verify mlflow RoleBinding with two subjects
	err = fakeClient.Get(ctx, types.NamespacedName{
		Name:      evalHubName + "-tenant-mlflow",
		Namespace: tenantNS,
	}, rb)
	require.NoError(t, err)
	assert.Equal(t, mlflowAccessClusterRoleName, rb.RoleRef.Name)
	require.Len(t, rb.Subjects, 2)
	// API SA from control-plane ns
	assert.Equal(t, evalHubName+"-api", rb.Subjects[0].Name)
	assert.Equal(t, controlPlaneNS, rb.Subjects[0].Namespace)
	// Jobs SA from tenant ns
	assert.Equal(t, evalHubName+"-jobs", rb.Subjects[1].Name)
	assert.Equal(t, tenantNS, rb.Subjects[1].Namespace)
}

func TestReconcileTenantNamespace_Idempotent(t *testing.T) {
	scheme := newTestScheme()
	ctx := context.Background()

	controlPlaneNS := "operator-system"
	tenantNS := "team-b"
	evalHubName := "my-evalhub"

	instance := newTestEvalHub(evalHubName, controlPlaneNS)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(instance).
		Build()

	reconciler := &EvalHubReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		Namespace:     controlPlaneNS,
		EventRecorder: record.NewFakeRecorder(10),
	}

	// Call twice — second call should not error
	err := reconciler.reconcileTenantNamespace(ctx, instance, tenantNS)
	require.NoError(t, err)

	err = reconciler.reconcileTenantNamespace(ctx, instance, tenantNS)
	require.NoError(t, err)

	// Resources should still exist
	sa := &corev1.ServiceAccount{}
	err = fakeClient.Get(ctx, types.NamespacedName{
		Name:      evalHubName + "-jobs",
		Namespace: tenantNS,
	}, sa)
	require.NoError(t, err)
}

func TestReconcileTenantNamespaces_SkipsControlPlane(t *testing.T) {
	scheme := newTestScheme()
	ctx := context.Background()

	controlPlaneNS := "operator-system"
	evalHubName := "my-evalhub"

	instance := newTestEvalHub(evalHubName, controlPlaneNS)

	// Create control-plane namespace WITH the tenant annotation
	cpNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: controlPlaneNS,
			Annotations: map[string]string{
				tenantAnnotation: "Control Plane",
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(instance, cpNamespace).
		Build()

	reconciler := &EvalHubReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		Namespace:     controlPlaneNS,
		EventRecorder: record.NewFakeRecorder(10),
	}

	err := reconciler.reconcileTenantNamespaces(ctx, instance)
	require.NoError(t, err)

	// No tenant SA should be created in the control-plane namespace
	saList := &corev1.ServiceAccountList{}
	err = fakeClient.List(ctx, saList)
	require.NoError(t, err)
	for _, sa := range saList.Items {
		if sa.Labels[tenantLabel] == evalHubName {
			t.Errorf("unexpected tenant SA %s/%s in control-plane namespace", sa.Namespace, sa.Name)
		}
	}
}

func TestReconcileTenantNamespaces_CleansUpRemovedAnnotation(t *testing.T) {
	scheme := newTestScheme()
	ctx := context.Background()

	controlPlaneNS := "operator-system"
	tenantNS := "team-c"
	evalHubName := "my-evalhub"

	instance := newTestEvalHub(evalHubName, controlPlaneNS)

	// Tenant namespace with annotation
	tenantNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: tenantNS,
			Annotations: map[string]string{
				tenantAnnotation: "Team C",
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(instance, tenantNamespace).
		Build()

	reconciler := &EvalHubReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		Namespace:     controlPlaneNS,
		EventRecorder: record.NewFakeRecorder(10),
	}

	// First reconcile: creates resources
	err := reconciler.reconcileTenantNamespaces(ctx, instance)
	require.NoError(t, err)

	// Verify SA exists
	sa := &corev1.ServiceAccount{}
	err = fakeClient.Get(ctx, types.NamespacedName{
		Name:      evalHubName + "-jobs",
		Namespace: tenantNS,
	}, sa)
	require.NoError(t, err)

	// Remove annotation from namespace
	err = fakeClient.Get(ctx, types.NamespacedName{Name: tenantNS}, tenantNamespace)
	require.NoError(t, err)
	delete(tenantNamespace.Annotations, tenantAnnotation)
	err = fakeClient.Update(ctx, tenantNamespace)
	require.NoError(t, err)

	// Second reconcile: should clean up
	err = reconciler.reconcileTenantNamespaces(ctx, instance)
	require.NoError(t, err)

	// Verify SA was cleaned up
	err = fakeClient.Get(ctx, types.NamespacedName{
		Name:      evalHubName + "-jobs",
		Namespace: tenantNS,
	}, sa)
	assert.True(t, err != nil, "expected SA to be deleted after annotation removal")

	// Verify RoleBindings were cleaned up
	rb := &rbacv1.RoleBinding{}
	err = fakeClient.Get(ctx, types.NamespacedName{
		Name:      evalHubName + "-tenant-jobs-writer",
		Namespace: tenantNS,
	}, rb)
	assert.True(t, err != nil, "expected RoleBinding to be deleted after annotation removal")
}

func TestCleanupTenantResources(t *testing.T) {
	scheme := newTestScheme()
	ctx := context.Background()

	controlPlaneNS := "operator-system"
	tenantNS := "team-d"
	evalHubName := "my-evalhub"

	instance := newTestEvalHub(evalHubName, controlPlaneNS)

	managedLabels := map[string]string{
		tenantLabel: evalHubName,
	}

	// Pre-create tenant resources
	tenantSA := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      evalHubName + "-jobs",
			Namespace: tenantNS,
			Labels:    managedLabels,
		},
	}
	tenantRB := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      evalHubName + "-tenant-jobs-writer",
			Namespace: tenantNS,
			Labels:    managedLabels,
		},
		Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: evalHubName + "-api", Namespace: controlPlaneNS}},
		RoleRef:  rbacv1.RoleRef{Kind: "ClusterRole", Name: jobsWriterClusterRoleName, APIGroup: rbacv1.GroupName},
	}

	// Also create a resource in control-plane namespace (should NOT be deleted by cleanup)
	cpRB := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      evalHubName + "-jobs-writer-rb",
			Namespace: controlPlaneNS,
			Labels:    managedLabels,
		},
		Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: evalHubName + "-api", Namespace: controlPlaneNS}},
		RoleRef:  rbacv1.RoleRef{Kind: "ClusterRole", Name: jobsWriterClusterRoleName, APIGroup: rbacv1.GroupName},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(instance, tenantSA, tenantRB, cpRB).
		Build()

	reconciler := &EvalHubReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		Namespace:     controlPlaneNS,
		EventRecorder: record.NewFakeRecorder(10),
	}

	err := reconciler.cleanupTenantResources(ctx, instance)
	require.NoError(t, err)

	// Tenant resources should be gone
	sa := &corev1.ServiceAccount{}
	err = fakeClient.Get(ctx, types.NamespacedName{
		Name:      evalHubName + "-jobs",
		Namespace: tenantNS,
	}, sa)
	assert.True(t, err != nil, "tenant SA should have been deleted")

	rb := &rbacv1.RoleBinding{}
	err = fakeClient.Get(ctx, types.NamespacedName{
		Name:      evalHubName + "-tenant-jobs-writer",
		Namespace: tenantNS,
	}, rb)
	assert.True(t, err != nil, "tenant RoleBinding should have been deleted")

	// Control-plane RoleBinding should still exist
	err = fakeClient.Get(ctx, types.NamespacedName{
		Name:      evalHubName + "-jobs-writer-rb",
		Namespace: controlPlaneNS,
	}, rb)
	require.NoError(t, err, "control-plane RoleBinding should NOT be deleted by tenant cleanup")
}

func TestReconcileTenantNamespaces_MultipleNamespaces(t *testing.T) {
	scheme := newTestScheme()
	ctx := context.Background()

	controlPlaneNS := "operator-system"
	evalHubName := "my-evalhub"

	instance := newTestEvalHub(evalHubName, controlPlaneNS)

	ns1 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "team-x",
			Annotations: map[string]string{tenantAnnotation: "Team X"},
		},
	}
	ns2 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "team-y",
			Annotations: map[string]string{tenantAnnotation: "Team Y"},
		},
	}
	// Namespace without annotation — should be ignored
	ns3 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "no-annotation",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(instance, ns1, ns2, ns3).
		Build()

	reconciler := &EvalHubReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		Namespace:     controlPlaneNS,
		EventRecorder: record.NewFakeRecorder(10),
	}

	err := reconciler.reconcileTenantNamespaces(ctx, instance)
	require.NoError(t, err)

	// SA should exist in team-x and team-y but not no-annotation
	sa := &corev1.ServiceAccount{}
	for _, ns := range []string{"team-x", "team-y"} {
		err = fakeClient.Get(ctx, types.NamespacedName{
			Name:      evalHubName + "-jobs",
			Namespace: ns,
		}, sa)
		require.NoError(t, err, "expected SA in namespace %s", ns)
	}

	err = fakeClient.Get(ctx, types.NamespacedName{
		Name:      evalHubName + "-jobs",
		Namespace: "no-annotation",
	}, sa)
	assert.True(t, err != nil, "should not create SA in unannotated namespace")
}

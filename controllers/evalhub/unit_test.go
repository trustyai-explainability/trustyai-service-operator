package evalhub

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/trustyai-explainability/trustyai-service-operator/api/common"
	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"
)

func TestEvalHubReconciler_reconcileDeployment(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, evalhubv1alpha1.AddToScheme(scheme))

	ctx := context.Background()
	testNamespace := "test-namespace"
	evalHubName := "test-evalhub"

	// Create EvalHub instance
	replicas := int32(2)
	evalHub := &evalhubv1alpha1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:      evalHubName,
			Namespace: testNamespace,
		},
		Spec: evalhubv1alpha1.EvalHubSpec{
			Replicas: &replicas,
			Env: []corev1.EnvVar{
				{Name: "TEST_VAR", Value: "test-value"},
			},
		},
	}

	// Create config map with image
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: testNamespace,
		},
		Data: map[string]string{
			configMapEvalHubImageKey:       "quay.io/test/eval-hub:latest",
			configMapKubeRBACProxyImageKey: "gcr.io/kubebuilder/kube-rbac-proxy:v0.13.1",
		},
	}

	// Create fake client
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(evalHub, configMap).
		Build()

	reconciler := &EvalHubReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		Namespace:     testNamespace,
		EventRecorder: record.NewFakeRecorder(10),
	}

	t.Run("should create deployment with correct spec", func(t *testing.T) {
		err := reconciler.reconcileDeployment(ctx, evalHub, nil)
		require.NoError(t, err)

		// Verify deployment was created
		deployment := &appsv1.Deployment{}
		err = fakeClient.Get(ctx, types.NamespacedName{
			Name:      evalHubName,
			Namespace: testNamespace,
		}, deployment)
		require.NoError(t, err)

		// Check basic deployment properties
		assert.Equal(t, evalHubName, deployment.Name)
		assert.Equal(t, testNamespace, deployment.Namespace)
		assert.Equal(t, replicas, *deployment.Spec.Replicas)

		// Check container configuration
		require.Len(t, deployment.Spec.Template.Spec.Containers, 2)

		// Find the evalhub container
		var container *corev1.Container
		for i, c := range deployment.Spec.Template.Spec.Containers {
			if c.Name == containerName {
				container = &deployment.Spec.Template.Spec.Containers[i]
				break
			}
		}
		require.NotNil(t, container, "evalhub container should be present")

		assert.Equal(t, containerName, container.Name)
		assert.Equal(t, "quay.io/test/eval-hub:latest", container.Image)
		assert.Equal(t, corev1.PullAlways, container.ImagePullPolicy)

		// Check ports
		require.Len(t, container.Ports, 1)
		assert.Equal(t, "http", container.Ports[0].Name)
		assert.Equal(t, int32(8080), container.Ports[0].ContainerPort)

		// Check environment variables include both default and custom
		envVarMap := make(map[string]string)
		for _, env := range container.Env {
			envVarMap[env.Name] = env.Value
		}
		// API_HOST is 127.0.0.1 to ensure only kube-rbac-proxy can reach the API (security hardening)
		assert.Equal(t, "127.0.0.1", envVarMap["API_HOST"])
		assert.Equal(t, "8080", envVarMap["API_PORT"])
		assert.Equal(t, "test-value", envVarMap["TEST_VAR"])

		// Check SERVICE_URL and EVALHUB_INSTANCE_NAME are propagated
		assert.Equal(t, "https://test-evalhub.test-namespace.svc.cluster.local:8443", envVarMap["SERVICE_URL"])
		assert.Equal(t, evalHubName, envVarMap["EVALHUB_INSTANCE_NAME"])

		// Check resource requirements
		assert.Equal(t, resource.MustParse("500m"), container.Resources.Requests[corev1.ResourceCPU])
		assert.Equal(t, resource.MustParse("512Mi"), container.Resources.Requests[corev1.ResourceMemory])

		// Check health probes (exec-based because API listens on 127.0.0.1 only)
		require.NotNil(t, container.LivenessProbe)
		require.NotNil(t, container.LivenessProbe.Exec)
		assert.Equal(t, []string{"/usr/bin/curl", "--fail", "--silent", "--max-time", "3", "http://127.0.0.1:8080/api/v1/health"}, container.LivenessProbe.Exec.Command)

		// Check readiness probe matches expected exec-based curl check
		require.NotNil(t, container.ReadinessProbe)
		require.NotNil(t, container.ReadinessProbe.Exec)
		assert.Equal(t, []string{"/usr/bin/curl", "--fail", "--silent", "--max-time", "2", "http://127.0.0.1:8080/api/v1/health"}, container.ReadinessProbe.Exec.Command)
	})

	t.Run("should fail when configmap missing", func(t *testing.T) {
		// Create client without configmap
		fakeClientNoConfig := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(evalHub).
			Build()

		reconcilerNoConfig := &EvalHubReconciler{
			Client:        fakeClientNoConfig,
			Scheme:        scheme,
			Namespace:     testNamespace,
			EventRecorder: record.NewFakeRecorder(10),
		}

		err := reconcilerNoConfig.reconcileDeployment(ctx, evalHub, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "kube-rbac-proxy configuration error")
	})
}

func TestEvalHubReconciler_reconcileService(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, evalhubv1alpha1.AddToScheme(scheme))

	ctx := context.Background()
	testNamespace := "test-namespace"
	evalHubName := "test-evalhub"

	evalHub := &evalhubv1alpha1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:      evalHubName,
			Namespace: testNamespace,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(evalHub).
		Build()

	reconciler := &EvalHubReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		Namespace:     testNamespace,
		EventRecorder: record.NewFakeRecorder(10),
	}

	t.Run("should create service with correct spec", func(t *testing.T) {
		err := reconciler.reconcileService(ctx, evalHub)
		require.NoError(t, err)

		// Verify service was created
		service := &corev1.Service{}
		err = fakeClient.Get(ctx, types.NamespacedName{
			Name:      evalHubName,
			Namespace: testNamespace,
		}, service)
		require.NoError(t, err)

		// Check basic service properties
		assert.Equal(t, evalHubName, service.Name)
		assert.Equal(t, testNamespace, service.Namespace)
		assert.Equal(t, corev1.ServiceTypeClusterIP, service.Spec.Type)

		// Check ports
		require.Len(t, service.Spec.Ports, 1)
		port := service.Spec.Ports[0]
		assert.Equal(t, "https", port.Name)
		assert.Equal(t, int32(8443), port.Port)
		assert.Equal(t, intstr.FromString("https"), port.TargetPort)
		assert.Equal(t, corev1.ProtocolTCP, port.Protocol)

		// Check selector
		assert.Equal(t, "eval-hub", service.Spec.Selector["app"])
		assert.Equal(t, evalHubName, service.Spec.Selector["instance"])
		assert.Equal(t, "api", service.Spec.Selector["component"])
	})
}

func TestEvalHubReconciler_reconcileConfigMap(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, evalhubv1alpha1.AddToScheme(scheme))

	ctx := context.Background()
	testNamespace := "test-namespace"
	evalHubName := "test-evalhub"

	evalHub := &evalhubv1alpha1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:      evalHubName,
			Namespace: testNamespace,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(evalHub).
		Build()

	reconciler := &EvalHubReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		Namespace:     testNamespace,
		EventRecorder: record.NewFakeRecorder(10),
	}

	t.Run("should create configmap with valid configuration", func(t *testing.T) {
		err := reconciler.reconcileConfigMap(ctx, evalHub)
		require.NoError(t, err)

		// Verify configmap was created
		configMap := &corev1.ConfigMap{}
		err = fakeClient.Get(ctx, types.NamespacedName{
			Name:      evalHubName + "-config",
			Namespace: testNamespace,
		}, configMap)
		require.NoError(t, err)

		// Check basic configmap properties
		assert.Equal(t, evalHubName+"-config", configMap.Name)
		assert.Equal(t, testNamespace, configMap.Namespace)

		// Check data keys exist
		assert.Contains(t, configMap.Data, "config.yaml")

		// Parse and validate config.yaml
		var config EvalHubConfig
		err = yaml.Unmarshal([]byte(configMap.Data["config.yaml"]), &config)
		require.NoError(t, err)
	})
}

func TestEvalHubReconciler_updateStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, evalhubv1alpha1.AddToScheme(scheme))

	ctx := context.Background()
	testNamespace := "test-namespace"
	evalHubName := "test-evalhub"

	evalHub := &evalhubv1alpha1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:      evalHubName,
			Namespace: testNamespace,
		},
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      evalHubName,
			Namespace: testNamespace,
		},
		Status: appsv1.DeploymentStatus{
			Replicas:      2,
			ReadyReplicas: 2,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(evalHub, deployment).
		WithStatusSubresource(&evalhubv1alpha1.EvalHub{}).
		Build()

	reconciler := &EvalHubReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		Namespace:     testNamespace,
		EventRecorder: record.NewFakeRecorder(10),
	}

	t.Run("should update status to Ready when deployment is ready", func(t *testing.T) {
		err := reconciler.updateStatus(ctx, evalHub)
		require.NoError(t, err)

		// Verify status was updated
		updatedEvalHub := &evalhubv1alpha1.EvalHub{}
		err = fakeClient.Get(ctx, types.NamespacedName{
			Name:      evalHubName,
			Namespace: testNamespace,
		}, updatedEvalHub)
		require.NoError(t, err)

		assert.Equal(t, "Ready", updatedEvalHub.Status.Phase)
		assert.Equal(t, corev1.ConditionTrue, updatedEvalHub.Status.Ready)
		assert.Equal(t, int32(2), updatedEvalHub.Status.Replicas)
		assert.Equal(t, int32(2), updatedEvalHub.Status.ReadyReplicas)

		expectedURL := "https://test-evalhub.test-namespace.svc.cluster.local:8443"
		assert.Equal(t, expectedURL, updatedEvalHub.Status.URL)

		// Check conditions
		require.NotEmpty(t, updatedEvalHub.Status.Conditions)
		var readyCondition *common.Condition
		for i := range updatedEvalHub.Status.Conditions {
			if updatedEvalHub.Status.Conditions[i].Type == "Ready" {
				readyCondition = &updatedEvalHub.Status.Conditions[i]
				break
			}
		}
		require.NotNil(t, readyCondition)
		assert.Equal(t, corev1.ConditionTrue, readyCondition.Status)
		assert.Equal(t, "DeploymentReady", readyCondition.Reason)
	})

	t.Run("should update status to Pending when deployment not ready", func(t *testing.T) {
		// Update deployment to not ready
		deployment.Status.ReadyReplicas = 1
		err := fakeClient.Status().Update(ctx, deployment)
		require.NoError(t, err)

		err = reconciler.updateStatus(ctx, evalHub)
		require.NoError(t, err)

		// Verify status was updated
		updatedEvalHub := &evalhubv1alpha1.EvalHub{}
		err = fakeClient.Get(ctx, types.NamespacedName{
			Name:      evalHubName,
			Namespace: testNamespace,
		}, updatedEvalHub)
		require.NoError(t, err)

		assert.Equal(t, "Pending", updatedEvalHub.Status.Phase)
		assert.Equal(t, corev1.ConditionFalse, updatedEvalHub.Status.Ready)

		// Check conditions
		require.NotEmpty(t, updatedEvalHub.Status.Conditions)
		var readyCondition *common.Condition
		for i := range updatedEvalHub.Status.Conditions {
			if updatedEvalHub.Status.Conditions[i].Type == "Ready" {
				readyCondition = &updatedEvalHub.Status.Conditions[i]
				break
			}
		}
		require.NotNil(t, readyCondition)
		assert.Equal(t, corev1.ConditionFalse, readyCondition.Status)
		assert.Equal(t, "DeploymentNotReady", readyCondition.Reason)
		assert.Contains(t, readyCondition.Message, "Waiting for deployment to be ready")
	})
}

func TestGenerateConfigData(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, evalhubv1alpha1.AddToScheme(scheme))

	evalHub := &evalhubv1alpha1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-evalhub",
			Namespace: "test-namespace",
		},
	}

	reconciler := &EvalHubReconciler{
		Scheme: scheme,
	}

	t.Run("should generate valid configuration data", func(t *testing.T) {
		configData, err := reconciler.generateConfigData(evalHub)
		require.NoError(t, err)

		// Check keys exist
		assert.Contains(t, configData, "config.yaml")

		// Parse config.yaml
		var config EvalHubConfig
		err = yaml.Unmarshal([]byte(configData["config.yaml"]), &config)
		require.NoError(t, err)
	})
}

func TestEvalHubHelperMethods(t *testing.T) {
	t.Run("EvalHub IsReady method", func(t *testing.T) {
		evalHub := &evalhubv1alpha1.EvalHub{}

		// Test not ready
		evalHub.Status.Ready = corev1.ConditionFalse
		assert.False(t, evalHub.IsReady())

		// Test ready
		evalHub.Status.Ready = corev1.ConditionTrue
		assert.True(t, evalHub.IsReady())
	})

	t.Run("EvalHub SetStatus method", func(t *testing.T) {
		evalHub := &evalhubv1alpha1.EvalHub{}

		// Set initial status
		evalHub.SetStatus("Ready", "TestReason", "Test message", corev1.ConditionTrue)

		require.Len(t, evalHub.Status.Conditions, 1)
		condition := evalHub.Status.Conditions[0]
		assert.Equal(t, "Ready", condition.Type)
		assert.Equal(t, "TestReason", condition.Reason)
		assert.Equal(t, "Test message", condition.Message)
		assert.Equal(t, corev1.ConditionTrue, condition.Status)
		require.NotNil(t, evalHub.Status.LastUpdateTime)
	})

	t.Run("EvalHubSpec GetReplicas method", func(t *testing.T) {
		spec := &evalhubv1alpha1.EvalHubSpec{}

		// Test default value
		assert.Equal(t, int32(1), spec.GetReplicas())

		// Test custom value
		customReplicas := int32(3)
		spec.Replicas = &customReplicas
		assert.Equal(t, int32(3), spec.GetReplicas())
	})
}

// TestEvalHubReconciler_createJobsServiceAccount verifies that the jobs ServiceAccount
// is created with the correct name, labels, and owner reference.
func TestEvalHubReconciler_createJobsServiceAccount(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, evalhubv1alpha1.AddToScheme(scheme))

	ctx := context.Background()
	testNamespace := "test-namespace"
	evalHubName := "test-evalhub"

	evalHub := &evalhubv1alpha1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:      evalHubName,
			Namespace: testNamespace,
			UID:       "test-uid-123",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(evalHub).
		Build()

	reconciler := &EvalHubReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		EventRecorder: record.NewFakeRecorder(10),
	}

	t.Run("should create jobs ServiceAccount with correct properties", func(t *testing.T) {
		err := reconciler.createJobsServiceAccount(ctx, evalHub)
		require.NoError(t, err)

		// Verify ServiceAccount was created
		jobsSAName := evalHubName + "-jobs"
		jobsSA := &corev1.ServiceAccount{}
		err = fakeClient.Get(ctx, types.NamespacedName{
			Name:      jobsSAName,
			Namespace: testNamespace,
		}, jobsSA)
		require.NoError(t, err)

		// Check name and namespace
		assert.Equal(t, jobsSAName, jobsSA.Name)
		assert.Equal(t, testNamespace, jobsSA.Namespace)

		// Check labels
		assert.Equal(t, "eval-hub", jobsSA.Labels["app"])
		assert.Equal(t, jobsSAName, jobsSA.Labels["app.kubernetes.io/name"])
		assert.Equal(t, evalHubName, jobsSA.Labels["app.kubernetes.io/instance"])
		assert.Equal(t, "jobs", jobsSA.Labels["app.kubernetes.io/component"])

		// Check owner reference
		require.Len(t, jobsSA.OwnerReferences, 1)
		assert.Equal(t, evalHubName, jobsSA.OwnerReferences[0].Name)
		assert.Equal(t, "EvalHub", jobsSA.OwnerReferences[0].Kind)
		assert.True(t, *jobsSA.OwnerReferences[0].Controller)
	})

	t.Run("should be idempotent on repeated calls", func(t *testing.T) {
		// Call again
		err := reconciler.createJobsServiceAccount(ctx, evalHub)
		require.NoError(t, err)

		// Verify still only one ServiceAccount exists
		jobsSAName := evalHubName + "-jobs"
		jobsSA := &corev1.ServiceAccount{}
		err = fakeClient.Get(ctx, types.NamespacedName{
			Name:      jobsSAName,
			Namespace: testNamespace,
		}, jobsSA)
		require.NoError(t, err)
	})
}

// TestEvalHubReconciler_createJobsAPIAccessRoleBinding verifies that the jobs API access RoleBinding
// is created with the correct RoleRef, Subjects, and owner reference.
func TestEvalHubReconciler_createJobsAPIAccessRoleBinding(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, rbacv1.AddToScheme(scheme))
	require.NoError(t, evalhubv1alpha1.AddToScheme(scheme))

	ctx := context.Background()
	testNamespace := "test-namespace"
	evalHubName := "test-evalhub"

	evalHub := &evalhubv1alpha1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:      evalHubName,
			Namespace: testNamespace,
			UID:       "test-uid-123",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(evalHub).
		Build()

	reconciler := &EvalHubReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		EventRecorder: record.NewFakeRecorder(10),
	}

	t.Run("should create jobs API access RoleBinding referencing per-instance Role", func(t *testing.T) {
		jobsSAName := evalHubName + "-jobs"
		err := reconciler.createJobsAPIAccessRoleBinding(ctx, evalHub, jobsSAName)
		require.NoError(t, err)

		// Verify RoleBinding was created
		rbName := evalHubName + "-jobs-api-access-rb"
		rb := &rbacv1.RoleBinding{}
		err = fakeClient.Get(ctx, types.NamespacedName{
			Name:      rbName,
			Namespace: testNamespace,
		}, rb)
		require.NoError(t, err)

		// Check RoleRef — should reference the per-instance Role (not ClusterRole)
		assert.Equal(t, "Role", rb.RoleRef.Kind)
		assert.Equal(t, generateJobsAPIAccessRoleName(evalHub), rb.RoleRef.Name)
		assert.Equal(t, rbacv1.GroupName, rb.RoleRef.APIGroup)

		// Check Subjects
		require.Len(t, rb.Subjects, 1)
		assert.Equal(t, "ServiceAccount", rb.Subjects[0].Kind)
		assert.Equal(t, jobsSAName, rb.Subjects[0].Name)
		assert.Equal(t, testNamespace, rb.Subjects[0].Namespace)

		// Check owner reference
		require.Len(t, rb.OwnerReferences, 1)
		assert.Equal(t, evalHubName, rb.OwnerReferences[0].Name)
		assert.Equal(t, "EvalHub", rb.OwnerReferences[0].Kind)
	})

	t.Run("should update subjects when they differ", func(t *testing.T) {
		// Get existing RoleBinding
		rbName := evalHubName + "-jobs-api-access-rb"
		rb := &rbacv1.RoleBinding{}
		err := fakeClient.Get(ctx, types.NamespacedName{
			Name:      rbName,
			Namespace: testNamespace,
		}, rb)
		require.NoError(t, err)

		// Modify subjects
		rb.Subjects = []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "different-sa",
				Namespace: testNamespace,
			},
		}
		err = fakeClient.Update(ctx, rb)
		require.NoError(t, err)

		// Reconcile again
		jobsSAName := evalHubName + "-jobs"
		err = reconciler.createJobsAPIAccessRoleBinding(ctx, evalHub, jobsSAName)
		require.NoError(t, err)

		// Verify subjects were updated
		err = fakeClient.Get(ctx, types.NamespacedName{
			Name:      rbName,
			Namespace: testNamespace,
		}, rb)
		require.NoError(t, err)

		require.Len(t, rb.Subjects, 1)
		assert.Equal(t, jobsSAName, rb.Subjects[0].Name)
	})
}

// TestEvalHubReconciler_createAPIAccessRole verifies that the per-instance API access Role
// is created with resourceNames scoped to the instance, with correct owner ref and idempotency.
func TestEvalHubReconciler_createAPIAccessRole(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, rbacv1.AddToScheme(scheme))
	require.NoError(t, evalhubv1alpha1.AddToScheme(scheme))

	ctx := context.Background()
	testNamespace := "test-namespace"
	evalHubName := "test-evalhub"

	evalHub := &evalhubv1alpha1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:      evalHubName,
			Namespace: testNamespace,
			UID:       "test-uid-123",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(evalHub).
		Build()

	reconciler := &EvalHubReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		EventRecorder: record.NewFakeRecorder(10),
	}

	t.Run("should create API access Role with resourceNames", func(t *testing.T) {
		err := reconciler.createAPIAccessRole(ctx, evalHub)
		require.NoError(t, err)

		roleName := generateAPIAccessRoleName(evalHub)
		role := &rbacv1.Role{}
		err = fakeClient.Get(ctx, types.NamespacedName{
			Name:      roleName,
			Namespace: testNamespace,
		}, role)
		require.NoError(t, err)

		// Check name
		assert.Equal(t, evalHubName+"-api-access-role", role.Name)

		// Check rules have resourceNames scoped to this instance
		require.Len(t, role.Rules, 2)

		// First rule: evalhubs get
		assert.Equal(t, []string{"trustyai.opendatahub.io"}, role.Rules[0].APIGroups)
		assert.Equal(t, []string{"evalhubs"}, role.Rules[0].Resources)
		assert.Equal(t, []string{evalHubName}, role.Rules[0].ResourceNames)
		assert.Equal(t, []string{"get"}, role.Rules[0].Verbs)

		// Second rule: evalhubs/proxy get,create
		assert.Equal(t, []string{"trustyai.opendatahub.io"}, role.Rules[1].APIGroups)
		assert.Equal(t, []string{"evalhubs/proxy"}, role.Rules[1].Resources)
		assert.Equal(t, []string{evalHubName}, role.Rules[1].ResourceNames)
		assert.Equal(t, []string{"get", "create"}, role.Rules[1].Verbs)

		// Check owner reference
		require.Len(t, role.OwnerReferences, 1)
		assert.Equal(t, evalHubName, role.OwnerReferences[0].Name)
		assert.Equal(t, "EvalHub", role.OwnerReferences[0].Kind)
		assert.True(t, *role.OwnerReferences[0].Controller)
	})

	t.Run("should be idempotent on repeated calls", func(t *testing.T) {
		err := reconciler.createAPIAccessRole(ctx, evalHub)
		require.NoError(t, err)

		roleName := generateAPIAccessRoleName(evalHub)
		role := &rbacv1.Role{}
		err = fakeClient.Get(ctx, types.NamespacedName{
			Name:      roleName,
			Namespace: testNamespace,
		}, role)
		require.NoError(t, err)
		assert.Equal(t, []string{evalHubName}, role.Rules[0].ResourceNames)
	})
}

// TestEvalHubReconciler_createAPIAccessRoleBinding_RefersToRole verifies that the
// API access RoleBinding references a Role (not ClusterRole).
func TestEvalHubReconciler_createAPIAccessRoleBinding_RefersToRole(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, rbacv1.AddToScheme(scheme))
	require.NoError(t, evalhubv1alpha1.AddToScheme(scheme))

	ctx := context.Background()
	testNamespace := "test-namespace"
	evalHubName := "test-evalhub"

	evalHub := &evalhubv1alpha1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:      evalHubName,
			Namespace: testNamespace,
			UID:       "test-uid-123",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(evalHub).
		Build()

	reconciler := &EvalHubReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		EventRecorder: record.NewFakeRecorder(10),
	}

	t.Run("should reference Role not ClusterRole", func(t *testing.T) {
		apiSAName := evalHubName + "-api"
		err := reconciler.createAPIAccessRoleBinding(ctx, evalHub, apiSAName)
		require.NoError(t, err)

		rbName := evalHubName + "-api-access-rb"
		rb := &rbacv1.RoleBinding{}
		err = fakeClient.Get(ctx, types.NamespacedName{
			Name:      rbName,
			Namespace: testNamespace,
		}, rb)
		require.NoError(t, err)

		assert.Equal(t, "Role", rb.RoleRef.Kind, "RoleBinding should reference Role, not ClusterRole")
		assert.Equal(t, generateAPIAccessRoleName(evalHub), rb.RoleRef.Name)
	})
}

// TestEvalHubReconciler_createMLFlowAccessRoleBinding_JobsRole verifies that the jobs
// MLflow RoleBinding uses the restricted jobs-specific ClusterRole.
func TestEvalHubReconciler_createMLFlowAccessRoleBinding_JobsRole(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, rbacv1.AddToScheme(scheme))
	require.NoError(t, evalhubv1alpha1.AddToScheme(scheme))

	ctx := context.Background()
	testNamespace := "test-namespace"
	evalHubName := "test-evalhub"

	evalHub := &evalhubv1alpha1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:      evalHubName,
			Namespace: testNamespace,
			UID:       "test-uid-123",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(evalHub).
		Build()

	reconciler := &EvalHubReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		EventRecorder: record.NewFakeRecorder(10),
	}

	t.Run("should create jobs MLflow RoleBinding with restricted ClusterRole", func(t *testing.T) {
		jobsSAName := evalHubName + "-jobs"
		err := reconciler.createMLFlowAccessRoleBinding(ctx, evalHub, jobsSAName, "jobs", mlflowJobsAccessClusterRoleName)
		require.NoError(t, err)

		// Verify RoleBinding was created
		rbName := evalHubName + "-mlflow-jobs-rb"
		rb := &rbacv1.RoleBinding{}
		err = fakeClient.Get(ctx, types.NamespacedName{
			Name:      rbName,
			Namespace: testNamespace,
		}, rb)
		require.NoError(t, err)

		// Check RoleRef uses the jobs-specific restricted role
		assert.Equal(t, "ClusterRole", rb.RoleRef.Kind)
		assert.Equal(t, "trustyai-service-operator-evalhub-mlflow-jobs-access", rb.RoleRef.Name)
		assert.Equal(t, rbacv1.GroupName, rb.RoleRef.APIGroup)

		// Check Subjects
		require.Len(t, rb.Subjects, 1)
		assert.Equal(t, "ServiceAccount", rb.Subjects[0].Kind)
		assert.Equal(t, jobsSAName, rb.Subjects[0].Name)
		assert.Equal(t, testNamespace, rb.Subjects[0].Namespace)

		// Check owner reference
		require.Len(t, rb.OwnerReferences, 1)
		assert.Equal(t, evalHubName, rb.OwnerReferences[0].Name)
		assert.Equal(t, "EvalHub", rb.OwnerReferences[0].Kind)
	})

	t.Run("should create API MLflow RoleBinding with full ClusterRole", func(t *testing.T) {
		apiSAName := evalHubName + "-api"
		err := reconciler.createMLFlowAccessRoleBinding(ctx, evalHub, apiSAName, "api", mlflowAccessClusterRoleName)
		require.NoError(t, err)

		// Verify RoleBinding was created
		rbName := evalHubName + "-mlflow-api-rb"
		rb := &rbacv1.RoleBinding{}
		err = fakeClient.Get(ctx, types.NamespacedName{
			Name:      rbName,
			Namespace: testNamespace,
		}, rb)
		require.NoError(t, err)

		// Check RoleRef uses the full access role
		assert.Equal(t, "ClusterRole", rb.RoleRef.Kind)
		assert.Equal(t, "trustyai-service-operator-evalhub-mlflow-access", rb.RoleRef.Name)
		assert.Equal(t, rbacv1.GroupName, rb.RoleRef.APIGroup)

		// Check Subjects
		require.Len(t, rb.Subjects, 1)
		assert.Equal(t, "ServiceAccount", rb.Subjects[0].Kind)
		assert.Equal(t, apiSAName, rb.Subjects[0].Name)
	})

	t.Run("should be idempotent on repeated calls", func(t *testing.T) {
		jobsSAName := evalHubName + "-jobs"
		err := reconciler.createMLFlowAccessRoleBinding(ctx, evalHub, jobsSAName, "jobs", mlflowJobsAccessClusterRoleName)
		require.NoError(t, err)

		// Verify RoleBinding still exists with correct properties
		rbName := evalHubName + "-mlflow-jobs-rb"
		rb := &rbacv1.RoleBinding{}
		err = fakeClient.Get(ctx, types.NamespacedName{
			Name:      rbName,
			Namespace: testNamespace,
		}, rb)
		require.NoError(t, err)

		assert.Equal(t, "trustyai-service-operator-evalhub-mlflow-jobs-access", rb.RoleRef.Name)
		require.Len(t, rb.Subjects, 1)
		assert.Equal(t, jobsSAName, rb.Subjects[0].Name)
	})
}

// TestEvalHubReconciler_cleanupClusterRoleBinding verifies that cleanup removes
// the auth-reviewer ClusterRoleBinding (the only cluster-scoped resource not owner-ref'd).
func TestEvalHubReconciler_cleanupClusterRoleBinding(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, rbacv1.AddToScheme(scheme))
	require.NoError(t, evalhubv1alpha1.AddToScheme(scheme))

	ctx := context.Background()
	testNamespace := "test-namespace"
	evalHubName := "test-evalhub"

	evalHub := &evalhubv1alpha1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:      evalHubName,
			Namespace: testNamespace,
		},
	}

	authCRBName := evalHubName + "-" + testNamespace + "-auth-reviewer-crb"
	authCRB := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: authCRBName,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     authReviewerClusterRoleName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      evalHubName + "-api",
				Namespace: testNamespace,
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(evalHub, authCRB).
		Build()

	reconciler := &EvalHubReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		EventRecorder: record.NewFakeRecorder(10),
	}

	t.Run("should delete auth reviewer ClusterRoleBinding", func(t *testing.T) {
		err := reconciler.cleanupClusterRoleBinding(ctx, evalHub)
		require.NoError(t, err)

		err = fakeClient.Get(ctx, types.NamespacedName{Name: authCRBName}, &rbacv1.ClusterRoleBinding{})
		assert.True(t, errors.IsNotFound(err), "Auth reviewer ClusterRoleBinding should be deleted")
	})

	t.Run("should be idempotent when resources don't exist", func(t *testing.T) {
		err := reconciler.cleanupClusterRoleBinding(ctx, evalHub)
		require.NoError(t, err)
	})
}

// TestEvalHubReconciler_reconcileServiceCAConfigMap verifies that the service CA ConfigMap
// is created when missing and updated with the inject-cabundle annotation when it exists.
func TestEvalHubReconciler_reconcileServiceCAConfigMap(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, evalhubv1alpha1.AddToScheme(scheme))

	ctx := context.Background()
	testNamespace := "test-namespace"
	evalHubName := "test-evalhub"

	evalHub := &evalhubv1alpha1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:      evalHubName,
			Namespace: testNamespace,
			UID:       "test-uid-123",
		},
	}

	t.Run("should create ConfigMap when missing", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(evalHub).
			Build()

		reconciler := &EvalHubReconciler{
			Client:        fakeClient,
			Scheme:        scheme,
			EventRecorder: record.NewFakeRecorder(10),
		}

		err := reconciler.reconcileServiceCAConfigMap(ctx, evalHub)
		require.NoError(t, err)

		// Verify ConfigMap was created
		cmName := evalHubName + "-service-ca"
		cm := &corev1.ConfigMap{}
		err = fakeClient.Get(ctx, types.NamespacedName{
			Name:      cmName,
			Namespace: testNamespace,
		}, cm)
		require.NoError(t, err)

		// Check name and namespace
		assert.Equal(t, cmName, cm.Name)
		assert.Equal(t, testNamespace, cm.Namespace)

		// Check inject-cabundle annotation
		assert.Equal(t, "true", cm.Annotations["service.beta.openshift.io/inject-cabundle"])

		// Check owner reference
		require.Len(t, cm.OwnerReferences, 1)
		assert.Equal(t, evalHubName, cm.OwnerReferences[0].Name)
		assert.Equal(t, "EvalHub", cm.OwnerReferences[0].Kind)

		// Check data is initialized (empty or nil - both are valid)
		// The OpenShift service CA operator will inject service-ca.crt later
		if cm.Data != nil {
			assert.Empty(t, cm.Data)
		}
	})

	t.Run("should update annotation when ConfigMap exists", func(t *testing.T) {
		// Create ConfigMap with missing annotation
		cmName := evalHubName + "-service-ca"
		existingCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cmName,
				Namespace: testNamespace,
				Annotations: map[string]string{
					"custom-annotation": "keep-me",
				},
			},
			Data: map[string]string{
				"existing-key": "existing-value",
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(evalHub, existingCM).
			Build()

		reconciler := &EvalHubReconciler{
			Client:        fakeClient,
			Scheme:        scheme,
			EventRecorder: record.NewFakeRecorder(10),
		}

		err := reconciler.reconcileServiceCAConfigMap(ctx, evalHub)
		require.NoError(t, err)

		// Verify ConfigMap was updated
		cm := &corev1.ConfigMap{}
		err = fakeClient.Get(ctx, types.NamespacedName{
			Name:      cmName,
			Namespace: testNamespace,
		}, cm)
		require.NoError(t, err)

		// Check inject-cabundle annotation was added
		assert.Equal(t, "true", cm.Annotations["service.beta.openshift.io/inject-cabundle"])

		// Check custom annotation was preserved
		assert.Equal(t, "keep-me", cm.Annotations["custom-annotation"])

		// Check data was preserved
		assert.Equal(t, "existing-value", cm.Data["existing-key"])
	})

	t.Run("should reset annotation when it has wrong value", func(t *testing.T) {
		// Create ConfigMap with wrong annotation value
		cmName := evalHubName + "-service-ca"
		existingCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cmName,
				Namespace: testNamespace,
				Annotations: map[string]string{
					"service.beta.openshift.io/inject-cabundle": "false",
				},
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(evalHub, existingCM).
			Build()

		reconciler := &EvalHubReconciler{
			Client:        fakeClient,
			Scheme:        scheme,
			EventRecorder: record.NewFakeRecorder(10),
		}

		err := reconciler.reconcileServiceCAConfigMap(ctx, evalHub)
		require.NoError(t, err)

		// Verify annotation was corrected
		cm := &corev1.ConfigMap{}
		err = fakeClient.Get(ctx, types.NamespacedName{
			Name:      cmName,
			Namespace: testNamespace,
		}, cm)
		require.NoError(t, err)

		assert.Equal(t, "true", cm.Annotations["service.beta.openshift.io/inject-cabundle"])
	})
}

func TestGenerateConfigData_WithDatabase(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, evalhubv1alpha1.AddToScheme(scheme))

	t.Run("should include database and secrets sections when DB configured", func(t *testing.T) {
		evalHub := &evalhubv1alpha1.EvalHub{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-evalhub",
				Namespace: "test-namespace",
			},
			Spec: evalhubv1alpha1.EvalHubSpec{
				Database: &evalhubv1alpha1.DatabaseSpec{
					Secret: "my-db-secret",
				},
			},
		}

		reconciler := &EvalHubReconciler{Scheme: scheme}
		configData, err := reconciler.generateConfigData(evalHub)
		require.NoError(t, err)

		var config EvalHubConfig
		err = yaml.Unmarshal([]byte(configData["config.yaml"]), &config)
		require.NoError(t, err)

		// Database section
		require.NotNil(t, config.Database)
		assert.Equal(t, dbDriver, config.Database.Driver)
		assert.Equal(t, dbDefaultMaxOpen, config.Database.MaxOpenConns)
		assert.Equal(t, dbDefaultMaxIdle, config.Database.MaxIdleConns)

		// Secrets section
		require.NotNil(t, config.Secrets)
		assert.Equal(t, dbSecretMountPath, config.Secrets.Dir)
		assert.Equal(t, "database.url", config.Secrets.Mappings[dbSecretKey])
	})

	t.Run("should use custom pool sizes when specified", func(t *testing.T) {
		evalHub := &evalhubv1alpha1.EvalHub{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-evalhub",
				Namespace: "test-namespace",
			},
			Spec: evalhubv1alpha1.EvalHubSpec{
				Database: &evalhubv1alpha1.DatabaseSpec{
					Secret:       "my-db-secret",
					MaxOpenConns: 50,
					MaxIdleConns: 10,
				},
			},
		}

		reconciler := &EvalHubReconciler{Scheme: scheme}
		configData, err := reconciler.generateConfigData(evalHub)
		require.NoError(t, err)

		var config EvalHubConfig
		err = yaml.Unmarshal([]byte(configData["config.yaml"]), &config)
		require.NoError(t, err)

		require.NotNil(t, config.Database)
		assert.Equal(t, 50, config.Database.MaxOpenConns)
		assert.Equal(t, 10, config.Database.MaxIdleConns)
	})

	t.Run("should default to sqlite when DB not explicitly configured", func(t *testing.T) {
		evalHub := &evalhubv1alpha1.EvalHub{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-evalhub",
				Namespace: "test-namespace",
			},
		}

		reconciler := &EvalHubReconciler{Scheme: scheme}
		configData, err := reconciler.generateConfigData(evalHub)
		require.NoError(t, err)

		var config EvalHubConfig
		err = yaml.Unmarshal([]byte(configData["config.yaml"]), &config)
		require.NoError(t, err)

		assert.NotNil(t, config.Database)
		assert.Equal(t, "sqlite", config.Database.Driver)
		assert.Nil(t, config.Secrets)
	})
}

func TestEvalHubReconciler_reconcileDeployment_WithDB(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, evalhubv1alpha1.AddToScheme(scheme))

	ctx := context.Background()
	testNamespace := "test-namespace"
	evalHubName := "test-evalhub"
	dbSecretName := "evalhub-db-credentials"

	evalHub := &evalhubv1alpha1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:      evalHubName,
			Namespace: testNamespace,
		},
		Spec: evalhubv1alpha1.EvalHubSpec{
			Database: &evalhubv1alpha1.DatabaseSpec{
				Secret: dbSecretName,
			},
		},
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: testNamespace,
		},
		Data: map[string]string{
			configMapEvalHubImageKey:       "quay.io/test/eval-hub:latest",
			configMapKubeRBACProxyImageKey: "gcr.io/kubebuilder/kube-rbac-proxy:v0.13.1",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(evalHub, configMap).
		Build()

	reconciler := &EvalHubReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		Namespace:     testNamespace,
		EventRecorder: record.NewFakeRecorder(10),
	}

	t.Run("should add DB secret volume and mount when database configured", func(t *testing.T) {
		err := reconciler.reconcileDeployment(ctx, evalHub, nil)
		require.NoError(t, err)

		deployment := &appsv1.Deployment{}
		err = fakeClient.Get(ctx, types.NamespacedName{
			Name:      evalHubName,
			Namespace: testNamespace,
		}, deployment)
		require.NoError(t, err)

		// Should have 6 volumes: evalhub-config, kube-rbac-proxy-config, tls, service-ca, mlflow-token, db-secret
		assert.Len(t, deployment.Spec.Template.Spec.Volumes, 6)

		// Find the DB secret volume
		var dbVolume *corev1.Volume
		for i, v := range deployment.Spec.Template.Spec.Volumes {
			if v.Name == dbSecretVolumeName {
				dbVolume = &deployment.Spec.Template.Spec.Volumes[i]
				break
			}
		}
		require.NotNil(t, dbVolume, "DB secret volume should be present")
		require.NotNil(t, dbVolume.VolumeSource.Secret)
		assert.Equal(t, dbSecretName, dbVolume.VolumeSource.Secret.SecretName)
		require.Len(t, dbVolume.VolumeSource.Secret.Items, 1)
		assert.Equal(t, dbSecretKey, dbVolume.VolumeSource.Secret.Items[0].Key)
		assert.Equal(t, dbSecretKey, dbVolume.VolumeSource.Secret.Items[0].Path)

		// Find evalhub container and check volume mount
		var container *corev1.Container
		for i, c := range deployment.Spec.Template.Spec.Containers {
			if c.Name == containerName {
				container = &deployment.Spec.Template.Spec.Containers[i]
				break
			}
		}
		require.NotNil(t, container)
		assert.Len(t, container.VolumeMounts, 4) // evalhub-config + service-ca + mlflow-token + db-secret

		var dbMount *corev1.VolumeMount
		for i, m := range container.VolumeMounts {
			if m.Name == dbSecretVolumeName {
				dbMount = &container.VolumeMounts[i]
				break
			}
		}
		require.NotNil(t, dbMount, "DB secret volume mount should be present")
		assert.Equal(t, dbSecretMountPath, dbMount.MountPath)
		assert.True(t, dbMount.ReadOnly)
	})
}

func TestEvalHubHelperMethods_IsDatabaseConfigured(t *testing.T) {
	t.Run("should return false when Database is nil", func(t *testing.T) {
		spec := &evalhubv1alpha1.EvalHubSpec{}
		assert.False(t, spec.IsDatabaseConfigured())
	})

	t.Run("should return false when Database.Secret is empty", func(t *testing.T) {
		spec := &evalhubv1alpha1.EvalHubSpec{
			Database: &evalhubv1alpha1.DatabaseSpec{},
		}
		assert.False(t, spec.IsDatabaseConfigured())
	})

	t.Run("should return true when Database.Secret is set", func(t *testing.T) {
		spec := &evalhubv1alpha1.EvalHubSpec{
			Database: &evalhubv1alpha1.DatabaseSpec{
				Secret: "my-secret",
			},
		}
		assert.True(t, spec.IsDatabaseConfigured())
	})
}

func TestEvalHubReconciler_reconcileProviderConfigMaps(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, evalhubv1alpha1.AddToScheme(scheme))

	ctx := context.Background()
	operatorNamespace := "operator-ns"
	instanceNamespace := "instance-ns"
	evalHubName := "test-evalhub"

	// Source provider ConfigMap in the operator namespace (simulates what kustomize deploys)
	sourceProvider := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "trustyai-service-operator-evalhub-provider-testprovider",
			Namespace: operatorNamespace,
			Labels: map[string]string{
				providerLabel:     "system",
				providerNameLabel: "testprovider",
			},
		},
		Data: map[string]string{
			"testprovider.yaml": "id: testprovider\nname: Test Provider\nruntime:\n  k8s:\n    image: quay.io/test/provider:latest\n",
		},
	}

	t.Run("should copy provider ConfigMap to instance namespace", func(t *testing.T) {
		evalHub := &evalhubv1alpha1.EvalHub{
			ObjectMeta: metav1.ObjectMeta{
				Name:      evalHubName,
				Namespace: instanceNamespace,
			},
			Spec: evalhubv1alpha1.EvalHubSpec{
				Providers: []string{"testprovider"},
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(evalHub, sourceProvider).
			Build()

		reconciler := &EvalHubReconciler{
			Client:        fakeClient,
			Scheme:        scheme,
			Namespace:     operatorNamespace,
			EventRecorder: record.NewFakeRecorder(10),
		}

		cmNames, err := reconciler.reconcileProviderConfigMaps(ctx, evalHub)
		require.NoError(t, err)

		// Should return the target ConfigMap name
		require.Len(t, cmNames, 1)
		assert.Equal(t, evalHubName+"-provider-testprovider", cmNames[0])

		// Verify the ConfigMap was created in the instance namespace with correct data
		copiedCM := &corev1.ConfigMap{}
		err = fakeClient.Get(ctx, types.NamespacedName{
			Name:      evalHubName + "-provider-testprovider",
			Namespace: instanceNamespace,
		}, copiedCM)
		require.NoError(t, err)
		assert.Equal(t, sourceProvider.Data["testprovider.yaml"], copiedCM.Data["testprovider.yaml"])
	})

	t.Run("should return nil when no providers specified", func(t *testing.T) {
		evalHub := &evalhubv1alpha1.EvalHub{
			ObjectMeta: metav1.ObjectMeta{
				Name:      evalHubName,
				Namespace: instanceNamespace,
			},
			Spec: evalhubv1alpha1.EvalHubSpec{},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(evalHub).
			Build()

		reconciler := &EvalHubReconciler{
			Client:        fakeClient,
			Scheme:        scheme,
			Namespace:     operatorNamespace,
			EventRecorder: record.NewFakeRecorder(10),
		}

		cmNames, err := reconciler.reconcileProviderConfigMaps(ctx, evalHub)
		require.NoError(t, err)
		assert.Nil(t, cmNames)
	})

	t.Run("should error when provider not found", func(t *testing.T) {
		evalHub := &evalhubv1alpha1.EvalHub{
			ObjectMeta: metav1.ObjectMeta{
				Name:      evalHubName,
				Namespace: instanceNamespace,
			},
			Spec: evalhubv1alpha1.EvalHubSpec{
				Providers: []string{"nonexistent"},
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(evalHub).
			Build()

		reconciler := &EvalHubReconciler{
			Client:        fakeClient,
			Scheme:        scheme,
			Namespace:     operatorNamespace,
			EventRecorder: record.NewFakeRecorder(10),
		}

		cmNames, err := reconciler.reconcileProviderConfigMaps(ctx, evalHub)
		require.Error(t, err)
		assert.Nil(t, cmNames)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("should mount providers as projected volume in deployment", func(t *testing.T) {
		evalHub := &evalhubv1alpha1.EvalHub{
			ObjectMeta: metav1.ObjectMeta{
				Name:      evalHubName,
				Namespace: instanceNamespace,
			},
			Spec: evalhubv1alpha1.EvalHubSpec{
				Providers: []string{"testprovider"},
			},
		}

		operatorConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: operatorNamespace,
			},
			Data: map[string]string{
				configMapEvalHubImageKey:       "quay.io/test/eval-hub:latest",
				configMapKubeRBACProxyImageKey: "gcr.io/kubebuilder/kube-rbac-proxy:v0.13.1",
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(evalHub, sourceProvider, operatorConfigMap).
			Build()

		reconciler := &EvalHubReconciler{
			Client:        fakeClient,
			Scheme:        scheme,
			Namespace:     operatorNamespace,
			EventRecorder: record.NewFakeRecorder(10),
		}

		// First reconcile provider ConfigMaps
		cmNames, err := reconciler.reconcileProviderConfigMaps(ctx, evalHub)
		require.NoError(t, err)
		require.Len(t, cmNames, 1)

		// Then reconcile deployment with the provider ConfigMap names
		err = reconciler.reconcileDeployment(ctx, evalHub, cmNames)
		require.NoError(t, err)

		// Verify the deployment has the projected volume
		deployment := &appsv1.Deployment{}
		err = fakeClient.Get(ctx, types.NamespacedName{
			Name:      evalHubName,
			Namespace: instanceNamespace,
		}, deployment)
		require.NoError(t, err)

		// Find the evalhub-providers projected volume
		var providersVolume *corev1.Volume
		for i, v := range deployment.Spec.Template.Spec.Volumes {
			if v.Name == providersVolumeName {
				providersVolume = &deployment.Spec.Template.Spec.Volumes[i]
				break
			}
		}
		require.NotNil(t, providersVolume, "evalhub-providers volume should be present")
		require.NotNil(t, providersVolume.VolumeSource.Projected)
		require.Len(t, providersVolume.VolumeSource.Projected.Sources, 1)
		assert.Equal(t, evalHubName+"-provider-testprovider",
			providersVolume.VolumeSource.Projected.Sources[0].ConfigMap.Name)

		// Find the providers volume mount on the evalhub container
		var evalHubContainer *corev1.Container
		for i, c := range deployment.Spec.Template.Spec.Containers {
			if c.Name == containerName {
				evalHubContainer = &deployment.Spec.Template.Spec.Containers[i]
				break
			}
		}
		require.NotNil(t, evalHubContainer)

		var providersMount *corev1.VolumeMount
		for i, m := range evalHubContainer.VolumeMounts {
			if m.Name == providersVolumeName {
				providersMount = &evalHubContainer.VolumeMounts[i]
				break
			}
		}
		require.NotNil(t, providersMount, "providers volume mount should be present")
		assert.Equal(t, providersMountPath, providersMount.MountPath)
		assert.True(t, providersMount.ReadOnly)
	})
}

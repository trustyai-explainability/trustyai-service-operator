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
		err := reconciler.reconcileDeployment(ctx, evalHub)
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
		assert.Equal(t, corev1.PullIfNotPresent, container.ImagePullPolicy)

		// Check ports
		require.Len(t, container.Ports, 1)
		assert.Equal(t, "http", container.Ports[0].Name)
		assert.Equal(t, int32(8000), container.Ports[0].ContainerPort)

		// Check environment variables include both default and custom
		envVarMap := make(map[string]string)
		for _, env := range container.Env {
			envVarMap[env.Name] = env.Value
		}
		assert.Equal(t, "0.0.0.0", envVarMap["API_HOST"])
		assert.Equal(t, "8000", envVarMap["API_PORT"])
		assert.Equal(t, "test-value", envVarMap["TEST_VAR"])

		// Check resource requirements
		assert.Equal(t, resource.MustParse("500m"), container.Resources.Requests[corev1.ResourceCPU])
		assert.Equal(t, resource.MustParse("512Mi"), container.Resources.Requests[corev1.ResourceMemory])

		// Check health probes
		require.NotNil(t, container.LivenessProbe)
		assert.Equal(t, "/api/v1/health", container.LivenessProbe.HTTPGet.Path)
		assert.Equal(t, intstr.FromString("http"), container.LivenessProbe.HTTPGet.Port)
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

		err := reconcilerNoConfig.reconcileDeployment(ctx, evalHub)
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
		assert.Contains(t, configMap.Data, "providers.yaml")

		// Parse and validate config.yaml
		var config EvalHubConfig
		err = yaml.Unmarshal([]byte(configMap.Data["config.yaml"]), &config)
		require.NoError(t, err)

		// Check providers
		assert.Len(t, config.Providers, 4)
		providerNames := make([]string, len(config.Providers))
		for i, provider := range config.Providers {
			providerNames[i] = provider.Name
		}
		assert.Contains(t, providerNames, "lm-eval-harness")
		assert.Contains(t, providerNames, "ragas-provider")
		assert.Contains(t, providerNames, "garak-security")
		assert.Contains(t, providerNames, "trustyai-custom")

		// Check collections
		assert.Contains(t, config.Collections, "healthcare_safety_v1")
		assert.Contains(t, config.Collections, "automotive_safety_v1")

		// Parse and validate providers.yaml
		var providersData map[string]interface{}
		err = yaml.Unmarshal([]byte(configMap.Data["providers.yaml"]), &providersData)
		require.NoError(t, err)
		assert.Contains(t, providersData, "providers")
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
		assert.Contains(t, configData, "providers.yaml")

		// Parse config.yaml
		var config EvalHubConfig
		err = yaml.Unmarshal([]byte(configData["config.yaml"]), &config)
		require.NoError(t, err)

		// Verify default providers
		assert.Len(t, config.Providers, 4)

		// Find lm-eval-harness provider
		var lmEvalProvider *ProviderConfig
		for _, provider := range config.Providers {
			if provider.Name == "lm-eval-harness" {
				lmEvalProvider = &provider
				break
			}
		}
		require.NotNil(t, lmEvalProvider)
		assert.Equal(t, "lm_evaluation_harness", lmEvalProvider.Type)
		assert.True(t, lmEvalProvider.Enabled)
		assert.Contains(t, lmEvalProvider.Benchmarks, "arc_challenge")
		assert.Equal(t, "8", lmEvalProvider.Config["batch_size"])

		// Verify collections
		assert.Contains(t, config.Collections, "healthcare_safety_v1")
		assert.Contains(t, config.Collections, "automotive_safety_v1")
		assert.Contains(t, config.Collections, "finance_compliance_v1")
		assert.Contains(t, config.Collections, "general_llm_eval_v1")
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

package evalhub

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestBuildDeploymentSpec(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, evalhubv1alpha1.AddToScheme(scheme))

	ctx := context.Background()
	testNamespace := "test-namespace"
	evalHubName := "test-evalhub"

	// Create EvalHub instance with custom environment variables
	replicas := int32(3)
	evalHub := &evalhubv1alpha1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:      evalHubName,
			Namespace: testNamespace,
		},
		Spec: evalhubv1alpha1.EvalHubSpec{
			Replicas: &replicas,
			Env: []corev1.EnvVar{
				{Name: "CUSTOM_VAR", Value: "custom-value"},
				{Name: "ANOTHER_VAR", Value: "another-value"},
			},
		},
	}

	// Create config map with images
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: testNamespace,
		},
		Data: map[string]string{
			configMapEvalHubImageKey:       "quay.io/test/eval-hub:v1.2.3",
			configMapKubeRBACProxyImageKey: "gcr.io/kubebuilder/kube-rbac-proxy:v0.13.1",
		},
	}

	// Create fake client with configmap
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(configMap).
		Build()

	reconciler := &EvalHubReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		Namespace:     testNamespace,
		EventRecorder: record.NewFakeRecorder(10),
	}

	t.Run("should build correct deployment spec", func(t *testing.T) {
		deploymentSpec, err := reconciler.buildDeploymentSpec(ctx, evalHub)
		require.NoError(t, err)

		// Check replicas
		assert.Equal(t, replicas, *deploymentSpec.Replicas)

		// Check selector and labels
		expectedLabels := map[string]string{
			"app":       "eval-hub",
			"instance":  evalHubName,
			"component": "api",
		}
		assert.Equal(t, expectedLabels, deploymentSpec.Selector.MatchLabels)
		assert.Equal(t, expectedLabels, deploymentSpec.Template.ObjectMeta.Labels)

		// Check pod template
		podSpec := deploymentSpec.Template.Spec
		require.Len(t, podSpec.Containers, 2)

		// Find the evalhub container
		var container *corev1.Container
		for i, c := range podSpec.Containers {
			if c.Name == containerName {
				container = &podSpec.Containers[i]
				break
			}
		}
		require.NotNil(t, container, "evalhub container should be present")

		// Check container basic properties
		assert.Equal(t, containerName, container.Name)
		assert.Equal(t, "quay.io/test/eval-hub:v1.2.3", container.Image)
		assert.Equal(t, corev1.PullIfNotPresent, container.ImagePullPolicy)

		// Check ports
		require.Len(t, container.Ports, 1)
		port := container.Ports[0]
		assert.Equal(t, "http", port.Name)
		assert.Equal(t, int32(8080), port.ContainerPort)
		assert.Equal(t, corev1.ProtocolTCP, port.Protocol)

		// Check environment variables
		envVarMap := make(map[string]string)
		for _, env := range container.Env {
			envVarMap[env.Name] = env.Value
		}

		// Check default environment variables
		assert.Equal(t, "0.0.0.0", envVarMap["API_HOST"])
		assert.Equal(t, "8080", envVarMap["API_PORT"])
		assert.Equal(t, "INFO", envVarMap["LOG_LEVEL"])
		assert.Equal(t, "10", envVarMap["MAX_CONCURRENT_EVALUATIONS"])
		assert.Equal(t, "60", envVarMap["DEFAULT_TIMEOUT_MINUTES"])
		assert.Equal(t, "3", envVarMap["MAX_RETRY_ATTEMPTS"])

		// Check custom environment variables
		assert.Equal(t, "custom-value", envVarMap["CUSTOM_VAR"])
		assert.Equal(t, "another-value", envVarMap["ANOTHER_VAR"])

		// Check resource requirements
		assert.Equal(t, resource.MustParse("500m"), container.Resources.Requests[corev1.ResourceCPU])
		assert.Equal(t, resource.MustParse("512Mi"), container.Resources.Requests[corev1.ResourceMemory])
		assert.Equal(t, resource.MustParse("2000m"), container.Resources.Limits[corev1.ResourceCPU])
		assert.Equal(t, resource.MustParse("2Gi"), container.Resources.Limits[corev1.ResourceMemory])

		// Check security context
		require.NotNil(t, container.SecurityContext)
		assert.False(t, *container.SecurityContext.AllowPrivilegeEscalation)
		assert.True(t, *container.SecurityContext.RunAsNonRoot)
		assert.Contains(t, container.SecurityContext.Capabilities.Drop, corev1.Capability("ALL"))

		// Check health probes
		require.NotNil(t, container.LivenessProbe)
		assert.Equal(t, "/api/v1/health", container.LivenessProbe.HTTPGet.Path)
		assert.Equal(t, intstr.FromString("http"), container.LivenessProbe.HTTPGet.Port)
		assert.Equal(t, int32(30), container.LivenessProbe.InitialDelaySeconds)
		assert.Equal(t, int32(10), container.LivenessProbe.PeriodSeconds)

		require.NotNil(t, container.ReadinessProbe)
		assert.Equal(t, "/api/v1/health", container.ReadinessProbe.HTTPGet.Path)
		assert.Equal(t, intstr.FromString("http"), container.ReadinessProbe.HTTPGet.Port)
		assert.Equal(t, int32(10), container.ReadinessProbe.InitialDelaySeconds)
		assert.Equal(t, int32(5), container.ReadinessProbe.PeriodSeconds)

		// Check pod security context
		require.NotNil(t, podSpec.SecurityContext)
		assert.True(t, *podSpec.SecurityContext.RunAsNonRoot)

		// Check rolling update strategy
		assert.Equal(t, appsv1.RollingUpdateDeploymentStrategyType, deploymentSpec.Strategy.Type)
		require.NotNil(t, deploymentSpec.Strategy.RollingUpdate)
		assert.Equal(t, "25%", deploymentSpec.Strategy.RollingUpdate.MaxUnavailable.StrVal)
		assert.Equal(t, "25%", deploymentSpec.Strategy.RollingUpdate.MaxSurge.StrVal)
	})

	t.Run("should fail when configmap missing", func(t *testing.T) {
		// Create client without configmap
		fakeClientNoConfig := fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		reconcilerNoConfig := &EvalHubReconciler{
			Client:        fakeClientNoConfig,
			Scheme:        scheme,
			Namespace:     testNamespace,
			EventRecorder: record.NewFakeRecorder(10),
		}

		deploymentSpec, err := reconcilerNoConfig.buildDeploymentSpec(ctx, evalHub)
		require.Error(t, err)
		// Should return empty deployment spec (zero value) on error
		assert.Equal(t, appsv1.DeploymentSpec{}, deploymentSpec)
		assert.Contains(t, err.Error(), "kube-rbac-proxy configuration error")
	})

	t.Run("should handle default replicas when not specified", func(t *testing.T) {
		evalHubNoReplicas := &evalhubv1alpha1.EvalHub{
			ObjectMeta: metav1.ObjectMeta{
				Name:      evalHubName,
				Namespace: testNamespace,
			},
			Spec: evalhubv1alpha1.EvalHubSpec{
				// No replicas specified
			},
		}

		deploymentSpec, err := reconciler.buildDeploymentSpec(ctx, evalHubNoReplicas)
		require.NoError(t, err)

		// Should use default replicas (1)
		assert.Equal(t, int32(1), *deploymentSpec.Replicas)
	})
}

func TestBuildServiceSpec(t *testing.T) {
	evalHubName := "test-evalhub"
	testNamespace := "test-namespace"

	evalHub := &evalhubv1alpha1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:      evalHubName,
			Namespace: testNamespace,
		},
	}

	reconciler := &EvalHubReconciler{}

	t.Run("should build correct service spec", func(t *testing.T) {
		serviceSpec := reconciler.buildServiceSpec(evalHub)

		// Check basic properties
		assert.Equal(t, corev1.ServiceTypeClusterIP, serviceSpec.Type)

		// Check selector
		expectedSelector := map[string]string{
			"app":       "eval-hub",
			"instance":  evalHubName,
			"component": "api",
		}
		assert.Equal(t, expectedSelector, serviceSpec.Selector)

		// Check ports
		require.Len(t, serviceSpec.Ports, 1)
		port := serviceSpec.Ports[0]
		assert.Equal(t, "https", port.Name)
		assert.Equal(t, int32(8443), port.Port)
		assert.Equal(t, intstr.FromString("https"), port.TargetPort)
		assert.Equal(t, corev1.ProtocolTCP, port.Protocol)
	})
}

func TestGetEvalHubImage(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	ctx := context.Background()
	testNamespace := "test-namespace"

	t.Run("should get image from configmap", func(t *testing.T) {
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: testNamespace,
			},
			Data: map[string]string{
				configMapEvalHubImageKey:       "quay.io/test/eval-hub:custom",
				configMapKubeRBACProxyImageKey: "gcr.io/kubebuilder/kube-rbac-proxy:v0.13.1",
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(configMap).
			Build()

		reconciler := &EvalHubReconciler{
			Client:    fakeClient,
			Namespace: testNamespace,
		}

		image, err := reconciler.getEvalHubImage(ctx)
		require.NoError(t, err)
		assert.Equal(t, "quay.io/test/eval-hub:custom", image)
	})

	t.Run("should return fallback image when configmap not found", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		reconciler := &EvalHubReconciler{
			Client:    fakeClient,
			Namespace: testNamespace,
		}

		image, err := reconciler.getEvalHubImage(ctx)
		require.Error(t, err)                       // Error is expected when configmap not found
		assert.Equal(t, defaultEvalHubImage, image) // But fallback image is returned
	})

	t.Run("should use default namespace when namespace not set", func(t *testing.T) {
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: "trustyai-service-operator-system",
			},
			Data: map[string]string{
				configMapEvalHubImageKey:       "quay.io/test/eval-hub:default-ns",
				configMapKubeRBACProxyImageKey: "gcr.io/kubebuilder/kube-rbac-proxy:v0.13.1",
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(configMap).
			Build()

		reconciler := &EvalHubReconciler{
			Client:    fakeClient,
			Namespace: "", // Empty namespace should use default
		}

		image, err := reconciler.getEvalHubImage(ctx)
		require.NoError(t, err)
		assert.Equal(t, "quay.io/test/eval-hub:default-ns", image)
	})
}

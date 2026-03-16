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
			configMapEvalHubImageKey: "quay.io/test/eval-hub:v1.2.3",
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
		deploymentSpec, err := reconciler.buildDeploymentSpec(ctx, evalHub, nil, nil)
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
		require.Len(t, podSpec.Containers, 1)

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
		assert.Equal(t, corev1.PullAlways, container.ImagePullPolicy)

		// Check ports
		require.Len(t, container.Ports, 1)
		port := container.Ports[0]
		assert.Equal(t, "https", port.Name)
		assert.Equal(t, int32(8443), port.ContainerPort)
		assert.Equal(t, corev1.ProtocolTCP, port.Protocol)

		// Check environment variables
		envVarMap := make(map[string]string)
		for _, env := range container.Env {
			envVarMap[env.Name] = env.Value
		}

		// Check default environment variables
		assert.Equal(t, "0.0.0.0", envVarMap["API_HOST"])
		assert.Equal(t, "8443", envVarMap["PORT"])
		assert.Equal(t, "/etc/tls/private/tls.crt", envVarMap["TLS_CERT_FILE"])
		assert.Equal(t, "/etc/tls/private/tls.key", envVarMap["TLS_KEY_FILE"])
		assert.Equal(t, "INFO", envVarMap["LOG_LEVEL"])
		assert.Equal(t, "10", envVarMap["MAX_CONCURRENT_EVALUATIONS"])
		assert.Equal(t, "60", envVarMap["DEFAULT_TIMEOUT_MINUTES"])
		assert.Equal(t, "3", envVarMap["MAX_RETRY_ATTEMPTS"])

		// Check SERVICE_URL default (must match buildDeploymentSpec logic)
		assert.Equal(t, "https://test-evalhub.test-namespace.svc.cluster.local:8443", envVarMap["SERVICE_URL"])

		// Check EVALHUB_INSTANCE_NAME default (must match instance name wiring)
		assert.Equal(t, evalHubName, envVarMap["EVALHUB_INSTANCE_NAME"])

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

		// Check health probes (HTTPGet with HTTPS)
		require.NotNil(t, container.LivenessProbe)
		require.NotNil(t, container.LivenessProbe.HTTPGet)
		assert.Equal(t, "/api/v1/health", container.LivenessProbe.HTTPGet.Path)
		assert.Equal(t, corev1.URISchemeHTTPS, container.LivenessProbe.HTTPGet.Scheme)
		assert.Equal(t, int32(30), container.LivenessProbe.InitialDelaySeconds)
		assert.Equal(t, int32(10), container.LivenessProbe.PeriodSeconds)

		require.NotNil(t, container.ReadinessProbe)
		require.NotNil(t, container.ReadinessProbe.HTTPGet)
		assert.Equal(t, "/api/v1/health", container.ReadinessProbe.HTTPGet.Path)
		assert.Equal(t, corev1.URISchemeHTTPS, container.ReadinessProbe.HTTPGet.Scheme)
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

	t.Run("should use fallback image when configmap missing", func(t *testing.T) {
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

		deploymentSpec, err := reconcilerNoConfig.buildDeploymentSpec(ctx, evalHub, nil, nil)
		require.NoError(t, err)
		// Should use default EvalHub image as fallback
		var container *corev1.Container
		for i, c := range deploymentSpec.Template.Spec.Containers {
			if c.Name == containerName {
				container = &deploymentSpec.Template.Spec.Containers[i]
				break
			}
		}
		require.NotNil(t, container)
		assert.Equal(t, defaultEvalHubImage, container.Image)
	})

	t.Run("should add provider volume and mount when providerCMNames is non-nil", func(t *testing.T) {
		providerCMNames := []string{"test-evalhub-provider-lm-eval", "test-evalhub-provider-garak"}
		deploymentSpec, err := reconciler.buildDeploymentSpec(ctx, evalHub, providerCMNames, nil)
		require.NoError(t, err)

		podSpec := deploymentSpec.Template.Spec
		container := findContainer(t, podSpec.Containers, containerName)

		// Verify provider projected volume exists
		var providerVol *corev1.Volume
		for i, v := range podSpec.Volumes {
			if v.Name == providersVolumeName {
				providerVol = &podSpec.Volumes[i]
				break
			}
		}
		require.NotNil(t, providerVol, "providers volume should be present")
		require.NotNil(t, providerVol.VolumeSource.Projected)
		assert.Len(t, providerVol.VolumeSource.Projected.Sources, 2)

		// Verify provider volume mount exists
		var providerMount *corev1.VolumeMount
		for i, m := range container.VolumeMounts {
			if m.Name == providersVolumeName {
				providerMount = &container.VolumeMounts[i]
				break
			}
		}
		require.NotNil(t, providerMount, "providers volume mount should be present")
		assert.Equal(t, providersMountPath, providerMount.MountPath)
		assert.True(t, providerMount.ReadOnly)

		// Verify no collections volume when collectionCMNames is nil
		for _, v := range podSpec.Volumes {
			assert.NotEqual(t, collectionsVolumeName, v.Name, "collections volume should not be present")
		}
	})

	t.Run("should add collection volume and mount when collectionCMNames is non-nil", func(t *testing.T) {
		collectionCMNames := []string{"test-evalhub-collection-healthcare-safety"}
		deploymentSpec, err := reconciler.buildDeploymentSpec(ctx, evalHub, nil, collectionCMNames)
		require.NoError(t, err)

		podSpec := deploymentSpec.Template.Spec
		container := findContainer(t, podSpec.Containers, containerName)

		// Verify collection projected volume exists
		var collectionVol *corev1.Volume
		for i, v := range podSpec.Volumes {
			if v.Name == collectionsVolumeName {
				collectionVol = &podSpec.Volumes[i]
				break
			}
		}
		require.NotNil(t, collectionVol, "collections volume should be present")
		require.NotNil(t, collectionVol.VolumeSource.Projected)
		assert.Len(t, collectionVol.VolumeSource.Projected.Sources, 1)

		// Verify collection volume mount exists
		var collectionMount *corev1.VolumeMount
		for i, m := range container.VolumeMounts {
			if m.Name == collectionsVolumeName {
				collectionMount = &container.VolumeMounts[i]
				break
			}
		}
		require.NotNil(t, collectionMount, "collections volume mount should be present")
		assert.Equal(t, collectionsMountPath, collectionMount.MountPath)
		assert.True(t, collectionMount.ReadOnly)

		// Verify no providers volume when providerCMNames is nil
		for _, v := range podSpec.Volumes {
			assert.NotEqual(t, providersVolumeName, v.Name, "providers volume should not be present")
		}
	})

	t.Run("should add both provider and collection volumes when both are non-nil", func(t *testing.T) {
		providerCMNames := []string{"test-evalhub-provider-lm-eval"}
		collectionCMNames := []string{"test-evalhub-collection-healthcare-safety", "test-evalhub-collection-finance"}
		deploymentSpec, err := reconciler.buildDeploymentSpec(ctx, evalHub, providerCMNames, collectionCMNames)
		require.NoError(t, err)

		podSpec := deploymentSpec.Template.Spec
		container := findContainer(t, podSpec.Containers, containerName)

		// Count base volumes (evalhub-config, tls, service-ca, mlflow-token) + providers + collections
		var hasProviders, hasCollections bool
		for _, v := range podSpec.Volumes {
			if v.Name == providersVolumeName {
				hasProviders = true
			}
			if v.Name == collectionsVolumeName {
				hasCollections = true
			}
		}
		assert.True(t, hasProviders, "providers volume should be present")
		assert.True(t, hasCollections, "collections volume should be present")

		// Verify both mounts exist on the container
		var hasProviderMount, hasCollectionMount bool
		for _, m := range container.VolumeMounts {
			if m.Name == providersVolumeName {
				hasProviderMount = true
			}
			if m.Name == collectionsVolumeName {
				hasCollectionMount = true
			}
		}
		assert.True(t, hasProviderMount, "provider volume mount should be present")
		assert.True(t, hasCollectionMount, "collection volume mount should be present")
	})

	t.Run("should not include provider or collection volumes when both are nil", func(t *testing.T) {
		deploymentSpec, err := reconciler.buildDeploymentSpec(ctx, evalHub, nil, nil)
		require.NoError(t, err)

		for _, v := range deploymentSpec.Template.Spec.Volumes {
			assert.NotEqual(t, providersVolumeName, v.Name, "providers volume should not be present when nil")
			assert.NotEqual(t, collectionsVolumeName, v.Name, "collections volume should not be present when nil")
		}

		container := findContainer(t, deploymentSpec.Template.Spec.Containers, containerName)
		for _, m := range container.VolumeMounts {
			assert.NotEqual(t, providersVolumeName, m.Name, "providers mount should not be present when nil")
			assert.NotEqual(t, collectionsVolumeName, m.Name, "collections mount should not be present when nil")
		}
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

		deploymentSpec, err := reconciler.buildDeploymentSpec(ctx, evalHubNoReplicas, nil, nil)
		require.NoError(t, err)

		// Should use default replicas (1)
		assert.Equal(t, int32(1), *deploymentSpec.Replicas)
	})
}

func findContainer(t *testing.T, containers []corev1.Container, name string) *corev1.Container {
	t.Helper()
	for i, c := range containers {
		if c.Name == name {
			return &containers[i]
		}
	}
	t.Fatalf("container %q not found", name)
	return nil
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
				configMapEvalHubImageKey: "quay.io/test/eval-hub:custom",
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
				configMapEvalHubImageKey: "quay.io/test/eval-hub:default-ns",
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

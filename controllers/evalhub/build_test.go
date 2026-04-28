package evalhub

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
)

// findContainerByName returns a pointer to the container with the given name, or nil.
func findContainerByName(containers []corev1.Container, name string) *corev1.Container {
	for i := range containers {
		if containers[i].Name == name {
			return &containers[i]
		}
	}
	return nil
}

var _ = Describe("buildDeploymentSpec", func() {
	const evalHubName = "test-evalhub"

	var (
		testNamespace string
		namespace     *corev1.Namespace
		evalHub       *evalhubv1alpha1.EvalHub
		operatorCM    *corev1.ConfigMap
		reconciler    *EvalHubReconciler
		replicas      int32
	)

	BeforeEach(func() {
		testNamespace = fmt.Sprintf("evalhub-buildspec-%d", time.Now().UnixNano())
		namespace = createNamespace(testNamespace)
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

		replicas = 3
		evalHub = &evalhubv1alpha1.EvalHub{
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

		operatorCM = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: testNamespace,
			},
			Data: map[string]string{
				configMapEvalHubImageKey:       "quay.io/test/eval-hub:v1.2.3",
				configMapKubeRBACProxyImageKey: "quay.io/test/kube-rbac-proxy:v1",
			},
		}
		Expect(k8sClient.Create(ctx, operatorCM)).To(Succeed())

		reconciler = &EvalHubReconciler{
			Client:                k8sClient,
			Scheme:                scheme.Scheme,
			Namespace:             testNamespace,
			OperatorConfigMapName: configMapName,
			EventRecorder:         record.NewFakeRecorder(10),
		}
	})

	AfterEach(func() {
		if namespace != nil {
			deleteNamespace(namespace)
		}
		operatorCM, namespace = nil, nil
	})

	It("builds the expected DeploymentSpec (labels, eval-hub, kube-rbac-proxy, strategy)", func() {
		deploymentSpec, err := reconciler.buildDeploymentSpec(ctx, evalHub, nil, nil)
		Expect(err).NotTo(HaveOccurred())

		Expect(*deploymentSpec.Replicas).To(Equal(replicas))

		expectedLabels := map[string]string{
			"app":       "eval-hub",
			"instance":  evalHubName,
			"component": "api",
		}
		Expect(deploymentSpec.Selector.MatchLabels).To(Equal(expectedLabels))
		Expect(deploymentSpec.Template.ObjectMeta.Labels).To(Equal(expectedLabels))

		podSpec := deploymentSpec.Template.Spec
		Expect(podSpec.Containers).To(HaveLen(2))

		container := findContainerByName(podSpec.Containers, containerName)
		Expect(container).NotTo(BeNil(), "evalhub container should be present")

		Expect(container.Name).To(Equal(containerName))
		Expect(container.Image).To(Equal("quay.io/test/eval-hub:v1.2.3"))
		Expect(container.ImagePullPolicy).To(Equal(corev1.PullAlways))

		Expect(container.Ports).To(HaveLen(1))
		Expect(container.Ports[0].Name).To(Equal("evalhub"))
		Expect(container.Ports[0].ContainerPort).To(Equal(int32(evalHubAppPort)))
		Expect(container.Ports[0].Protocol).To(Equal(corev1.ProtocolTCP))

		envVarMap := make(map[string]string)
		for _, env := range container.Env {
			envVarMap[env.Name] = env.Value
		}
		Expect(envVarMap["API_HOST"]).To(Equal("127.0.0.1"))
		Expect(envVarMap["PORT"]).To(Equal(fmt.Sprintf("%d", evalHubAppPort)))
		Expect(envVarMap["TLS_CERT_FILE"]).To(Equal("/etc/tls/private/tls.crt"))
		Expect(envVarMap["TLS_KEY_FILE"]).To(Equal("/etc/tls/private/tls.key"))
		Expect(envVarMap["LOG_LEVEL"]).To(Equal("INFO"))
		Expect(envVarMap["MAX_CONCURRENT_EVALUATIONS"]).To(Equal("10"))
		Expect(envVarMap["DEFAULT_TIMEOUT_MINUTES"]).To(Equal("60"))
		Expect(envVarMap["MAX_RETRY_ATTEMPTS"]).To(Equal("3"))
		Expect(envVarMap["SERVICE_URL"]).To(Equal(fmt.Sprintf("https://%s.%s.svc.cluster.local:%d", evalHubName, testNamespace, servicePort)))
		Expect(envVarMap["EVALHUB_INSTANCE_NAME"]).To(Equal(evalHubName))
		Expect(envVarMap["CUSTOM_VAR"]).To(Equal("custom-value"))
		Expect(envVarMap["ANOTHER_VAR"]).To(Equal("another-value"))

		Expect(container.Resources.Requests[corev1.ResourceCPU]).To(Equal(resource.MustParse("500m")))
		Expect(container.Resources.Requests[corev1.ResourceMemory]).To(Equal(resource.MustParse("512Mi")))
		Expect(container.Resources.Limits[corev1.ResourceCPU]).To(Equal(resource.MustParse("2000m")))
		Expect(container.Resources.Limits[corev1.ResourceMemory]).To(Equal(resource.MustParse("2Gi")))

		Expect(container.SecurityContext).NotTo(BeNil())
		Expect(*container.SecurityContext.AllowPrivilegeEscalation).To(BeFalse())
		Expect(*container.SecurityContext.RunAsNonRoot).To(BeTrue())
		Expect(container.SecurityContext.Capabilities.Drop).To(ContainElement(corev1.Capability("ALL")))

		Expect(container.LivenessProbe).NotTo(BeNil())
		Expect(container.LivenessProbe.HTTPGet).NotTo(BeNil())
		Expect(container.LivenessProbe.HTTPGet.Path).To(Equal("/api/v1/health"))
		Expect(container.LivenessProbe.HTTPGet.Host).To(Equal("127.0.0.1"))
		Expect(container.LivenessProbe.HTTPGet.Port).To(Equal(intstr.FromInt(evalHubAppPort)))
		Expect(container.LivenessProbe.HTTPGet.Scheme).To(Equal(corev1.URISchemeHTTPS))
		Expect(container.LivenessProbe.InitialDelaySeconds).To(Equal(int32(30)))
		Expect(container.LivenessProbe.PeriodSeconds).To(Equal(int32(10)))
		Expect(container.LivenessProbe.TimeoutSeconds).To(Equal(int32(5)))

		Expect(container.ReadinessProbe).NotTo(BeNil())
		Expect(container.ReadinessProbe.HTTPGet).NotTo(BeNil())
		Expect(container.ReadinessProbe.HTTPGet.Path).To(Equal("/api/v1/health"))
		Expect(container.ReadinessProbe.HTTPGet.Host).To(Equal("127.0.0.1"))
		Expect(container.ReadinessProbe.HTTPGet.Port).To(Equal(intstr.FromInt(evalHubAppPort)))
		Expect(container.ReadinessProbe.HTTPGet.Scheme).To(Equal(corev1.URISchemeHTTPS))
		Expect(container.ReadinessProbe.InitialDelaySeconds).To(Equal(int32(10)))
		Expect(container.ReadinessProbe.PeriodSeconds).To(Equal(int32(5)))
		Expect(container.ReadinessProbe.TimeoutSeconds).To(Equal(int32(3)))

		krp := findContainerByName(podSpec.Containers, kubeRBACProxyContainerName)
		Expect(krp).NotTo(BeNil())
		Expect(krp.Image).To(Equal("quay.io/test/kube-rbac-proxy:v1"))
		Expect(strings.Join(krp.Args, " ")).To(ContainSubstring(fmt.Sprintf("--upstream=https://127.0.0.1:%d/", evalHubAppPort)))
		Expect(krp.ReadinessProbe).NotTo(BeNil())
		Expect(krp.ReadinessProbe.HTTPGet.Path).To(Equal("/healthz"))
		Expect(krp.ReadinessProbe.HTTPGet.Port).To(Equal(intstr.FromInt(kubeRBACProxyHealthPort)))
		Expect(krp.ReadinessProbe.HTTPGet.Scheme).To(Equal(corev1.URISchemeHTTPS))
		Expect(krp.LivenessProbe).NotTo(BeNil())
		Expect(krp.LivenessProbe.HTTPGet.Path).To(Equal("/healthz"))
		Expect(krp.LivenessProbe.HTTPGet.Port).To(Equal(intstr.FromInt(kubeRBACProxyHealthPort)))

		Expect(podSpec.SecurityContext).NotTo(BeNil())
		Expect(*podSpec.SecurityContext.RunAsNonRoot).To(BeTrue())

		Expect(deploymentSpec.Strategy.Type).To(Equal(appsv1.RollingUpdateDeploymentStrategyType))
		Expect(deploymentSpec.Strategy.RollingUpdate).NotTo(BeNil())
		Expect(deploymentSpec.Strategy.RollingUpdate.MaxUnavailable.StrVal).To(Equal("25%"))
		Expect(deploymentSpec.Strategy.RollingUpdate.MaxSurge.StrVal).To(Equal("25%"))
	})

	It("uses default EvalHub and kube-rbac-proxy images when operator ConfigMap is missing", func() {
		fallbackNS := fmt.Sprintf("evalhub-buildspec-fallback-%d", time.Now().UnixNano())
		ns := createNamespace(fallbackNS)
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		defer deleteNamespace(ns)

		r := &EvalHubReconciler{
			Client:                k8sClient,
			Scheme:                scheme.Scheme,
			Namespace:             fallbackNS,
			OperatorConfigMapName: configMapName,
			EventRecorder:         record.NewFakeRecorder(10),
		}

		deploymentSpec, err := r.buildDeploymentSpec(ctx, evalHub, nil, nil)
		Expect(err).NotTo(HaveOccurred())

		app := findContainerByName(deploymentSpec.Template.Spec.Containers, containerName)
		Expect(app).NotTo(BeNil())
		Expect(app.Image).To(Equal(defaultEvalHubImage))

		krp := findContainerByName(deploymentSpec.Template.Spec.Containers, kubeRBACProxyContainerName)
		Expect(krp).NotTo(BeNil())
		Expect(krp.Image).To(Equal(defaultKubeRBACProxyImage))
	})

	It("adds provider volume and mount when providerCMNames is non-nil", func() {
		providerCMNames := []string{"test-evalhub-provider-lm-eval", "test-evalhub-provider-garak"}
		deploymentSpec, err := reconciler.buildDeploymentSpec(ctx, evalHub, providerCMNames, nil)
		Expect(err).NotTo(HaveOccurred())

		podSpec := deploymentSpec.Template.Spec
		container := findContainerByName(podSpec.Containers, containerName)
		Expect(container).NotTo(BeNil())

		var providerVol *corev1.Volume
		for i := range podSpec.Volumes {
			if podSpec.Volumes[i].Name == providersVolumeName {
				providerVol = &podSpec.Volumes[i]
				break
			}
		}
		Expect(providerVol).NotTo(BeNil(), "providers volume should be present")
		Expect(providerVol.VolumeSource.Projected).NotTo(BeNil())
		Expect(providerVol.VolumeSource.Projected.Sources).To(HaveLen(2))

		var providerMount *corev1.VolumeMount
		for i := range container.VolumeMounts {
			if container.VolumeMounts[i].Name == providersVolumeName {
				providerMount = &container.VolumeMounts[i]
				break
			}
		}
		Expect(providerMount).NotTo(BeNil(), "providers volume mount should be present")
		Expect(providerMount.MountPath).To(Equal(providersMountPath))
		Expect(providerMount.ReadOnly).To(BeTrue())

		for _, v := range podSpec.Volumes {
			Expect(v.Name).NotTo(Equal(collectionsVolumeName), "collections volume should not be present")
		}
	})

	It("adds collection volume and mount when collectionCMNames is non-nil", func() {
		collectionCMNames := []string{"test-evalhub-collection-healthcare-safety"}
		deploymentSpec, err := reconciler.buildDeploymentSpec(ctx, evalHub, nil, collectionCMNames)
		Expect(err).NotTo(HaveOccurred())

		podSpec := deploymentSpec.Template.Spec
		container := findContainerByName(podSpec.Containers, containerName)
		Expect(container).NotTo(BeNil())

		var collectionVol *corev1.Volume
		for i := range podSpec.Volumes {
			if podSpec.Volumes[i].Name == collectionsVolumeName {
				collectionVol = &podSpec.Volumes[i]
				break
			}
		}
		Expect(collectionVol).NotTo(BeNil(), "collections volume should be present")
		Expect(collectionVol.VolumeSource.Projected).NotTo(BeNil())
		Expect(collectionVol.VolumeSource.Projected.Sources).To(HaveLen(1))

		var collectionMount *corev1.VolumeMount
		for i := range container.VolumeMounts {
			if container.VolumeMounts[i].Name == collectionsVolumeName {
				collectionMount = &container.VolumeMounts[i]
				break
			}
		}
		Expect(collectionMount).NotTo(BeNil(), "collections volume mount should be present")
		Expect(collectionMount.MountPath).To(Equal(collectionsMountPath))
		Expect(collectionMount.ReadOnly).To(BeTrue())

		for _, v := range podSpec.Volumes {
			Expect(v.Name).NotTo(Equal(providersVolumeName), "providers volume should not be present")
		}
	})

	It("adds both provider and collection volumes when both slices are non-nil", func() {
		providerCMNames := []string{"test-evalhub-provider-lm-eval"}
		collectionCMNames := []string{"test-evalhub-collection-healthcare-safety", "test-evalhub-collection-finance"}
		deploymentSpec, err := reconciler.buildDeploymentSpec(ctx, evalHub, providerCMNames, collectionCMNames)
		Expect(err).NotTo(HaveOccurred())

		podSpec := deploymentSpec.Template.Spec
		container := findContainerByName(podSpec.Containers, containerName)
		Expect(container).NotTo(BeNil())

		var hasProviders, hasCollections bool
		for _, v := range podSpec.Volumes {
			if v.Name == providersVolumeName {
				hasProviders = true
			}
			if v.Name == collectionsVolumeName {
				hasCollections = true
			}
		}
		Expect(hasProviders).To(BeTrue(), "providers volume should be present")
		Expect(hasCollections).To(BeTrue(), "collections volume should be present")

		var hasProviderMount, hasCollectionMount bool
		for _, m := range container.VolumeMounts {
			if m.Name == providersVolumeName {
				hasProviderMount = true
			}
			if m.Name == collectionsVolumeName {
				hasCollectionMount = true
			}
		}
		Expect(hasProviderMount).To(BeTrue(), "provider volume mount should be present")
		Expect(hasCollectionMount).To(BeTrue(), "collection volume mount should be present")
	})

	It("does not include provider or collection volumes when both arguments are nil", func() {
		deploymentSpec, err := reconciler.buildDeploymentSpec(ctx, evalHub, nil, nil)
		Expect(err).NotTo(HaveOccurred())

		for _, v := range deploymentSpec.Template.Spec.Volumes {
			Expect(v.Name).NotTo(Equal(providersVolumeName), "providers volume should not be present when nil")
			Expect(v.Name).NotTo(Equal(collectionsVolumeName), "collections volume should not be present when nil")
		}

		container := findContainerByName(deploymentSpec.Template.Spec.Containers, containerName)
		Expect(container).NotTo(BeNil())
		for _, m := range container.VolumeMounts {
			Expect(m.Name).NotTo(Equal(providersVolumeName), "providers mount should not be present when nil")
			Expect(m.Name).NotTo(Equal(collectionsVolumeName), "collections mount should not be present when nil")
		}
	})

	It("defaults replicas to 1 when EvalHub spec does not set replicas", func() {
		evalHubNoReplicas := &evalhubv1alpha1.EvalHub{
			ObjectMeta: metav1.ObjectMeta{
				Name:      evalHubName,
				Namespace: testNamespace,
			},
			Spec: evalhubv1alpha1.EvalHubSpec{},
		}

		deploymentSpec, err := reconciler.buildDeploymentSpec(ctx, evalHubNoReplicas, nil, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(*deploymentSpec.Replicas).To(Equal(int32(1)))
	})
})

var _ = Describe("buildServiceSpec", func() {
	const evalHubName = "test-evalhub"
	const testNamespace = "test-namespace"

	It("builds the expected Service spec", func() {
		evalHub := &evalhubv1alpha1.EvalHub{
			ObjectMeta: metav1.ObjectMeta{
				Name:      evalHubName,
				Namespace: testNamespace,
			},
		}
		r := &EvalHubReconciler{}
		serviceSpec := r.buildServiceSpec(evalHub)

		Expect(serviceSpec.Type).To(Equal(corev1.ServiceTypeClusterIP))
		expectedSelector := map[string]string{
			"app":       "eval-hub",
			"instance":  evalHubName,
			"component": "api",
		}
		Expect(serviceSpec.Selector).To(Equal(expectedSelector))

		Expect(serviceSpec.Ports).To(HaveLen(1))
		port := serviceSpec.Ports[0]
		Expect(port.Name).To(Equal("https"))
		Expect(port.Port).To(Equal(int32(8443)))
		Expect(port.TargetPort).To(Equal(intstr.FromString("https")))
		Expect(port.Protocol).To(Equal(corev1.ProtocolTCP))
	})
})

var _ = Describe("getEvalHubImage", func() {
	It("returns the image from the operator ConfigMap", func() {
		nsName := fmt.Sprintf("evalhub-getimage-%d", time.Now().UnixNano())
		ns := createNamespace(nsName)
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		defer deleteNamespace(ns)

		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: nsName,
			},
			Data: map[string]string{
				configMapEvalHubImageKey: "quay.io/test/eval-hub:custom",
			},
		}
		Expect(k8sClient.Create(ctx, cm)).To(Succeed())

		r := &EvalHubReconciler{
			Client:    k8sClient,
			Namespace: nsName,
		}
		image, err := r.getEvalHubImage(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(image).To(Equal("quay.io/test/eval-hub:custom"))
	})

	It("returns the fallback image and an error when the ConfigMap is not found", func() {
		emptyNS := fmt.Sprintf("evalhub-getimage-empty-%d", time.Now().UnixNano())
		empty := createNamespace(emptyNS)
		Expect(k8sClient.Create(ctx, empty)).To(Succeed())
		defer deleteNamespace(empty)

		r := &EvalHubReconciler{
			Client:    k8sClient,
			Namespace: emptyNS,
		}
		image, err := r.getEvalHubImage(ctx)
		Expect(err).To(HaveOccurred())
		Expect(image).To(Equal(defaultEvalHubImage))
	})

	It("resolves the operator namespace when reconciler.Namespace is empty", func() {
		const systemNS = "trustyai-service-operator-system"
		sys := createNamespace(systemNS)
		Expect(k8sClient.Create(ctx, sys)).To(Succeed())
		defer deleteNamespace(sys)

		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: systemNS,
			},
			Data: map[string]string{
				configMapEvalHubImageKey: "quay.io/test/eval-hub:default-ns",
			},
		}
		Expect(k8sClient.Create(ctx, cm)).To(Succeed())

		r := &EvalHubReconciler{
			Client:    k8sClient,
			Namespace: "",
		}
		image, err := r.getEvalHubImage(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(image).To(Equal("quay.io/test/eval-hub:default-ns"))
	})
})

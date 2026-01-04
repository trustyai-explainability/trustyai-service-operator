package evalhub

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var _ = Describe("EvalHub Deployment", func() {
	const (
		testNamespace = "evalhub-deployment-test"
		evalHubName   = "deployment-evalhub"
		configMapName = "trustyai-service-operator-config"
	)

	var (
		namespace  *corev1.Namespace
		configMap  *corev1.ConfigMap
		evalHub    *evalhubv1alpha1.EvalHub
		reconciler *EvalHubReconciler
	)

	BeforeEach(func() {
		// Create test namespace
		namespace = createNamespace(testNamespace)
		Expect(k8sClient.Create(ctx, namespace)).Should(Succeed())

		// Create config map for image configuration
		configMap = createConfigMap(configMapName, testNamespace)
		Expect(k8sClient.Create(ctx, configMap)).Should(Succeed())

		// Setup reconciler
		reconciler, _ = setupReconciler(testNamespace)
	})

	AfterEach(func() {
		// Clean up resources
		if evalHub != nil {
			k8sClient.Delete(ctx, evalHub)
		}
		if configMap != nil {
			k8sClient.Delete(ctx, configMap)
		}
		if namespace != nil {
			k8sClient.Delete(ctx, namespace)
		}
	})

	Context("When reconciling deployment", func() {
		BeforeEach(func() {
			evalHub = createEvalHubInstance(evalHubName, testNamespace)
			Expect(k8sClient.Create(ctx, evalHub)).Should(Succeed())
		})

		It("should create deployment with correct specifications", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying deployment exists")
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName,
				Namespace: testNamespace,
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Checking deployment specifications")
			Expect(deployment.Name).To(Equal(evalHubName))
			Expect(deployment.Namespace).To(Equal(testNamespace))
			Expect(*deployment.Spec.Replicas).To(Equal(*evalHub.Spec.Replicas))

			// Check owner reference
			Expect(deployment.OwnerReferences).To(HaveLen(1))
			Expect(deployment.OwnerReferences[0].Name).To(Equal(evalHub.Name))
			Expect(deployment.OwnerReferences[0].Kind).To(Equal("EvalHub"))

			// Check labels
			labels := deployment.Spec.Selector.MatchLabels
			Expect(labels["app"]).To(Equal("eval-hub"))
			Expect(labels["instance"]).To(Equal(evalHubName))
			Expect(labels["component"]).To(Equal("api"))
		})

		It("should configure container correctly", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting deployment")
			deployment := waitForDeployment(evalHubName, testNamespace)

			By("Checking container configuration")
			Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
			container := deployment.Spec.Template.Spec.Containers[0]

			Expect(container.Name).To(Equal("evalhub"))
			Expect(container.Image).To(Equal("quay.io/ruimvieira/eval-hub:test")) // From test configmap
			Expect(container.ImagePullPolicy).To(Equal(corev1.PullAlways))

			// Check ports
			Expect(container.Ports).To(HaveLen(1))
			Expect(container.Ports[0].Name).To(Equal("http"))
			Expect(container.Ports[0].ContainerPort).To(Equal(int32(8000)))
			Expect(container.Ports[0].Protocol).To(Equal(corev1.ProtocolTCP))
		})

		It("should set default environment variables", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting deployment")
			deployment := waitForDeployment(evalHubName, testNamespace)
			container := deployment.Spec.Template.Spec.Containers[0]

			By("Checking default environment variables")
			envVars := make(map[string]string)
			for _, env := range container.Env {
				envVars[env.Name] = env.Value
			}

			Expect(envVars["API_HOST"]).To(Equal("0.0.0.0"))
			Expect(envVars["API_PORT"]).To(Equal("8000"))
			Expect(envVars["LOG_LEVEL"]).To(Equal("INFO"))
			Expect(envVars["MAX_CONCURRENT_EVALUATIONS"]).To(Equal("10"))
			Expect(envVars["DEFAULT_TIMEOUT_MINUTES"]).To(Equal("60"))
			Expect(envVars["MAX_RETRY_ATTEMPTS"]).To(Equal("3"))
		})

		It("should include custom environment variables", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting deployment")
			deployment := waitForDeployment(evalHubName, testNamespace)
			container := deployment.Spec.Template.Spec.Containers[0]

			By("Checking custom environment variables")
			var hasTestEnv bool
			for _, env := range container.Env {
				if env.Name == "TEST_ENV" && env.Value == "test-value" {
					hasTestEnv = true
					break
				}
			}
			Expect(hasTestEnv).To(BeTrue())
		})

		It("should configure resource requirements", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting deployment")
			deployment := waitForDeployment(evalHubName, testNamespace)
			container := deployment.Spec.Template.Spec.Containers[0]

			By("Checking resource requirements")
			Expect(container.Resources.Requests).NotTo(BeNil())
			Expect(container.Resources.Limits).NotTo(BeNil())

			// Check CPU and memory are set (exact values from constants.go)
			Expect(container.Resources.Requests[corev1.ResourceCPU]).To(Equal(resource.MustParse("500m")))
			Expect(container.Resources.Requests[corev1.ResourceMemory]).To(Equal(resource.MustParse("512Mi")))
			Expect(container.Resources.Limits[corev1.ResourceCPU]).To(Equal(resource.MustParse("2000m")))
			Expect(container.Resources.Limits[corev1.ResourceMemory]).To(Equal(resource.MustParse("2Gi")))
		})

		It("should configure health probes", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting deployment")
			deployment := waitForDeployment(evalHubName, testNamespace)
			container := deployment.Spec.Template.Spec.Containers[0]

			By("Checking liveness probe")
			Expect(container.LivenessProbe).NotTo(BeNil())
			Expect(container.LivenessProbe.HTTPGet.Path).To(Equal("/api/v1/health"))
			Expect(container.LivenessProbe.HTTPGet.Port).To(Equal(intstr.FromString("http")))
			Expect(container.LivenessProbe.InitialDelaySeconds).To(Equal(int32(30)))
			Expect(container.LivenessProbe.PeriodSeconds).To(Equal(int32(10)))

			By("Checking readiness probe")
			Expect(container.ReadinessProbe).NotTo(BeNil())
			Expect(container.ReadinessProbe.HTTPGet.Path).To(Equal("/api/v1/health"))
			Expect(container.ReadinessProbe.HTTPGet.Port).To(Equal(intstr.FromString("http")))
			Expect(container.ReadinessProbe.InitialDelaySeconds).To(Equal(int32(10)))
			Expect(container.ReadinessProbe.PeriodSeconds).To(Equal(int32(5)))
		})

		It("should configure security contexts", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting deployment")
			deployment := waitForDeployment(evalHubName, testNamespace)

			By("Checking pod security context")
			podSecurityContext := deployment.Spec.Template.Spec.SecurityContext
			Expect(podSecurityContext).NotTo(BeNil())
			Expect(*podSecurityContext.RunAsNonRoot).To(BeTrue())
			Expect(*podSecurityContext.FSGroup).To(Equal(int64(1001)))

			By("Checking container security context")
			container := deployment.Spec.Template.Spec.Containers[0]
			containerSecurityContext := container.SecurityContext
			Expect(containerSecurityContext).NotTo(BeNil())
			Expect(*containerSecurityContext.AllowPrivilegeEscalation).To(BeFalse())
			Expect(*containerSecurityContext.RunAsNonRoot).To(BeTrue())
			Expect(*containerSecurityContext.RunAsUser).To(Equal(int64(1001)))
			Expect(containerSecurityContext.Capabilities.Drop).To(ContainElement(corev1.Capability("ALL")))
		})

		It("should update existing deployment", func() {
			By("Creating initial deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Modifying EvalHub replicas")
			newReplicas := int32(3)
			evalHub.Spec.Replicas = &newReplicas
			err = k8sClient.Update(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Reconciling deployment again")
			err = reconciler.reconcileDeployment(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying deployment is updated")
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName,
				Namespace: testNamespace,
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(*deployment.Spec.Replicas).To(Equal(int32(3)))
		})

		It("should use fallback image when config map is missing", func() {
			By("Deleting config map")
			err := k8sClient.Delete(ctx, configMap)
			Expect(err).NotTo(HaveOccurred())

			By("Reconciling deployment without config map")
			err = reconciler.reconcileDeployment(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying fallback image is used")
			deployment := waitForDeployment(evalHubName, testNamespace)
			container := deployment.Spec.Template.Spec.Containers[0]
			Expect(container.Image).To(Equal(defaultEvalHubImage))
		})

		It("should configure rolling update strategy", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting deployment")
			deployment := waitForDeployment(evalHubName, testNamespace)

			By("Checking deployment strategy")
			Expect(deployment.Spec.Strategy.Type).To(Equal(appsv1.RollingUpdateDeploymentStrategyType))
			Expect(deployment.Spec.Strategy.RollingUpdate).NotTo(BeNil())
			Expect(deployment.Spec.Strategy.RollingUpdate.MaxUnavailable.StrVal).To(Equal("25%"))
			Expect(deployment.Spec.Strategy.RollingUpdate.MaxSurge.StrVal).To(Equal("25%"))
		})
	})

	Context("When handling deployment errors", func() {
		It("should handle missing EvalHub instance", func() {
			By("Creating deployment spec for non-existent EvalHub")
			nonExistentEvalHub := &evalhubv1alpha1.EvalHub{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "non-existent",
					Namespace: testNamespace,
				},
			}

			By("Attempting to reconcile deployment")
			err := reconciler.reconcileDeployment(ctx, nonExistentEvalHub)
			Expect(err).To(HaveOccurred())
		})
	})
})

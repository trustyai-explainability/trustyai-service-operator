package evalhub

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
)

var _ = Describe("EvalHub Controller", func() {
	const (
		testNamespacePrefix = "evalhub-test"
		evalHubName         = "test-evalhub"
		configMapName       = "trustyai-service-operator-config"
	)

	var (
		testNamespace string
		namespace     *corev1.Namespace
		configMap     *corev1.ConfigMap
		evalHub       *evalhubv1alpha1.EvalHub
		reconciler    *EvalHubReconciler
	)

	BeforeEach(func() {
		// Create unique namespace name to avoid conflicts
		testNamespace = fmt.Sprintf("%s-%d", testNamespacePrefix, time.Now().UnixNano())

		// Create test namespace
		namespace = createNamespace(testNamespace)
		Expect(k8sClient.Create(ctx, namespace)).Should(Succeed())

		// Create config map for image configuration
		configMap = createConfigMap(configMapName, testNamespace)
		Expect(k8sClient.Create(ctx, configMap)).Should(Succeed())

		// Create EvalHub instance
		evalHub = createEvalHubInstance(evalHubName, testNamespace)
		Expect(k8sClient.Create(ctx, evalHub)).Should(Succeed())

		// Setup reconciler
		reconciler, _ = setupReconciler(testNamespace)
	})

	AfterEach(func() {
		// Clean up resources in namespace first
		cleanupResourcesInNamespace(testNamespace, evalHub, configMap)

		// Then delete namespace
		deleteNamespace(namespace)

		// Reset variables
		evalHub = nil
		configMap = nil
		namespace = nil
	})

	Context("When reconciling an EvalHub", func() {
		It("should initialize status to Pending", func() {
			By("Performing reconciliation")
			result, err := performReconcile(reconciler, evalHubName, testNamespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(time.Second * 5))

			By("Checking initial status")
			updatedEvalHub := &evalhubv1alpha1.EvalHub{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName,
				Namespace: testNamespace,
			}, updatedEvalHub)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedEvalHub.Status.Phase).To(Equal("Pending"))
			Expect(updatedEvalHub.Status.Ready).To(Equal(corev1.ConditionFalse))
		})

		It("should add finalizer to EvalHub instance", func() {
			By("Performing multiple reconciliations")
			// First reconcile sets status
			performReconcile(reconciler, evalHubName, testNamespace)
			// Second reconcile adds finalizer
			result, err := performReconcile(reconciler, evalHubName, testNamespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(time.Second * 5))

			By("Checking finalizer is added")
			updatedEvalHub := &evalhubv1alpha1.EvalHub{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName,
				Namespace: testNamespace,
			}, updatedEvalHub)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedEvalHub.GetFinalizers()).To(ContainElement(evalhubv1alpha1.FinalizerName))
		})

		It("should handle EvalHub deletion", func() {
			By("Setting up finalizer first")
			evalHub.SetFinalizers([]string{evalhubv1alpha1.FinalizerName})
			err := k8sClient.Update(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Deleting the EvalHub")
			err = k8sClient.Delete(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Performing reconciliation on deleted instance")
			result, err := performReconcile(reconciler, evalHubName, testNamespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			By("Checking that EvalHub is removed")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      evalHubName,
					Namespace: testNamespace,
				}, &evalhubv1alpha1.EvalHub{})
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())
		})

		It("should handle missing EvalHub gracefully", func() {
			By("Reconciling a non-existent EvalHub")
			result, err := performReconcile(reconciler, "non-existent", testNamespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())
		})
	})

	Context("When reconciling with errors", func() {
		It("should requeue on errors", func() {
			By("Performing initial reconciliations to set status and finalizer")
			performReconcile(reconciler, evalHubName, testNamespace)
			performReconcile(reconciler, evalHubName, testNamespace)

			By("Performing reconciliation")
			badReconciler := &EvalHubReconciler{
				Client:        k8sClient,
				Scheme:        runtime.NewScheme(),
				Namespace:     testNamespace,
				EventRecorder: record.NewFakeRecorder(100),
			}
			result, err := performReconcile(badReconciler, evalHubName, testNamespace)
			Expect(err).To(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())
		})
	})
})

var _ = Describe("EvalHub Lifecycle Integration", func() {
	const (
		testNamespacePrefix = "evalhub-lifecycle-test"
		evalHubName         = "lifecycle-evalhub"
		configMapName       = "trustyai-service-operator-config"
	)

	var (
		testNamespace string
		namespace     *corev1.Namespace
		configMap     *corev1.ConfigMap
		evalHub       *evalhubv1alpha1.EvalHub
		reconciler    *EvalHubReconciler
	)

	BeforeEach(func() {
		// Create unique namespace name to avoid conflicts
		testNamespace = fmt.Sprintf("%s-%d", testNamespacePrefix, time.Now().UnixNano())

		// Create test namespace
		namespace = createNamespace(testNamespace)
		Expect(k8sClient.Create(ctx, namespace)).Should(Succeed())

		// Create config map for image configuration
		configMap = createConfigMap(configMapName, testNamespace)
		Expect(k8sClient.Create(ctx, configMap)).Should(Succeed())

		// Create EvalHub instance
		evalHub = createEvalHubInstance(evalHubName, testNamespace)
		Expect(k8sClient.Create(ctx, evalHub)).Should(Succeed())

		// Setup reconciler
		reconciler, _ = setupReconciler(testNamespace)
	})

	AfterEach(func() {
		// Clean up resources in namespace first
		cleanupResourcesInNamespace(testNamespace, evalHub, configMap)

		// Then delete namespace
		deleteNamespace(namespace)

		// Reset variables
		evalHub = nil
		configMap = nil
		namespace = nil
	})

	It("should create all required resources through complete reconciliation", func() {
		By("Performing initial reconciliation to set status")
		result, err := performReconcile(reconciler, evalHubName, testNamespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(time.Second * 5))

		By("Performing second reconciliation to add finalizer")
		result, err = performReconcile(reconciler, evalHubName, testNamespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(time.Second * 5))

		By("Performing third reconciliation to create resources")
		result, err = performReconcile(reconciler, evalHubName, testNamespace)
		Expect(err).NotTo(HaveOccurred())

		By("Checking that ConfigMap is created")
		configMapCreated := waitForConfigMap(evalHubName+"-config", testNamespace)
		Expect(configMapCreated.Data).To(HaveKey("config.yaml"))
		Expect(configMapCreated.Data).To(HaveKey("providers.yaml"))

		By("Checking that Deployment is created")
		deployment := waitForDeployment(evalHubName, testNamespace)
		Expect(deployment.Spec.Replicas).To(Equal(evalHub.Spec.Replicas))
		Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(2))

		// Find the evalhub container
		var container *corev1.Container
		for i, c := range deployment.Spec.Template.Spec.Containers {
			if c.Name == "evalhub" {
				container = &deployment.Spec.Template.Spec.Containers[i]
				break
			}
		}
		Expect(container).NotTo(BeNil(), "evalhub container should be present")
		Expect(container.Name).To(Equal("evalhub"))
		Expect(container.Image).To(Equal("quay.io/ruimvieira/eval-hub:test"))
		Expect(container.Ports[0].ContainerPort).To(Equal(int32(8000)))

		// Check custom environment variables are included
		var hasTestEnv bool
		for _, env := range container.Env {
			if env.Name == "TEST_ENV" && env.Value == "test-value" {
				hasTestEnv = true
				break
			}
		}
		Expect(hasTestEnv).To(BeTrue())

		By("Checking that Service is created")
		service := waitForService(evalHubName, testNamespace)
		Expect(service.Spec.Ports[0].Port).To(Equal(int32(8443)))
		Expect(service.Spec.Selector["app"]).To(Equal("eval-hub"))
		Expect(service.Spec.Selector["instance"]).To(Equal(evalHubName))
	})

	It("should update status based on deployment readiness", func() {
		By("Going through initial reconciliation steps")
		// Status initialization
		performReconcile(reconciler, evalHubName, testNamespace)
		// Finalizer addition
		performReconcile(reconciler, evalHubName, testNamespace)
		// Resource creation
		performReconcile(reconciler, evalHubName, testNamespace)

		By("Checking initial status is Pending")
		waitForEvalHubStatus(evalHubName, testNamespace, "Pending")

		By("Simulating deployment becoming ready")
		deployment := &appsv1.Deployment{}
		err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      evalHubName,
			Namespace: testNamespace,
		}, deployment)
		Expect(err).NotTo(HaveOccurred())

		// Update deployment status to simulate ready state
		deployment.Status.Replicas = *deployment.Spec.Replicas
		deployment.Status.ReadyReplicas = *deployment.Spec.Replicas
		err = k8sClient.Status().Update(ctx, deployment)
		Expect(err).NotTo(HaveOccurred())

		By("Performing reconciliation after deployment is ready")
		result, err := performReconcile(reconciler, evalHubName, testNamespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(time.Minute * 5))

		By("Checking status is updated to Ready")
		waitForEvalHubStatus(evalHubName, testNamespace, "Ready")

		// Verify full status
		updatedEvalHub := &evalhubv1alpha1.EvalHub{}
		err = k8sClient.Get(ctx, types.NamespacedName{
			Name:      evalHubName,
			Namespace: testNamespace,
		}, updatedEvalHub)
		Expect(err).NotTo(HaveOccurred())
		Expect(updatedEvalHub.Status.Ready).To(Equal(corev1.ConditionTrue))
		Expect(updatedEvalHub.Status.Replicas).To(Equal(int32(1)))
		Expect(updatedEvalHub.Status.ReadyReplicas).To(Equal(int32(1)))
		Expect(updatedEvalHub.Status.URL).To(ContainSubstring(evalHubName))
	})
})

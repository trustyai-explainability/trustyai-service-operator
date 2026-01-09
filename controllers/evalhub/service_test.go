package evalhub

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var _ = Describe("EvalHub Service", func() {
	const (
		testNamespacePrefix = "evalhub-service-test"
		evalHubName         = "service-evalhub"
	)

	var (
		testNamespace string
		namespace     *corev1.Namespace
		evalHub       *evalhubv1alpha1.EvalHub
		reconciler    *EvalHubReconciler
	)

	BeforeEach(func() {
		// Create unique namespace name to avoid conflicts
		testNamespace = fmt.Sprintf("%s-%d", testNamespacePrefix, time.Now().UnixNano())

		// Create test namespace
		namespace = createNamespace(testNamespace)
		Expect(k8sClient.Create(ctx, namespace)).Should(Succeed())

		// Create EvalHub instance
		evalHub = createEvalHubInstance(evalHubName, testNamespace)
		Expect(k8sClient.Create(ctx, evalHub)).Should(Succeed())

		// Setup reconciler
		reconciler, _ = setupReconciler(testNamespace)
	})

	AfterEach(func() {
		// Clean up resources in namespace first
		cleanupResourcesInNamespace(testNamespace, evalHub, nil)

		// Then delete namespace
		deleteNamespace(namespace)

		// Reset variables
		evalHub = nil
		namespace = nil
	})

	Context("When reconciling service", func() {
		It("should create service with correct specifications", func() {
			By("Reconciling service")
			err := reconciler.reconcileService(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying service exists")
			service := &corev1.Service{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName,
				Namespace: testNamespace,
			}, service)
			Expect(err).NotTo(HaveOccurred())

			By("Checking service specifications")
			Expect(service.Name).To(Equal(evalHubName))
			Expect(service.Namespace).To(Equal(testNamespace))

			// Check owner reference
			Expect(service.OwnerReferences).To(HaveLen(1))
			Expect(service.OwnerReferences[0].Name).To(Equal(evalHub.Name))
			Expect(service.OwnerReferences[0].Kind).To(Equal("EvalHub"))

			// Check labels
			Expect(service.Labels["app"]).To(Equal("eval-hub"))
			Expect(service.Labels["instance"]).To(Equal(evalHubName))
			Expect(service.Labels["component"]).To(Equal("api"))
		})

		It("should configure service ports correctly for kube-rbac-proxy", func() {
			By("Reconciling service")
			err := reconciler.reconcileService(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting service")
			service := waitForService(evalHubName, testNamespace)

			By("Checking service ports")
			Expect(service.Spec.Ports).To(HaveLen(1))

			port := service.Spec.Ports[0]
			Expect(port.Name).To(Equal("https"))
			Expect(port.Port).To(Equal(int32(8443)))
			Expect(port.TargetPort).To(Equal(intstr.FromString("https")))
			Expect(port.Protocol).To(Equal(corev1.ProtocolTCP))
		})

		It("should configure service selector", func() {
			By("Reconciling service")
			err := reconciler.reconcileService(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting service")
			service := waitForService(evalHubName, testNamespace)

			By("Checking service selector")
			Expect(service.Spec.Selector["app"]).To(Equal("eval-hub"))
			Expect(service.Spec.Selector["instance"]).To(Equal(evalHubName))
			Expect(service.Spec.Selector["component"]).To(Equal("api"))
		})

		It("should set service type to ClusterIP", func() {
			By("Reconciling service")
			err := reconciler.reconcileService(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting service")
			service := waitForService(evalHubName, testNamespace)

			By("Checking service type")
			Expect(service.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
		})

		It("should update existing service", func() {
			By("Creating initial service")
			err := reconciler.reconcileService(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting initial service")
			service := &corev1.Service{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName,
				Namespace: testNamespace,
			}, service)
			Expect(err).NotTo(HaveOccurred())

			By("Manually modifying service selector")
			service.Spec.Selector["component"] = "wrong"
			err = k8sClient.Update(ctx, service)
			Expect(err).NotTo(HaveOccurred())

			// Store resource version after manual change
			initialResourceVersion := service.ResourceVersion

			By("Reconciling service again")
			err = reconciler.reconcileService(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying service is updated")
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName,
				Namespace: testNamespace,
			}, service)
			Expect(err).NotTo(HaveOccurred())

			// Resource version should change when service is updated
			Expect(service.ResourceVersion).NotTo(Equal(initialResourceVersion))
			Expect(service.Spec.Selector["component"]).To(Equal("api"))
		})

		It("should preserve service annotations and labels", func() {
			By("Reconciling service")
			err := reconciler.reconcileService(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Adding custom annotation to service")
			service := &corev1.Service{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName,
				Namespace: testNamespace,
			}, service)
			Expect(err).NotTo(HaveOccurred())

			if service.Annotations == nil {
				service.Annotations = make(map[string]string)
			}
			service.Annotations["custom.annotation"] = "test-value"
			err = k8sClient.Update(ctx, service)
			Expect(err).NotTo(HaveOccurred())

			By("Reconciling service again")
			err = reconciler.reconcileService(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying custom annotation is preserved")
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName,
				Namespace: testNamespace,
			}, service)
			Expect(err).NotTo(HaveOccurred())
			Expect(service.Annotations["custom.annotation"]).To(Equal("test-value"))

			By("Verifying required labels are still present")
			Expect(service.Labels["app"]).To(Equal("eval-hub"))
			Expect(service.Labels["instance"]).To(Equal(evalHubName))
		})

		It("should handle service spec changes", func() {
			By("Creating initial service")
			err := reconciler.reconcileService(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Manually modifying service spec")
			service := &corev1.Service{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName,
				Namespace: testNamespace,
			}, service)
			Expect(err).NotTo(HaveOccurred())

			// Change port to something different
			service.Spec.Ports[0].Port = 9000
			err = k8sClient.Update(ctx, service)
			Expect(err).NotTo(HaveOccurred())

			By("Reconciling service to restore correct spec")
			err = reconciler.reconcileService(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying service spec is restored")
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName,
				Namespace: testNamespace,
			}, service)
			Expect(err).NotTo(HaveOccurred())
			Expect(service.Spec.Ports[0].Port).To(Equal(int32(8443)))
		})
	})

	Context("When handling service errors", func() {
		It("should handle missing EvalHub instance", func() {
			By("Creating service for non-existent EvalHub")
			nonExistentEvalHub := &evalhubv1alpha1.EvalHub{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "non-existent",
					Namespace: testNamespace,
				},
			}

			By("Attempting to reconcile service")
			err := reconciler.reconcileService(ctx, nonExistentEvalHub)
			Expect(err).To(HaveOccurred())
		})

		It("should handle service creation in non-existent namespace", func() {
			By("Creating EvalHub in non-existent namespace")
			badEvalHub := createEvalHubInstance("bad-service-evalhub", "non-existent-namespace")

			By("Attempting to reconcile service")
			badReconciler, _ := setupReconciler("non-existent-namespace")
			err := badReconciler.reconcileService(ctx, badEvalHub)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("Service integration with deployment", func() {
		It("should have matching selectors with deployment labels", func() {
			By("Reconciling service")
			err := reconciler.reconcileService(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting service")
			service := waitForService(evalHubName, testNamespace)

			By("Checking service selector matches expected deployment labels")
			expectedLabels := map[string]string{
				"app":       "eval-hub",
				"instance":  evalHubName,
				"component": "api",
			}

			for key, value := range expectedLabels {
				Expect(service.Spec.Selector[key]).To(Equal(value))
			}
		})

		It("should have service port matching container port", func() {
			By("Reconciling service")
			err := reconciler.reconcileService(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting service")
			service := waitForService(evalHubName, testNamespace)

			By("Verifying service port matches expected container port")
			Expect(service.Spec.Ports[0].Port).To(Equal(int32(8443)))
			Expect(service.Spec.Ports[0].TargetPort.StrVal).To(Equal("https"))
		})
	})
})

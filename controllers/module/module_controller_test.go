package module

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	modulev1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/module/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var _ = Describe("TrustyAI Module Controller", func() {
	var reconciler *TrustyAIReconciler

	BeforeEach(func() {
		reconciler = setupReconciler()
	})

	AfterEach(func() {
		// Cleanup all TrustyAI instances
		moduleList := &modulev1alpha1.TrustyAIList{}
		_ = k8sClient.List(ctx, moduleList)
		for i := range moduleList.Items {
			module := &moduleList.Items[i]
			_ = k8sClient.Delete(ctx, module)
			// Trigger reconcile to handle deletion and remove finalizer
			_, _ = performReconcile(reconciler, module.Name)
		}

		// Wait for all deletions to complete
		Eventually(func() int {
			moduleList := &modulev1alpha1.TrustyAIList{}
			_ = k8sClient.List(ctx, moduleList)
			return len(moduleList.Items)
		}, timeout, interval).Should(Equal(0))
	})

	Context("When reconciling a TrustyAI module", func() {
		It("Should create and add finalizer", func() {
			module := createModuleInstance("default", modulev1alpha1.ManagementStateManaged)
			Expect(k8sClient.Create(ctx, module)).To(Succeed())

			// Reconcile
			result, err := performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeTrue())

			// Check finalizer was added
			updated := &modulev1alpha1.TrustyAI{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "default"}, updated)).To(Succeed())
			Expect(controllerutil.ContainsFinalizer(updated, FinalizerName)).To(BeTrue())
		})

		It("Should update observedGeneration on reconcile", func() {
			module := createModuleInstance("default", modulev1alpha1.ManagementStateManaged)
			Expect(k8sClient.Create(ctx, module)).To(Succeed())

			// First reconcile - adds finalizer
			_, err := performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile - should update observedGeneration
			result, err := performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(time.Duration(60) * time.Second))

			// Check observedGeneration was updated
			updated := &modulev1alpha1.TrustyAI{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "default"}, updated)).To(Succeed())
			Expect(updated.Status.ObservedGeneration).To(Equal(module.Generation))
		})

		It("Should set Ready condition when managed", func() {
			module := createModuleInstance("default", modulev1alpha1.ManagementStateManaged)
			Expect(k8sClient.Create(ctx, module)).To(Succeed())

			// First reconcile - adds finalizer
			_, err := performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile - sets conditions
			_, err = performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())

			// Check Ready condition
			// Until health checker integration (RHOAIENG-67662), should be NotReady
			updated := &modulev1alpha1.TrustyAI{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "default"}, updated)).To(Succeed())

			readyCondition := meta.FindStatusCondition(updated.Status.Conditions, ConditionTypeReady)
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCondition.Reason).To(Equal("HealthCheckPending"))
		})

		It("Should set ProvisioningSucceeded condition", func() {
			module := createModuleInstance("default", modulev1alpha1.ManagementStateManaged)
			Expect(k8sClient.Create(ctx, module)).To(Succeed())

			// First reconcile - adds finalizer
			_, err := performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile - sets conditions
			_, err = performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())

			// Check ProvisioningSucceeded condition
			// Until health checker integration (RHOAIENG-67662), should be False/Pending
			updated := &modulev1alpha1.TrustyAI{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "default"}, updated)).To(Succeed())

			provisioningCondition := meta.FindStatusCondition(updated.Status.Conditions, ConditionTypeProvisioningSucceeded)
			Expect(provisioningCondition).NotTo(BeNil())
			Expect(provisioningCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(provisioningCondition.Reason).To(Equal("HealthCheckPending"))
		})

		It("Should set phase to NotReady pending health checker", func() {
			module := createModuleInstance("default", modulev1alpha1.ManagementStateManaged)
			Expect(k8sClient.Create(ctx, module)).To(Succeed())

			// First reconcile - adds finalizer
			_, err := performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile - sets phase
			_, err = performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())

			// Check phase
			// Until health checker integration (RHOAIENG-67662), should be NotReady
			updated := &modulev1alpha1.TrustyAI{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "default"}, updated)).To(Succeed())
			Expect(updated.Status.Phase).To(Equal(PhaseNotReady))
		})

		It("Should handle Removed management state", func() {
			module := createModuleInstance("default", modulev1alpha1.ManagementStateRemoved)
			Expect(k8sClient.Create(ctx, module)).To(Succeed())

			// First reconcile - adds finalizer
			_, err := performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile - handles removal
			_, err = performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())

			// Check phase and condition
			updated := &modulev1alpha1.TrustyAI{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "default"}, updated)).To(Succeed())
			Expect(updated.Status.Phase).To(Equal(PhaseNotReady))

			readyCondition := meta.FindStatusCondition(updated.Status.Conditions, ConditionTypeReady)
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCondition.Reason).To(Equal("ModuleRemoved"))
		})

		It("Should remove finalizer on deletion", func() {
			module := createModuleInstance("default", modulev1alpha1.ManagementStateManaged)
			Expect(k8sClient.Create(ctx, module)).To(Succeed())

			// First reconcile - adds finalizer
			_, err := performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())

			// Delete the module
			Expect(k8sClient.Delete(ctx, module)).To(Succeed())

			// Reconcile deletion
			_, err = performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())

			// Check module is deleted (finalizer was removed)
			deleted := &modulev1alpha1.TrustyAI{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "default"}, deleted)
				return apierrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())
		})

		It("Should populate releases field", func() {
			module := createModuleInstance("default", modulev1alpha1.ManagementStateManaged)
			Expect(k8sClient.Create(ctx, module)).To(Succeed())

			// First reconcile - adds finalizer
			_, err := performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile - populates releases
			_, err = performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())

			// Check releases
			updated := &modulev1alpha1.TrustyAI{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "default"}, updated)).To(Succeed())
			Expect(updated.Status.Releases).NotTo(BeEmpty())
			Expect(updated.Status.Releases[0].Name).To(Equal("trustyai-service-operator"))
		})
	})
})

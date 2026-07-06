package module

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	modulev1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/module/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Dependency Validation", func() {
	var reconciler *TrustyAIReconciler

	BeforeEach(func() {
		reconciler = setupReconciler()
		// Enable dependency checks for these tests
		reconciler.SkipDependencyChecks = false
	})

	AfterEach(func() {
		// Cleanup module instances
		moduleList := &modulev1alpha1.TrustyAIList{}
		_ = k8sClient.List(ctx, moduleList)
		for i := range moduleList.Items {
			module := &moduleList.Items[i]
			_ = k8sClient.Delete(ctx, module)
			_, _ = performReconcile(reconciler, module.Name)
		}

		// Wait for all deletions to complete
		Eventually(func() int {
			moduleList := &modulev1alpha1.TrustyAIList{}
			_ = k8sClient.List(ctx, moduleList)
			return len(moduleList.Items)
		}, timeout, interval).Should(Equal(0))

	})

	Context("When dependencies are missing", func() {
		It("Should block deployment and set DependenciesMet=False", func() {
			module := createModuleInstance("default", modulev1alpha1.ManagementStateManaged)
			Expect(k8sClient.Create(ctx, module)).To(Succeed())

			// First reconcile - add finalizer
			result, err := performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeTrue())

			// Second reconcile - check dependencies (should fail because CRDs don't exist in envtest)
			result, err = performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())

			// Verify status
			updated := &modulev1alpha1.TrustyAI{}
			Eventually(func() bool {
				_ = k8sClient.Get(ctx, types.NamespacedName{Name: "default"}, updated)
				return meta.IsStatusConditionPresentAndEqual(updated.Status.Conditions, ConditionTypeDependenciesMet, "False")
			}, timeout, interval).Should(BeTrue())

			// Verify Ready condition is also False
			Expect(meta.IsStatusConditionPresentAndEqual(updated.Status.Conditions, ConditionTypeReady, "False")).To(BeTrue())

			// Verify phase is NotReady
			Expect(updated.Status.Phase).To(Equal(PhaseNotReady))

			// Verify message mentions missing dependencies
			depCondition := meta.FindStatusCondition(updated.Status.Conditions, ConditionTypeDependenciesMet)
			Expect(depCondition).NotTo(BeNil())
			Expect(depCondition.Reason).To(Equal("DependenciesMissing"))
			// In envtest, CRDs don't exist, so both dependencies should fail
			Expect(depCondition.Message).To(ContainSubstring("Missing dependencies"))
		})
	})

	// Note: The following tests would require SMCP and Prometheus CRDs to be installed in envtest
	// In a real cluster with those CRDs, the dependency checks would work correctly
	// For now, we test the blocking behavior when dependencies are missing
})


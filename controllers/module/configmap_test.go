package module

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	modulev1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/module/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("DSC ConfigMap", func() {
	var reconciler *TrustyAIReconciler

	BeforeEach(func() {
		reconciler = setupReconciler()
	})

	AfterEach(func() {
		// Cleanup ConfigMap
		cm := &corev1.ConfigMap{}
		_ = k8sClient.Get(ctx, types.NamespacedName{
			Name:      DSCConfigMapName,
			Namespace: reconciler.Namespace,
		}, cm)
		_ = k8sClient.Delete(ctx, cm)

		// Cleanup module instances
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

	Context("When creating ConfigMap", func() {
		It("Should create DSC ConfigMap on first reconcile", func() {
			module := createModuleInstance("default", modulev1alpha1.ManagementStateManaged)
			module.Spec.Eval.LMEval.PermitCodeExecution = true
			module.Spec.Eval.LMEval.PermitOnline = false
			Expect(k8sClient.Create(ctx, module)).To(Succeed())

			// First reconcile - add finalizer
			_, err := performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile - create ConfigMap
			_, err = performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())

			// Verify ConfigMap was created
			cm := &corev1.ConfigMap{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      DSCConfigMapName,
					Namespace: reconciler.Namespace,
				}, cm)
			}, timeout, interval).Should(Succeed())

			// Verify ConfigMap data
			Expect(cm.Data).To(HaveKey(LMEvalPermitCodeExecutionKey))
			Expect(cm.Data[LMEvalPermitCodeExecutionKey]).To(Equal("true"))
			Expect(cm.Data).To(HaveKey(LMEvalPermitOnlineKey))
			Expect(cm.Data[LMEvalPermitOnlineKey]).To(Equal("false"))
		})

		It("Should create ConfigMap with default values when eval not specified", func() {
			module := createModuleInstance("default", modulev1alpha1.ManagementStateManaged)
			// Don't set eval fields - use defaults
			Expect(k8sClient.Create(ctx, module)).To(Succeed())

			// First reconcile - add finalizer
			_, err := performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile - create ConfigMap
			_, err = performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())

			// Verify ConfigMap was created with defaults
			cm := &corev1.ConfigMap{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      DSCConfigMapName,
					Namespace: reconciler.Namespace,
				}, cm)
			}, timeout, interval).Should(Succeed())

			// Default values should be false
			Expect(cm.Data[LMEvalPermitCodeExecutionKey]).To(Equal("false"))
			Expect(cm.Data[LMEvalPermitOnlineKey]).To(Equal("false"))
		})
	})

	Context("When updating ConfigMap", func() {
		It("Should update ConfigMap when module spec changes", func() {
			module := createModuleInstance("default", modulev1alpha1.ManagementStateManaged)
			module.Spec.Eval.LMEval.PermitCodeExecution = false
			module.Spec.Eval.LMEval.PermitOnline = false
			Expect(k8sClient.Create(ctx, module)).To(Succeed())

			// First reconcile - add finalizer
			_, err := performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile - create ConfigMap
			_, err = performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())

			// Verify initial state
			cm := &corev1.ConfigMap{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      DSCConfigMapName,
					Namespace: reconciler.Namespace,
				}, cm)
			}, timeout, interval).Should(Succeed())
			Expect(cm.Data[LMEvalPermitCodeExecutionKey]).To(Equal("false"))
			Expect(cm.Data[LMEvalPermitOnlineKey]).To(Equal("false"))

			// Update module spec
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "default"}, module)).To(Succeed())
			module.Spec.Eval.LMEval.PermitCodeExecution = true
			module.Spec.Eval.LMEval.PermitOnline = true
			Expect(k8sClient.Update(ctx, module)).To(Succeed())

			// Second reconcile - update ConfigMap
			_, err = performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())

			// Verify updated state
			Eventually(func() string {
				_ = k8sClient.Get(ctx, types.NamespacedName{
					Name:      DSCConfigMapName,
					Namespace: reconciler.Namespace,
				}, cm)
				return cm.Data[LMEvalPermitCodeExecutionKey]
			}, timeout, interval).Should(Equal("true"))
			Expect(cm.Data[LMEvalPermitOnlineKey]).To(Equal("true"))
		})

		It("Should not update ConfigMap when data hasn't changed", func() {
			module := createModuleInstance("default", modulev1alpha1.ManagementStateManaged)
			module.Spec.Eval.LMEval.PermitCodeExecution = true
			module.Spec.Eval.LMEval.PermitOnline = false
			Expect(k8sClient.Create(ctx, module)).To(Succeed())

			// First reconcile - add finalizer
			_, err := performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile - create ConfigMap
			_, err = performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())

			// Get ConfigMap resource version
			cm := &corev1.ConfigMap{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      DSCConfigMapName,
					Namespace: reconciler.Namespace,
				}, cm)
			}, timeout, interval).Should(Succeed())
			initialResourceVersion := cm.ResourceVersion

			// Third reconcile without changes
			_, err = performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())

			// Verify resource version hasn't changed (no update occurred)
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      DSCConfigMapName,
				Namespace: reconciler.Namespace,
			}, cm)).To(Succeed())
			Expect(cm.ResourceVersion).To(Equal(initialResourceVersion))
		})
	})

	Context("When deleting module", func() {
		It("Should delete ConfigMap during finalizer cleanup", func() {
			module := createModuleInstance("default", modulev1alpha1.ManagementStateManaged)
			module.Spec.Eval.LMEval.PermitCodeExecution = true
			Expect(k8sClient.Create(ctx, module)).To(Succeed())

			// First reconcile - add finalizer
			_, err := performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile - create ConfigMap
			_, err = performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())

			// Verify ConfigMap exists
			cm := &corev1.ConfigMap{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      DSCConfigMapName,
					Namespace: reconciler.Namespace,
				}, cm)
			}, timeout, interval).Should(Succeed())

			// Delete module
			Expect(k8sClient.Delete(ctx, module)).To(Succeed())

			// Trigger finalizer cleanup
			_, err = performReconcile(reconciler, "default")
			Expect(err).NotTo(HaveOccurred())

			// Verify ConfigMap is deleted
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      DSCConfigMapName,
					Namespace: reconciler.Namespace,
				}, cm)
				return err != nil
			}, timeout, interval).Should(BeTrue())
		})
	})
})

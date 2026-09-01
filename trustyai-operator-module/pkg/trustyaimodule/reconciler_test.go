package trustyaimodule

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	common "github.com/opendatahub-io/odh-platform-utilities/api/common"
	platformv1alpha1 "github.com/trustyai-explainability/trustyai-operator-module/pkg/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const trustyAIInstanceName = "default-trustyai"

var _ = Describe("TrustyAI Module Reconciler", func() {
	const testNamespace = "default"

	var (
		ctx                = context.Background()
		typeNamespacedName = types.NamespacedName{Name: trustyAIInstanceName}
	)

	newReconciler := func() *TrustyAIModuleReconciler {
		return &TrustyAIModuleReconciler{
			Client:               k8sClient,
			Scheme:               k8sClient.Scheme(),
			Namespace:            testNamespace,
			EventRecorder:        record.NewFakeRecorder(100),
			SkipDependencyChecks: true,
		}
	}

	AfterEach(func() {
		module := &platformv1alpha1.TrustyAI{}
		err := k8sClient.Get(ctx, typeNamespacedName, module)
		if err == nil {
			Expect(k8sClient.Delete(ctx, module)).To(Succeed())

			// Drive the finalizer cleanup path so the singleton name is free
			// for the next spec.
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, typeNamespacedName, module))
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())
		}
	})

	Context("singleton enforcement", func() {
		It("admits a TrustyAI resource named 'default-trustyai'", func() {
			module := &platformv1alpha1.TrustyAI{
				ObjectMeta: metav1.ObjectMeta{Name: trustyAIInstanceName},
			}
			Expect(k8sClient.Create(ctx, module)).To(Succeed())
		})

		It("rejects a TrustyAI resource not named 'default-trustyai'", func() {
			module := &platformv1alpha1.TrustyAI{
				ObjectMeta: metav1.ObjectMeta{Name: "not-default-trustyai"},
			}
			err := k8sClient.Create(ctx, module)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("must be named 'default-trustyai'"))
		})
	})

	Context("reconciliation", func() {
		BeforeEach(func() {
			module := &platformv1alpha1.TrustyAI{
				ObjectMeta: metav1.ObjectMeta{Name: trustyAIInstanceName},
			}
			Expect(k8sClient.Create(ctx, module)).To(Succeed())
		})

		It("adds a finalizer on first reconcile", func() {
			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			module := &platformv1alpha1.TrustyAI{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, module)).To(Succeed())
			Expect(module.Finalizers).To(ContainElement(FinalizerName))
		})

		It("updates observedGeneration to match metadata.generation after a successful reconcile", func() {
			r := newReconciler()
			// First reconcile only adds the finalizer and requeues.
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			module := &platformv1alpha1.TrustyAI{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, module)).To(Succeed())
			Expect(module.Status.ObservedGeneration).To(Equal(module.Generation))
		})

		It("removes the DSC ConfigMap and finalizer on deletion", func() {
			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: DSCConfigMapName, Namespace: testNamespace}, cm)).To(Succeed())

			module := &platformv1alpha1.TrustyAI{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, module)).To(Succeed())
			Expect(k8sClient.Delete(ctx, module)).To(Succeed())

			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, typeNamespacedName, module))
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

			Expect(errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: DSCConfigMapName, Namespace: testNamespace}, cm))).To(BeTrue())
		})

		It("sets Ready=False and Degraded=False when ManagementState is Removed", func() {
			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			module := &platformv1alpha1.TrustyAI{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, module)).To(Succeed())
			module.Spec.ManagementState = common.Removed
			Expect(k8sClient.Update(ctx, module)).To(Succeed())

			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, typeNamespacedName, module)).To(Succeed())
			Expect(module.Status.Phase).To(Equal(common.PhaseNotReady))
			Expect(module.Status.ObservedGeneration).To(Equal(module.Generation))

			readyCond := findCondition(module.Status.Conditions, string(common.ConditionTypeReady))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal("ModuleRemoved"))

			degradedCond := findCondition(module.Status.Conditions, string(common.ConditionTypeDegraded))
			Expect(degradedCond).NotTo(BeNil())
			Expect(degradedCond.Status).To(Equal(metav1.ConditionFalse))
		})
	})
})

func findCondition(conditions []common.Condition, condType string) *common.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}

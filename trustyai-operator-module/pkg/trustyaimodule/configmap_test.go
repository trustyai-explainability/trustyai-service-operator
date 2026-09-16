package trustyaimodule

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	platformv1alpha1 "github.com/trustyai-explainability/trustyai-operator-module/pkg/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("DSC ConfigMap reconciliation", func() {
	const testNamespace = "default"

	var (
		ctx                = context.Background()
		typeNamespacedName = types.NamespacedName{Name: trustyAIInstanceName}
	)

	newReconciler := func() *TrustyAIModuleReconciler {
		return &TrustyAIModuleReconciler{
			Client:                k8sClient,
			Scheme:                k8sClient.Scheme(),
			Namespace:             testNamespace,
			ManifestsTemplatePath: "../../config/manifests-template",
			EventRecorder:         record.NewFakeRecorder(100),
			SkipDependencyChecks:  true,
		}
	}

	BeforeEach(func() {
		module := &platformv1alpha1.TrustyAI{}
		err := k8sClient.Get(ctx, typeNamespacedName, module)
		if err == nil {
			Expect(k8sClient.Delete(ctx, module)).To(Succeed())
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, typeNamespacedName, module))
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())
		}

		module = &platformv1alpha1.TrustyAI{
			ObjectMeta: metav1.ObjectMeta{Name: trustyAIInstanceName},
		}
		Expect(k8sClient.Create(ctx, module)).To(Succeed())
	})

	AfterEach(func() {
		module := &platformv1alpha1.TrustyAI{}
		err := k8sClient.Get(ctx, typeNamespacedName, module)
		if err == nil {
			Expect(k8sClient.Delete(ctx, module)).To(Succeed())
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, typeNamespacedName, module))
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())
		}
	})

	It("creates the DSC ConfigMap from spec.eval settings", func() {
		module := &platformv1alpha1.TrustyAI{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, module)).To(Succeed())
		module.Spec.Eval.LMEval.PermitCodeExecution = true
		module.Spec.Eval.LMEval.PermitOnline = true
		Expect(k8sClient.Update(ctx, module)).To(Succeed())

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())

		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: DSCConfigMapName, Namespace: testNamespace}, cm)).To(Succeed())
		Expect(cm.Data[LMEvalPermitCodeExecutionKey]).To(Equal("true"))
		Expect(cm.Data[LMEvalPermitOnlineKey]).To(Equal("true"))
	})

	It("updates the DSC ConfigMap when spec.eval changes", func() {
		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())

		module := &platformv1alpha1.TrustyAI{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, module)).To(Succeed())
		module.Spec.Eval.LMEval.PermitCodeExecution = true
		Expect(k8sClient.Update(ctx, module)).To(Succeed())

		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())

		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: DSCConfigMapName, Namespace: testNamespace}, cm)).To(Succeed())
		Expect(cm.Data[LMEvalPermitCodeExecutionKey]).To(Equal("true"))
		Expect(cm.Data[LMEvalPermitOnlineKey]).To(Equal("false"))
	})
})

package guardrails

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	guardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/guardrails/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("Guardrails controller", func() {

	Context("Gurardrails controller test", func() {
		var (
			typeNamespaceName types.NamespacedName
			namespace         *corev1.Namespace
			orchestrator      *guardrailsv1alpha1.GuardrailsOrchestrator
		)

		specInit := func() {
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      orchestrator.Name,
					Namespace: orchestrator.Namespace,
				},
			}
			typeNamespaceName = types.NamespacedName{Name: orchestrator.Name, Namespace: orchestrator.Namespace}
			orchestrator = &guardrailsv1alpha1.GuardrailsOrchestrator{}

			By("Creating a namespace to run the tests")
			err := k8sClient.Create(ctx, namespace)
			Expect(err).ToNot(HaveOccurred())

			By("Creating a GuardrailsOrchestrator object")
			err = k8sClient.Get(ctx, typeNamespaceName, orchestrator)
			Expect(err != nil && errors.IsNotFound(err)).To(BeTrue())

			orchestrator = &guardrailsv1alpha1.GuardrailsOrchestrator{}

		}
		It("When using default parameters", func() {
			specInit()
			err := k8sClient.Create(ctx, orchestrator)
			Expect(err).ToNot(HaveOccurred())
			Eventually(validateGuardrails(ctx, typeNamespaceName, orchestrator),
				time.Minute, time.Second).Should(Succeed())
		})
		AfterEach(func() {
			By("Deleting the GuardrailsOrchestrator object")
			found := &guardrailsv1alpha1.GuardrailsOrchestrator{}
			err := k8sClient.Get(ctx, typeNamespaceName, found)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() error {
				return k8sClient.Delete(context.TODO(), found)
			}, time.Minute, time.Second).Should(Succeed())

			By("Deleting the namespace")
			_ = k8sClient.Delete(ctx, namespace)
		})
	})
})

func validateGuardrails(ctx context.Context, typeNamespaceName types.NamespacedName, orchestrator *guardrailsv1alpha1.GuardrailsOrchestrator) func() error {
	return func() error {
		By("Checking if the GuardrailsOrchestrator object has been created")
		Eventually(func() error {
			found := &guardrailsv1alpha1.GuardrailsOrchestrator{}
			return k8sClient.Get(ctx, typeNamespaceName, found)
		}, time.Minute, time.Second).Should(Succeed())

		scheme := k8sClient.Scheme()
		guaradrailsReconciler := &GuardrailsReconciler{
			Client:        k8sClient,
			Scheme:        scheme,
			Namespace:     orchestrator.Namespace,
			EventRecorder: &record.FakeRecorder{},
		}
		By("Reconciling the GuardrailsOrchestrator object created")
		Eventually(func() error {
			result, err := guaradrailsReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespaceName})
			if err != nil {
				return err
			}
			if !result.IsZero() {
				deployment := &appsv1.Deployment{}
				derr := k8sClient.Get(ctx, typeNamespaceName, deployment)
				if derr != nil {
					return derr
				}
			}
			// reconcile complete
			return nil
		}, time.Minute, time.Second).Should(Succeed())

		By("Checking if the deployment has been successfully created")
		Eventually(func() error {
			found := &appsv1.Deployment{}
			return k8sClient.Get(ctx, typeNamespaceName, found)
		}, time.Minute, time.Second).Should(Succeed())

		By("Checking if the service has been successfully created")
		Eventually(func() error {
			service := &corev1.Service{}
			err := k8sClient.Get(ctx, typeNamespaceName, service)
			Expect(err).ToNot(HaveOccurred())

			name := service.Name
			Expect(name).To(Equal(orchestrator.Name))

			description := service.Annotations["description"]
			Expect(description).To(Equal("Service for TrustyAI Guardrails Orchestrator"))
			return nil
		}, time.Minute, time.Second).Should(Succeed())

		return nil
	}
}

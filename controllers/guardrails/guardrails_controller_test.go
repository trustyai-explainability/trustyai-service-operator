package guardrails

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	guardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/guardrails/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	orchestratorName      = "test-orchestrator"
	orchestratorNamespace = "test-namespace"

	timeout  = time.Second * 10
	duration = time.Second * 10
	interval = time.Millisecond * 250
)

var _ = Describe("Guardrails Controller", func() {
	Context("When creating the default Deployment", func() {
		It("Should create a Deployment and Service", func() {
			// Create namespace in the cluster
			namespace := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: orchestratorNamespace,
				},
			}
			Expect(k8sClient.Create(ctx, &namespace)).To(Succeed())

			// Create the Orchestrator custom resource
			orchestrator := guardrailsv1alpha1.GuardrailsOrchestrator{
				ObjectMeta: metav1.ObjectMeta{
					Name:      orchestratorName,
					Namespace: orchestratorNamespace,
				},
				Spec: guardrailsv1alpha1.GuardrailsOrchestratorSpec{},
			}
			Expect(k8sClient.Create(ctx, &orchestrator)).To(Succeed())

			// Check if the Deployment has been created sucessfully
			deployment := &appsv1.Deployment{}
			typeNamespaceName := types.NamespacedName{Name: orchestrator.Name, Namespace: orchestrator.Namespace}
			Eventually(func() error {
				err := k8sClient.Get(ctx, typeNamespaceName, deployment)
				if err != nil {
					return err
				}
				return nil
			}, timeout, interval).Should(Succeed())
			Expect(*deployment.Spec.Replicas).Should(Equal(int32(1)))
			Expect(deployment.Namespace).Should(Equal(namespace))
			Expect(deployment.Name).Should(Equal(orchestrator.Name))

			// Check if the Service has been created sucessfully
			service := &corev1.Service{}
			Eventually(func() error {
				err := k8sClient.Get(ctx, typeNamespaceName, service)
				if err != nil {
					return err
				}
				return nil
			}, timeout, interval).Should(Succeed())
			Expect(service.Namespace).Should(Equal(namespace))
			Expect(service.Name).Should(Equal(orchestrator.Name))
		})
	})
})

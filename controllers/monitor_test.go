package controllers

import (
	"context"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
)

var _ = Describe("Service Monitor Reconciliation", func() {

	BeforeEach(func() {
		recorder = record.NewFakeRecorder(10)
		reconciler = &TrustyAIServiceReconciler{
			Client:        k8sClient,
			Scheme:        scheme.Scheme,
			EventRecorder: recorder,
			Namespace:     operatorNamespace,
		}
		ctx = context.Background()
	})

	Context("When creating a central ServiceMonitor", func() {

		It("Should have correct values", func() {

			err := reconciler.ensureCentralServiceMonitor(ctx)
			Expect(err).ToNot(HaveOccurred())

			// Define the ServiceMonitor object and fetch from the cluster
			serviceMonitor := &monitoringv1.ServiceMonitor{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: serviceMonitorName, Namespace: reconciler.Namespace}, serviceMonitor)
			Expect(err).NotTo(HaveOccurred())

			Expect(serviceMonitor.ObjectMeta.Name).To(Equal(serviceMonitorName))
			Expect(serviceMonitor.ObjectMeta.Namespace).To(Equal(reconciler.Namespace))
			Expect(serviceMonitor.Labels["modelmesh-service"]).To(Equal("modelmesh-serving"))

			Expect(serviceMonitor.Spec.Selector.MatchLabels["app.kubernetes.io/part-of"]).To(Equal(componentName))

			Expect(serviceMonitor.Spec.NamespaceSelector.Any).To(BeTrue())

			Expect(serviceMonitor.Spec.Endpoints).To(HaveLen(1))
			endpoint := serviceMonitor.Spec.Endpoints[0]
			Expect(endpoint.BearerTokenSecret.Key).To(Equal(""))
			Expect(endpoint.HonorLabels).To(BeTrue())

			Expect(endpoint.Path).To(Equal("/q/metrics"))
			Expect(endpoint.Scheme).To(Equal("http"))
			Expect(endpoint.Params["match[]"]).To(ConsistOf("{__name__= \"trustyai_spd\"}", "{__name__= \"trustyai_dir\"}"))

		})
	})

	Context("When creating a local ServiceMonitor", func() {
		var instance *trustyaiopendatahubiov1alpha1.TrustyAIService
		It("Should have correct values", func() {
			namespace := "sm-test-namespace-1"
			instance = createDefaultPVCCustomResource(namespace)

			WaitFor(func() error {
				return createNamespace(ctx, k8sClient, namespace)
			}, "failed to create namespace")

			WaitFor(func() error {
				return reconciler.ensureLocalServiceMonitor(instance, ctx)
			}, "failed to create local ServiceMonitor")

			// Define the ServiceMonitor object and fetch from the cluster
			serviceMonitor := &monitoringv1.ServiceMonitor{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, serviceMonitor)
			Expect(err).NotTo(HaveOccurred())

			Expect(serviceMonitor.ObjectMeta.Name).To(Equal(instance.Name))
			Expect(serviceMonitor.ObjectMeta.Namespace).To(Equal(instance.Namespace))
			Expect(serviceMonitor.Labels["modelmesh-service"]).To(Equal("modelmesh-serving"))

			Expect(serviceMonitor.Spec.Selector.MatchLabels["app.kubernetes.io/part-of"]).To(Equal(componentName))

			Expect(serviceMonitor.Spec.NamespaceSelector.MatchNames).To(ContainElement(namespace))

			Expect(serviceMonitor.Spec.Endpoints).To(HaveLen(1))
			endpoint := serviceMonitor.Spec.Endpoints[0]
			Expect(endpoint.BearerTokenSecret.Key).To(Equal(""))
			Expect(endpoint.HonorLabels).To(BeTrue())

			Expect(endpoint.Path).To(Equal("/q/metrics"))
			Expect(endpoint.Scheme).To(Equal("http"))
			Expect(endpoint.Params["match[]"]).To(ConsistOf("{__name__= \"trustyai_spd\"}", "{__name__= \"trustyai_dir\"}"))

		})
	})

})

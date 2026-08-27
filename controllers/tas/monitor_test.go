package tas

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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
			Expect(endpoint.Port).To(Equal("http"))
			Expect(endpoint.Scheme).To(Equal("http"))
			Expect(endpoint.Params["match[]"]).To(ConsistOf("{__name__= \"trustyai_spd\"}", "{__name__= \"trustyai_dir\"}"))

		})
	})

	Context("When creating a local ServiceMonitor", func() {
		var instance *trustyaiopendatahubiov1.TrustyAIService
		It("Should have correct values", func() {
			namespace := "sm-test-namespace-1"
			instance = createDefaultPVCCustomResource(namespace)

			WaitFor(func() error {
				return createNamespace(ctx, k8sClient, namespace)
			}, "failed to create namespace")

			WaitFor(func() error {
				return reconciler.ensureLocalServiceMonitor(instance, ctx)
			}, "failed to create local ServiceMonitor")

			serviceMonitor := &monitoringv1.ServiceMonitor{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, serviceMonitor)
			Expect(err).NotTo(HaveOccurred())

			Expect(serviceMonitor.ObjectMeta.Name).To(Equal(instance.Name))
			Expect(serviceMonitor.ObjectMeta.Namespace).To(Equal(instance.Namespace))
			Expect(serviceMonitor.Labels["modelmesh-service"]).To(Equal("modelmesh-serving"))

			Expect(serviceMonitor.Spec.Selector.MatchLabels["app.kubernetes.io/part-of"]).To(Equal(componentName))
			Expect(serviceMonitor.Spec.Selector.MatchLabels["trustyai-service-name"]).To(Equal(instance.Name))

			Expect(serviceMonitor.Spec.NamespaceSelector.MatchNames).To(ContainElement(namespace))

			Expect(serviceMonitor.Spec.Endpoints).To(HaveLen(1))
			endpoint := serviceMonitor.Spec.Endpoints[0]
			Expect(endpoint.BearerTokenSecret.Name).To(Equal(instance.Name + metricsReaderTokenSuffix))
			Expect(endpoint.BearerTokenSecret.Key).To(Equal("token"))
			Expect(endpoint.HonorLabels).To(BeTrue())

			Expect(endpoint.Path).To(Equal("/q/metrics"))
			Expect(endpoint.Port).To(Equal("https"))
			Expect(endpoint.Scheme).To(Equal("https"))
			Expect(endpoint.TLSConfig).ToNot(BeNil())
			Expect(endpoint.TLSConfig.InsecureSkipVerify).To(BeFalse())
			Expect(endpoint.TLSConfig.CA.ConfigMap).ToNot(BeNil())
			Expect(endpoint.TLSConfig.CA.ConfigMap.Name).To(Equal(instance.Name + metricsCABundleConfigMapSuffix))
			Expect(endpoint.TLSConfig.CA.ConfigMap.Key).To(Equal(metricsCABundleConfigMapKey))
			Expect(endpoint.TLSConfig.ServerName).To(Equal(instance.Name + "-tls." + namespace + ".svc"))
			Expect(endpoint.Params["match[]"]).To(ConsistOf("{__name__= \"trustyai_spd\"}", "{__name__= \"trustyai_dir\"}"))

		})
	})

	Context("When creating metrics reader ServiceAccount", func() {
		var instance *trustyaiopendatahubiov1.TrustyAIService
		It("Should create ServiceAccount and token Secret", func() {
			namespace := "sm-test-namespace-3"
			instance = createDefaultPVCCustomResource(namespace)

			WaitFor(func() error {
				return createNamespace(ctx, k8sClient, namespace)
			}, "failed to create namespace")

			WaitFor(func() error {
				return reconciler.ensureMetricsReaderServiceAccount(instance, ctx)
			}, "failed to create metrics reader SA")

			saName := instance.Name + metricsReaderSuffix
			sa := &corev1.ServiceAccount{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: saName, Namespace: namespace}, sa)
			Expect(err).NotTo(HaveOccurred())
			Expect(sa.Labels["app.kubernetes.io/part-of"]).To(Equal(componentName))

			secretName := instance.Name + metricsReaderTokenSuffix
			secret := &corev1.Secret{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, secret)
			Expect(err).NotTo(HaveOccurred())
			Expect(secret.Type).To(Equal(corev1.SecretTypeServiceAccountToken))
			Expect(secret.Annotations["kubernetes.io/service-account.name"]).To(Equal(saName))
		})
	})

	Context("When creating Prometheus RBAC", func() {
		var instance *trustyaiopendatahubiov1.TrustyAIService
		It("Should create Role and RoleBinding for metrics-reader SA", func() {
			namespace := "sm-test-namespace-2"
			instance = createDefaultPVCCustomResource(namespace)

			WaitFor(func() error {
				return createNamespace(ctx, k8sClient, namespace)
			}, "failed to create namespace")

			WaitFor(func() error {
				return reconciler.ensurePrometheusRBAC(instance, ctx)
			}, "failed to create Prometheus RBAC")

			roleName := instance.Name + metricsReaderSuffix

			role := &rbacv1.Role{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: roleName, Namespace: namespace}, role)
			Expect(err).NotTo(HaveOccurred())
			Expect(role.Rules).To(HaveLen(2))
			Expect(role.Rules[0].Resources).To(ContainElement("services"))
			Expect(role.Rules[0].ResourceNames).To(ContainElement(instance.Name))
			Expect(role.Rules[0].Verbs).To(ContainElement("get"))
			Expect(role.Rules[1].Resources).To(ContainElement("secrets"))
			Expect(role.Rules[1].ResourceNames).To(ContainElement(instance.Name + metricsReaderTokenSuffix))
			Expect(role.Rules[1].Verbs).To(ContainElement("get"))

			binding := &rbacv1.RoleBinding{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: roleName, Namespace: namespace}, binding)
			Expect(err).NotTo(HaveOccurred())
			Expect(binding.Subjects).To(HaveLen(2))
			Expect(binding.Subjects[0].Name).To(Equal(instance.Name + metricsReaderSuffix))
			Expect(binding.Subjects[0].Namespace).To(Equal(namespace))
			Expect(binding.Subjects[1].Name).To(Equal("prometheus-user-workload"))
			Expect(binding.Subjects[1].Namespace).To(Equal("openshift-user-workload-monitoring"))
			Expect(binding.RoleRef.Name).To(Equal(roleName))
		})
	})

})

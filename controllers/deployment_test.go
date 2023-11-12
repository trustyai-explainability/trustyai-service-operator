package controllers

import (
	"context"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"time"
	//+kubebuilder:scaffold:imports
)

var _ = Describe("TrustyAI operator", func() {

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

	Context("When deploying with default settings without an InferenceService", func() {
		var instance *trustyaiopendatahubiov1alpha1.TrustyAIService
		It("Creates a deployment and a service with the default configuration", func() {
			namespace := "trusty-ns-1"
			instance = createDefaultCR(namespace)
			Eventually(func() error {
				return createNamespace(ctx, k8sClient, namespace)
			}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to create namespace")

			Eventually(func() error {
				return createTestPVC(ctx, k8sClient, instance)
			}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to create PVC")

			Eventually(func() error {
				return reconciler.ensureDeployment(ctx, instance)
			}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to create deployment")

			deployment := &appsv1.Deployment{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, deployment)
			Expect(err).ToNot(HaveOccurred())
			Expect(deployment).ToNot(BeNil())

			Expect(*deployment.Spec.Replicas).Should(Equal(int32(1)))
			Expect(deployment.Namespace).Should(Equal(namespace))
			Expect(deployment.Name).Should(Equal(defaultServiceName))
			Expect(deployment.Labels["app"]).Should(Equal(defaultServiceName))
			Expect(deployment.Labels["app.kubernetes.io/name"]).Should(Equal(defaultServiceName))
			Expect(deployment.Labels["app.kubernetes.io/instance"]).Should(Equal(defaultServiceName))
			Expect(deployment.Labels["app.kubernetes.io/part-of"]).Should(Equal(componentName))
			Expect(deployment.Labels["app.kubernetes.io/version"]).Should(Equal("0.1.0"))

			Expect(deployment.Spec.Template.Spec.Containers[0].Image).Should(Equal("quay.io/trustyai/trustyai-service:latest"))

			Eventually(func() error {
				service, _ := reconciler.reconcileService(instance)
				return reconciler.Create(ctx, service)
			}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to create service")

			service := &corev1.Service{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: defaultServiceName, Namespace: namespace}, service)
			}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to get Service")

			Expect(service.Annotations["prometheus.io/path"]).Should(Equal("/q/metrics"))
			Expect(service.Annotations["prometheus.io/scheme"]).Should(Equal("http"))
			Expect(service.Annotations["prometheus.io/scrape"]).Should(Equal("true"))
			Expect(service.Namespace).Should(Equal(namespace))
		})

		Context("When deploying with an associated InferenceService", func() {
			It("Sets up the InferenceService and links it to the TrustyAIService deployment", func() {
				namespace := "trusty-ns-2"
				instance = createDefaultCR(namespace)

				Eventually(func() error {
					return createNamespace(ctx, k8sClient, namespace)
				}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to create namespace")
				Eventually(func() error {
					return createTestPVC(ctx, k8sClient, instance)
				}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to create PVC")
				Eventually(func() error {
					return reconciler.ensureDeployment(ctx, instance)
				}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to create deployment")

				// Creating the InferenceService
				inferenceService := createInferenceService("my-model", namespace)
				Eventually(func() error {
					return k8sClient.Create(ctx, inferenceService)
				}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to create deployment")

				Expect(reconciler.patchKServe(ctx, instance, *inferenceService, namespace, instance.Name, false)).ToNot(HaveOccurred())

				deployment := &appsv1.Deployment{}
				Eventually(func() error {
					// Define defaultServiceName for the deployment created by the operator
					namespacedNamed := types.NamespacedName{
						Namespace: namespace,
						Name:      instance.Name,
					}
					return k8sClient.Get(ctx, namespacedNamed, deployment)
				}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to get Deployment")

				Expect(*deployment.Spec.Replicas).Should(Equal(int32(1)))
				Expect(deployment.Namespace).Should(Equal(namespace))
				Expect(deployment.Name).Should(Equal(defaultServiceName))
				Expect(deployment.Labels["app"]).Should(Equal(defaultServiceName))
				Expect(deployment.Labels["app.kubernetes.io/name"]).Should(Equal(defaultServiceName))
				Expect(deployment.Labels["app.kubernetes.io/instance"]).Should(Equal(defaultServiceName))
				Expect(deployment.Labels["app.kubernetes.io/part-of"]).Should(Equal(componentName))
				Expect(deployment.Labels["app.kubernetes.io/version"]).Should(Equal("0.1.0"))

			})
		})
	})

	Context("Across multiple namespaces", func() {
		var instances []*trustyaiopendatahubiov1alpha1.TrustyAIService

		var namespaces = []string{"namespace1", "namespace2", "namespace3"}

		instances = make([]*trustyaiopendatahubiov1alpha1.TrustyAIService, len(namespaces))

		It("Deploys services with defaults in each specified namespace", func() {
			for i, namespace := range namespaces {
				instances[i] = createDefaultCR(namespace)
				instances[i].Namespace = namespace
				Eventually(func() error {
					return createNamespace(ctx, k8sClient, namespace)
				}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to create namespace")
			}

			for _, instance := range instances {
				Eventually(func() error {
					return createTestPVC(ctx, k8sClient, instance)
				}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to create PVC")
				Eventually(func() error {
					return reconciler.ensureDeployment(ctx, instance)
				}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to create deployment")
				//Expect(k8sClient.Create(ctx, instance)).Should(Succeed())
				deployment := &appsv1.Deployment{}
				Eventually(func() error {
					// Define defaultServiceName for the deployment created by the operator
					namespacedNamed := types.NamespacedName{
						Namespace: instance.Namespace,
						Name:      defaultServiceName,
					}
					return k8sClient.Get(ctx, namespacedNamed, deployment)
				}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to get Deployment")

				Expect(*deployment.Spec.Replicas).Should(Equal(int32(1)))
				Expect(deployment.Namespace).Should(Equal(instance.Namespace))
				Expect(deployment.Name).Should(Equal(defaultServiceName))
				Expect(deployment.Labels["app"]).Should(Equal(defaultServiceName))
				Expect(deployment.Labels["app.kubernetes.io/name"]).Should(Equal(defaultServiceName))
				Expect(deployment.Labels["app.kubernetes.io/instance"]).Should(Equal(defaultServiceName))
				Expect(deployment.Labels["app.kubernetes.io/part-of"]).Should(Equal(componentName))
				Expect(deployment.Labels["app.kubernetes.io/version"]).Should(Equal("0.1.0"))

				Expect(deployment.Spec.Template.Spec.Containers[0].Image).Should(Equal("quay.io/trustyai/trustyai-service:latest"))

			}
		})
	})

})

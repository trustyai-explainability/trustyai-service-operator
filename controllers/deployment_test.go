package controllers

import (
	"context"
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	//+kubebuilder:scaffold:imports
)

func printKubeObject(obj interface{}) {
	bytes, err := json.MarshalIndent(obj, "", "    ")
	if err != nil {
		fmt.Println("Error printing object:", err)
	} else {
		fmt.Println(string(bytes))
	}
}

var _ = Describe("TrustyAI operator", func() {

	BeforeEach(func() {
		recorder = record.NewFakeRecorder(10)
		k8sClient = fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
		reconciler = &TrustyAIServiceReconciler{
			Client:        k8sClient,
			Scheme:        scheme.Scheme,
			EventRecorder: recorder,
			Namespace:     operatorNamespace,
		}
		ctx = context.Background()
	})

	AfterEach(func() {
		// Attempt to delete the ConfigMap
		configMap := &corev1.ConfigMap{}
		err := k8sClient.Get(ctx, types.NamespacedName{
			Namespace: operatorNamespace,
			Name:      imageConfigMap,
		}, configMap)

		// If the ConfigMap exists, delete it
		if err == nil {
			Expect(k8sClient.Delete(ctx, configMap)).To(Succeed())
		} else if !apierrors.IsNotFound(err) {
			Fail(fmt.Sprintf("Unexpected error while getting ConfigMap: %s", err))
		}
	})

	Context("When deploying with default settings without an InferenceService", func() {
		var instance *trustyaiopendatahubiov1alpha1.TrustyAIService

		It("Creates a deployment and a service with the default configuration", func() {

			namespace := "trusty-ns-a-1"
			Expect(createNamespace(ctx, k8sClient, namespace)).To(Succeed())
			instance = createDefaultCR(namespace)

			Expect(createTestPVC(ctx, k8sClient, instance)).To(Succeed())
			Expect(reconciler.createServiceAccount(ctx, instance)).To(Succeed())
			Expect(reconciler.ensureDeployment(ctx, instance)).To(Succeed())

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

			Expect(len(deployment.Spec.Template.Spec.Containers)).Should(Equal(2))
			Expect(deployment.Spec.Template.Spec.Containers[0].Image).Should(Equal("quay.io/trustyai/trustyai-service:latest"))
			Expect(deployment.Spec.Template.Spec.Containers[1].Image).Should(Equal("registry.redhat.io/openshift4/ose-oauth-proxy:latest"))

			WaitFor(func() error {
				service, _ := reconciler.reconcileService(instance)
				return reconciler.Create(ctx, service)
			}, "failed to create service")

			service := &corev1.Service{}
			WaitFor(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: defaultServiceName, Namespace: namespace}, service)
			}, "failed to get Service")

			Expect(service.Annotations["prometheus.io/path"]).Should(Equal("/q/metrics"))
			Expect(service.Annotations["prometheus.io/scheme"]).Should(Equal("http"))
			Expect(service.Annotations["prometheus.io/scrape"]).Should(Equal("true"))
			Expect(service.Namespace).Should(Equal(namespace))

			WaitFor(func() error {
				err := reconciler.reconcileOAuthService(ctx, instance)
				return err
			}, "failed to create oauth service")

			desiredOAuthService, err := generateTrustyAIOAuthService(ctx, instance)
			Expect(err).ToNot(HaveOccurred())

			oauthService := &corev1.Service{}
			WaitFor(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: desiredOAuthService.Name, Namespace: namespace}, oauthService)
			}, "failed to get OAuth Service")

			// Check if the OAuth service has the expected labels
			Expect(oauthService.Labels["app"]).Should(Equal(instance.Name))
			Expect(oauthService.Labels["app.kubernetes.io/instance"]).Should(Equal(instance.Name))
			Expect(oauthService.Labels["app.kubernetes.io/name"]).Should(Equal(instance.Name))
			Expect(oauthService.Labels["app.kubernetes.io/part-of"]).Should(Equal("trustyai"))
			Expect(oauthService.Labels["app.kubernetes.io/version"]).Should(Equal("0.1.0"))
			Expect(oauthService.Labels["trustyai-service-name"]).Should(Equal(instance.Name))

		})
	})

	Context("When deploying with a ConfigMap and without an InferenceService", func() {
		var instance *trustyaiopendatahubiov1alpha1.TrustyAIService

		It("Creates a deployment and a service with the ConfigMap configuration", func() {

			namespace := "trusty-ns-a-1-cm"
			serviceImage := "custom-service-image:foo"
			oauthImage := "custom-oauth-proxy:bar"
			Expect(createNamespace(ctx, k8sClient, namespace)).To(Succeed())

			WaitFor(func() error {
				configMap := createConfigMap(operatorNamespace, oauthImage, serviceImage)
				return k8sClient.Create(ctx, configMap)
			}, "failed to create ConfigMap")

			instance = createDefaultCR(namespace)

			Expect(createTestPVC(ctx, k8sClient, instance)).To(Succeed())
			Expect(reconciler.createServiceAccount(ctx, instance)).To(Succeed())
			WaitFor(func() error {
				return reconciler.ensureDeployment(ctx, instance)
			}, "failed to reconcile deployment")

			deployment := &appsv1.Deployment{}
			WaitFor(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: namespace}, deployment)
			}, "failed to get updated deployment")
			Expect(deployment).ToNot(BeNil())

			Expect(*deployment.Spec.Replicas).Should(Equal(int32(1)))
			Expect(deployment.Namespace).Should(Equal(namespace))
			Expect(deployment.Name).Should(Equal(defaultServiceName))
			Expect(deployment.Labels["app"]).Should(Equal(defaultServiceName))
			Expect(deployment.Labels["app.kubernetes.io/name"]).Should(Equal(defaultServiceName))
			Expect(deployment.Labels["app.kubernetes.io/instance"]).Should(Equal(defaultServiceName))
			Expect(deployment.Labels["app.kubernetes.io/part-of"]).Should(Equal(componentName))
			Expect(deployment.Labels["app.kubernetes.io/version"]).Should(Equal("0.1.0"))

			Expect(len(deployment.Spec.Template.Spec.Containers)).Should(Equal(2))
			Expect(deployment.Spec.Template.Spec.Containers[0].Image).Should(Equal(serviceImage))
			Expect(deployment.Spec.Template.Spec.Containers[1].Image).Should(Equal(oauthImage))

			WaitFor(func() error {
				service, _ := reconciler.reconcileService(instance)
				return reconciler.Create(ctx, service)
			}, "failed to create service")

			service := &corev1.Service{}
			WaitFor(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: defaultServiceName, Namespace: namespace}, service)
			}, "failed to get Service")

			Expect(service.Annotations["prometheus.io/path"]).Should(Equal("/q/metrics"))
			Expect(service.Annotations["prometheus.io/scheme"]).Should(Equal("http"))
			Expect(service.Annotations["prometheus.io/scrape"]).Should(Equal("true"))
			Expect(service.Namespace).Should(Equal(namespace))

			WaitFor(func() error {
				err := reconciler.reconcileOAuthService(ctx, instance)
				return err
			}, "failed to create oauth service")

			desiredOAuthService, err := generateTrustyAIOAuthService(ctx, instance)
			Expect(err).ToNot(HaveOccurred())

			oauthService := &corev1.Service{}
			WaitFor(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: desiredOAuthService.Name, Namespace: namespace}, oauthService)
			}, "failed to get OAuth Service")

			// Check if the OAuth service has the expected labels
			Expect(oauthService.Labels["app"]).Should(Equal(instance.Name))
			Expect(oauthService.Labels["app.kubernetes.io/instance"]).Should(Equal(instance.Name))
			Expect(oauthService.Labels["app.kubernetes.io/name"]).Should(Equal(instance.Name))
			Expect(oauthService.Labels["app.kubernetes.io/part-of"]).Should(Equal("trustyai"))
			Expect(oauthService.Labels["app.kubernetes.io/version"]).Should(Equal("0.1.0"))
			Expect(oauthService.Labels["trustyai-service-name"]).Should(Equal(instance.Name))

		})
	})

	Context("When deploying with default settings without an InferenceService", func() {
		var instance *trustyaiopendatahubiov1alpha1.TrustyAIService

		It("should set environment variables correctly", func() {

			namespace := "trusty-ns-a-4"
			instance = createDefaultCR(namespace)
			Expect(createNamespace(ctx, k8sClient, namespace)).To(Succeed())
			Expect(createTestPVC(ctx, k8sClient, instance)).To(Succeed())
			Expect(reconciler.createServiceAccount(ctx, instance)).To(Succeed())
			Expect(reconciler.ensureDeployment(ctx, instance)).To(Succeed())

			deployment := &appsv1.Deployment{}
			namespacedName := types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}
			Expect(k8sClient.Get(ctx, namespacedName, deployment)).Should(Succeed())

			foundEnvVar := func(envVars []corev1.EnvVar, name string) *corev1.EnvVar {
				for _, env := range envVars {
					if env.Name == name {
						return &env
					}
				}
				return nil
			}

			var trustyaiServiceContainer *corev1.Container
			for _, container := range deployment.Spec.Template.Spec.Containers {
				if container.Name == "trustyai-service" {
					trustyaiServiceContainer = &container
					break
				}
			}

			Expect(trustyaiServiceContainer).NotTo(BeNil(), "trustyai-service container not found")

			// Checking the environment variables of the trustyai-service container
			var envVar *corev1.EnvVar

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "SERVICE_BATCH_SIZE")
			Expect(envVar).NotTo(BeNil(), "Env var SERVICE_BATCH_SIZE not found")
			Expect(envVar.Value).To(Equal("5000"))

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "STORAGE_DATA_FILENAME")
			Expect(envVar).NotTo(BeNil(), "Env var STORAGE_DATA_FILENAME not found")
			Expect(envVar.Value).To(Equal("data.csv"))

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "SERVICE_STORAGE_FORMAT")
			Expect(envVar).NotTo(BeNil(), "Env var SERVICE_STORAGE_FORMAT not found")
			Expect(envVar.Value).To(Equal("PVC"))

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "STORAGE_DATA_FOLDER")
			Expect(envVar).NotTo(BeNil(), "Env var STORAGE_DATA_FOLDER not found")
			Expect(envVar.Value).To(Equal("/data"))

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "SERVICE_DATA_FORMAT")
			Expect(envVar).NotTo(BeNil(), "Env var SERVICE_DATA_FORMAT not found")
			Expect(envVar.Value).To(Equal("CSV"))

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "SERVICE_METRICS_SCHEDULE")
			Expect(envVar).NotTo(BeNil(), "Env var SERVICE_METRICS_SCHEDULE not found")
			Expect(envVar.Value).To(Equal("5s"))
		})
	})

	Context("When deploying with default settings without an InferenceService", func() {
		var instance *trustyaiopendatahubiov1alpha1.TrustyAIService

		It("should use the correct service account", func() {

			namespace := "trusty-ns-a-6"
			instance = createDefaultCR(namespace)
			Expect(createNamespace(ctx, k8sClient, namespace)).To(Succeed())
			Expect(createTestPVC(ctx, k8sClient, instance)).To(Succeed())
			Expect(reconciler.createServiceAccount(ctx, instance)).To(Succeed())
			WaitFor(func() error {
				return reconciler.ensureDeployment(ctx, instance)
			}, "failed to create deployment")

			deployment := &appsv1.Deployment{}
			namespacedName := types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}
			Expect(k8sClient.Get(ctx, namespacedName, deployment)).Should(Succeed())

			Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(Equal(instance.Name + "-proxy"))
		})
	})

	Context("When deploying with no custom CA bundle ConfigMap", func() {
		var instance *trustyaiopendatahubiov1alpha1.TrustyAIService

		It("should use the correct service account and not include CustomCertificatesBundle", func() {

			namespace := "trusty-ns-a-7"
			instance = createDefaultCR(namespace)
			Expect(createNamespace(ctx, k8sClient, namespace)).To(Succeed())
			Expect(createTestPVC(ctx, k8sClient, instance)).To(Succeed())
			Expect(reconciler.createServiceAccount(ctx, instance)).To(Succeed())
			WaitFor(func() error {
				return reconciler.ensureDeployment(ctx, instance)
			}, "failed to create deployment")

			deployment := &appsv1.Deployment{}
			namespacedName := types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}
			Expect(k8sClient.Get(ctx, namespacedName, deployment)).Should(Succeed())

			Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(Equal(instance.Name + "-proxy"))

			customCertificatesBundleVolumeName := caBundleName
			for _, volume := range deployment.Spec.Template.Spec.Volumes {
				Expect(volume.Name).ToNot(Equal(customCertificatesBundleVolumeName))
			}

			for _, container := range deployment.Spec.Template.Spec.Containers {
				for _, volumeMount := range container.VolumeMounts {
					Expect(volumeMount.Name).ToNot(Equal(customCertificatesBundleVolumeName))
				}
				for _, arg := range container.Args {
					Expect(arg).ToNot(ContainSubstring("--openshift-ca"))
				}
			}
		})
	})

	Context("When deploying with a custom CA bundle ConfigMap", func() {
		var instance *trustyaiopendatahubiov1alpha1.TrustyAIService

		It("should use the correct service account and include CustomCertificatesBundle", func() {

			namespace := "trusty-ns-a-8"
			instance = createDefaultCR(namespace)
			Expect(createNamespace(ctx, k8sClient, namespace)).To(Succeed())
			caBundleConfigMap := createTrustedCABundleConfigMap(instance.Namespace)
			Expect(k8sClient.Create(ctx, caBundleConfigMap)).To(Succeed())
			Expect(createTestPVC(ctx, k8sClient, instance)).To(Succeed())
			Expect(reconciler.createServiceAccount(ctx, instance)).To(Succeed())
			WaitFor(func() error {
				return reconciler.ensureDeployment(ctx, instance)
			}, "failed to create deployment")

			deployment := &appsv1.Deployment{}
			namespacedName := types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}
			Expect(k8sClient.Get(ctx, namespacedName, deployment)).Should(Succeed())

			Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(Equal(instance.Name + "-proxy"))

			foundTrustedCAVolume := false
			for _, volume := range deployment.Spec.Template.Spec.Volumes {
				if volume.Name == caBundleName && volume.ConfigMap != nil && volume.ConfigMap.Name == caBundleName {
					foundTrustedCAVolume = true
					Expect(volume.ConfigMap.Items).To(ContainElement(corev1.KeyToPath{
						Key:  "ca-bundle.crt",
						Path: "tls-ca-bundle.pem",
					}))
				}
			}
			Expect(foundTrustedCAVolume).To(BeTrue(), caBundleName+" volume not found")

			foundTrustedCAVolumeMount := false
			for _, container := range deployment.Spec.Template.Spec.Containers {
				for _, volumeMount := range container.VolumeMounts {
					if volumeMount.Name == caBundleName && volumeMount.MountPath == "/etc/pki/ca-trust/extracted/pem" {
						foundTrustedCAVolumeMount = true
					}

					if container.Name == "oauth-proxy" {
						foundOpenshiftCAArg := false
						for _, arg := range container.Args {
							if arg == "--openshift-ca=/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem" {
								foundOpenshiftCAArg = true
							}
						}
						Expect(foundOpenshiftCAArg).To(BeTrue(), "oauth-proxy container missing --openshift-ca argument")
					}
				}
			}
			Expect(foundTrustedCAVolumeMount).To(BeTrue(), caBundleName+"trusted-ca volume mount not found in any container")
			Expect(k8sClient.Delete(ctx, caBundleConfigMap)).To(Succeed(), "failed to delete custom CA bundle ConfigMap")

		})
	})

})

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

	Context("When deploying with an associated InferenceService", func() {

		It("Sets up the InferenceService and links it to the TrustyAIService deployment", func() {

			namespace := "trusty-ns-2"
			instance := createDefaultCR(namespace)
			WaitFor(func() error {
				return createNamespace(ctx, k8sClient, namespace)
			}, "failed to create namespace")
			WaitFor(func() error {
				return createTestPVC(ctx, k8sClient, instance)
			}, "failed to create PVC")
			WaitFor(func() error {
				return reconciler.ensureDeployment(ctx, instance)
			}, "failed to create deployment")

			// Creating the InferenceService
			inferenceService := createInferenceService("my-model", namespace)
			WaitFor(func() error {
				return k8sClient.Create(ctx, inferenceService)
			}, "failed to create deployment")

			Expect(reconciler.patchKServe(ctx, instance, *inferenceService, namespace, instance.Name, false)).ToNot(HaveOccurred())

			deployment := &appsv1.Deployment{}
			WaitFor(func() error {
				// Define defaultServiceName for the deployment created by the operator
				namespacedNamed := types.NamespacedName{
					Namespace: namespace,
					Name:      instance.Name,
				}
				return k8sClient.Get(ctx, namespacedNamed, deployment)
			}, "failed to get Deployment")

			Expect(*deployment.Spec.Replicas).Should(Equal(int32(1)))
			Expect(deployment.Namespace).Should(Equal(namespace))
			Expect(deployment.Name).Should(Equal(defaultServiceName))
			Expect(deployment.Labels["app"]).Should(Equal(defaultServiceName))
			Expect(deployment.Labels["app.kubernetes.io/name"]).Should(Equal(defaultServiceName))
			Expect(deployment.Labels["app.kubernetes.io/instance"]).Should(Equal(defaultServiceName))
			Expect(deployment.Labels["app.kubernetes.io/part-of"]).Should(Equal(componentName))
			Expect(deployment.Labels["app.kubernetes.io/version"]).Should(Equal("0.1.0"))

			WaitFor(func() error {
				err := reconciler.reconcileOAuthService(ctx, instance)
				return err
			}, "failed to create oauth service")

			desiredOAuthService, err := generateTrustyAIOAuthService(ctx, instance)
			Expect(err).ToNot(HaveOccurred())

			oauthService := &corev1.Service{}
			WaitFor(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: desiredOAuthService.Name, Namespace: namespace}, oauthService)
			}, "failed to get OAuth Service")

			// Check if the OAuth service has the expected labels
			Expect(oauthService.Labels["app"]).Should(Equal(instance.Name))
			Expect(oauthService.Labels["app.kubernetes.io/instance"]).Should(Equal(instance.Name))
			Expect(oauthService.Labels["app.kubernetes.io/name"]).Should(Equal(instance.Name))
			Expect(oauthService.Labels["app.kubernetes.io/part-of"]).Should(Equal("trustyai"))
			Expect(oauthService.Labels["app.kubernetes.io/version"]).Should(Equal("0.1.0"))
			Expect(oauthService.Labels["trustyai-service-name"]).Should(Equal(instance.Name))

		})
	})
})

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

	Context("Across multiple namespaces", func() {
		var instances []*trustyaiopendatahubiov1alpha1.TrustyAIService

		var namespaces = []string{"namespace1", "namespace2", "namespace3"}

		instances = make([]*trustyaiopendatahubiov1alpha1.TrustyAIService, len(namespaces))

		It("Deploys services with defaults in each specified namespace", func() {
			for i, namespace := range namespaces {
				instances[i] = createDefaultCR(namespace)
				instances[i].Namespace = namespace
				WaitFor(func() error {
					return createNamespace(ctx, k8sClient, namespace)
				}, "failed to create namespace")
			}

			for _, instance := range instances {
				WaitFor(func() error {
					return createTestPVC(ctx, k8sClient, instance)
				}, "failed to create PVC")
				WaitFor(func() error {
					return reconciler.ensureDeployment(ctx, instance)
				}, "failed to create deployment")
				//Expect(k8sClient.Create(ctx, instance)).Should(Succeed())
				deployment := &appsv1.Deployment{}
				WaitFor(func() error {
					// Define defaultServiceName for the deployment created by the operator
					namespacedNamed := types.NamespacedName{
						Namespace: instance.Namespace,
						Name:      defaultServiceName,
					}
					return k8sClient.Get(ctx, namespacedNamed, deployment)
				}, "failed to get Deployment")

				Expect(*deployment.Spec.Replicas).Should(Equal(int32(1)))
				Expect(deployment.Namespace).Should(Equal(instance.Namespace))
				Expect(deployment.Name).Should(Equal(defaultServiceName))
				Expect(deployment.Labels["app"]).Should(Equal(defaultServiceName))
				Expect(deployment.Labels["app.kubernetes.io/name"]).Should(Equal(defaultServiceName))
				Expect(deployment.Labels["app.kubernetes.io/instance"]).Should(Equal(defaultServiceName))
				Expect(deployment.Labels["app.kubernetes.io/part-of"]).Should(Equal(componentName))
				Expect(deployment.Labels["app.kubernetes.io/version"]).Should(Equal("0.1.0"))

				Expect(len(deployment.Spec.Template.Spec.Containers)).Should(Equal(2))
				Expect(deployment.Spec.Template.Spec.Containers[0].Image).Should(Equal("quay.io/trustyai/trustyai-service:latest"))
				Expect(deployment.Spec.Template.Spec.Containers[1].Image).Should(Equal("registry.redhat.io/openshift4/ose-oauth-proxy:latest"))

				WaitFor(func() error {
					err := reconciler.reconcileOAuthService(ctx, instance)
					return err
				}, "failed to create oauth service")

				desiredOAuthService, err := generateTrustyAIOAuthService(ctx, instance)
				Expect(err).ToNot(HaveOccurred())

				oauthService := &corev1.Service{}
				WaitFor(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: desiredOAuthService.Name, Namespace: instance.Namespace}, oauthService)
				}, "failed to get OAuth Service")

				// Check if the OAuth service has the expected labels
				Expect(oauthService.Labels["app"]).Should(Equal(instance.Name))
				Expect(oauthService.Labels["app.kubernetes.io/instance"]).Should(Equal(instance.Name))
				Expect(oauthService.Labels["app.kubernetes.io/name"]).Should(Equal(instance.Name))
				Expect(oauthService.Labels["app.kubernetes.io/part-of"]).Should(Equal("trustyai"))
				Expect(oauthService.Labels["app.kubernetes.io/version"]).Should(Equal("0.1.0"))
				Expect(oauthService.Labels["trustyai-service-name"]).Should(Equal(instance.Name))

			}
		})
	})

})

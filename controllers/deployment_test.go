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

func setupAndTestDeploymentDefault(instance *trustyaiopendatahubiov1alpha1.TrustyAIService, namespace string) {
	Expect(createNamespace(ctx, k8sClient, namespace)).To(Succeed())
	caBundle := reconciler.GetCustomCertificatesBundle(ctx, instance)

	Expect(createTestPVC(ctx, k8sClient, instance)).To(Succeed())
	Expect(reconciler.createServiceAccount(ctx, instance)).To(Succeed())
	Expect(reconciler.ensureDeployment(ctx, instance, caBundle, false)).To(Succeed())

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
	Expect(deployment.Labels["app.kubernetes.io/version"]).Should(Equal(Version))

	Expect(len(deployment.Spec.Template.Spec.Containers)).Should(Equal(2))
	Expect(deployment.Spec.Template.Spec.Containers[0].Image).Should(Equal("quay.io/trustyai/trustyai-service:latest"))
	Expect(deployment.Spec.Template.Spec.Containers[1].Image).Should(Equal("registry.redhat.io/openshift4/ose-oauth-proxy:latest"))

	WaitFor(func() error {
		service, _ := reconciler.reconcileService(ctx, instance)
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
		err := reconciler.reconcileOAuthService(ctx, instance, caBundle)
		return err
	}, "failed to create oauth service")

	desiredOAuthService, err := generateTrustyAIOAuthService(ctx, instance, caBundle)
	Expect(err).ToNot(HaveOccurred())

	oauthService := &corev1.Service{}
	WaitFor(func() error {
		return k8sClient.Get(ctx, types.NamespacedName{Name: desiredOAuthService.Name, Namespace: namespace}, oauthService)
	}, "failed to get OAuth Service")

	// Check if the OAuth service has the expected labels
	Expect(oauthService.Labels["app"]).Should(Equal(instance.Name))
	Expect(oauthService.Labels["app.kubernetes.io/instance"]).Should(Equal(instance.Name))
	Expect(oauthService.Labels["app.kubernetes.io/name"]).Should(Equal(instance.Name))
	Expect(oauthService.Labels["app.kubernetes.io/part-of"]).Should(Equal(componentName))
	Expect(oauthService.Labels["app.kubernetes.io/version"]).Should(Equal(Version))
	Expect(oauthService.Labels["trustyai-service-name"]).Should(Equal(instance.Name))

}

func setupAndTestDeploymentConfigMap(instance *trustyaiopendatahubiov1alpha1.TrustyAIService, namespace string) {
	serviceImage := "custom-service-image:foo"
	oauthImage := "custom-oauth-proxy:bar"
	Expect(createNamespace(ctx, k8sClient, namespace)).To(Succeed())

	WaitFor(func() error {
		configMap := createConfigMap(operatorNamespace, oauthImage, serviceImage)
		return k8sClient.Create(ctx, configMap)
	}, "failed to create ConfigMap")

	caBundle := reconciler.GetCustomCertificatesBundle(ctx, instance)

	Expect(createTestPVC(ctx, k8sClient, instance)).To(Succeed())
	Expect(reconciler.createServiceAccount(ctx, instance)).To(Succeed())
	WaitFor(func() error {
		return reconciler.ensureDeployment(ctx, instance, caBundle, false)
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
	Expect(deployment.Labels["app.kubernetes.io/version"]).Should(Equal(Version))

	Expect(len(deployment.Spec.Template.Spec.Containers)).Should(Equal(2))
	Expect(deployment.Spec.Template.Spec.Containers[0].Image).Should(Equal(serviceImage))
	Expect(deployment.Spec.Template.Spec.Containers[1].Image).Should(Equal(oauthImage))

	WaitFor(func() error {
		service, _ := reconciler.reconcileService(ctx, instance)
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
		err := reconciler.reconcileOAuthService(ctx, instance, caBundle)
		return err
	}, "failed to create oauth service")

	desiredOAuthService, err := generateTrustyAIOAuthService(ctx, instance, caBundle)
	Expect(err).ToNot(HaveOccurred())

	oauthService := &corev1.Service{}
	WaitFor(func() error {
		return k8sClient.Get(ctx, types.NamespacedName{Name: desiredOAuthService.Name, Namespace: namespace}, oauthService)
	}, "failed to get OAuth Service")

	// Check if the OAuth service has the expected labels
	Expect(oauthService.Labels["app"]).Should(Equal(instance.Name))
	Expect(oauthService.Labels["app.kubernetes.io/instance"]).Should(Equal(instance.Name))
	Expect(oauthService.Labels["app.kubernetes.io/name"]).Should(Equal(instance.Name))
	Expect(oauthService.Labels["app.kubernetes.io/part-of"]).Should(Equal(componentName))
	Expect(oauthService.Labels["app.kubernetes.io/version"]).Should(Equal(Version))
	Expect(oauthService.Labels["trustyai-service-name"]).Should(Equal(instance.Name))

}

func setupAndTestDeploymentNoCustomCABundle(instance *trustyaiopendatahubiov1alpha1.TrustyAIService, namespace string) {
	Expect(createNamespace(ctx, k8sClient, namespace)).To(Succeed())

	caBundle := reconciler.GetCustomCertificatesBundle(ctx, instance)

	Expect(createTestPVC(ctx, k8sClient, instance)).To(Succeed())
	Expect(reconciler.createServiceAccount(ctx, instance)).To(Succeed())
	WaitFor(func() error {
		return reconciler.ensureDeployment(ctx, instance, caBundle, false)
	}, "failed to create deployment")

	deployment := &appsv1.Deployment{}
	namespacedName := types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}
	Expect(k8sClient.Get(ctx, namespacedName, deployment)).Should(Succeed())

	Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(Equal(instance.Name + "-proxy"))

	customCertificatesBundleVolumeName := caBundle
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

}

func setupAndTestDeploymentCustomCABundle(instance *trustyaiopendatahubiov1alpha1.TrustyAIService, namespace string) {
	caBundleConfigMap := createTrustedCABundleConfigMap(namespace)
	Expect(createNamespace(ctx, k8sClient, namespace)).To(Succeed())
	Expect(k8sClient.Create(ctx, caBundleConfigMap)).To(Succeed())

	caBundle := reconciler.GetCustomCertificatesBundle(ctx, instance)

	Expect(createTestPVC(ctx, k8sClient, instance)).To(Succeed())
	Expect(reconciler.createServiceAccount(ctx, instance)).To(Succeed())
	WaitFor(func() error {
		return reconciler.ensureDeployment(ctx, instance, caBundle, false)
	}, "failed to create deployment")

	deployment := &appsv1.Deployment{}
	namespacedName := types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}
	Expect(k8sClient.Get(ctx, namespacedName, deployment)).Should(Succeed())

	Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(Equal(instance.Name + "-proxy"))

	foundCustomCertificatesBundleVolumeMount := false

	customCertificatesBundleMountPath := "/etc/ssl/certs/ca-bundle.crt"
	for _, container := range deployment.Spec.Template.Spec.Containers {
		for _, volumeMount := range container.VolumeMounts {
			if volumeMount.Name == caBundleName && volumeMount.MountPath == customCertificatesBundleMountPath {
				foundCustomCertificatesBundleVolumeMount = true
			}
		}
	}
	Expect(foundCustomCertificatesBundleVolumeMount).To(BeTrue(), caBundleName+" volume mount not found in any container")

	Expect(k8sClient.Delete(ctx, caBundleConfigMap)).To(Succeed(), "failed to delete custom certificates bundle ConfigMap")

}

func setupAndTestDeploymentServiceAccount(instance *trustyaiopendatahubiov1alpha1.TrustyAIService, namespace string, mode string) {
	Expect(createNamespace(ctx, k8sClient, namespace)).To(Succeed())

	caBundle := reconciler.GetCustomCertificatesBundle(ctx, instance)

	if mode == "PVC" {
		Expect(createTestPVC(ctx, k8sClient, instance)).To(Succeed())
	}
	Expect(reconciler.createServiceAccount(ctx, instance)).To(Succeed())
	WaitFor(func() error {
		return reconciler.ensureDeployment(ctx, instance, caBundle, false)
	}, "failed to create deployment")

	deployment := &appsv1.Deployment{}
	namespacedName := types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}
	Expect(k8sClient.Get(ctx, namespacedName, deployment)).Should(Succeed())

	Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(Equal(instance.Name + "-proxy"))
}

func setupAndTestDeploymentInferenceService(instance *trustyaiopendatahubiov1alpha1.TrustyAIService, namespace string, mode string) {
	WaitFor(func() error {
		return createNamespace(ctx, k8sClient, namespace)
	}, "failed to create namespace")

	caBundle := reconciler.GetCustomCertificatesBundle(ctx, instance)

	WaitFor(func() error {
		return createTestPVC(ctx, k8sClient, instance)
	}, "failed to create PVC")
	WaitFor(func() error {
		return reconciler.ensureDeployment(ctx, instance, caBundle, false)
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
	Expect(deployment.Labels["app.kubernetes.io/version"]).Should(Equal(Version))

	WaitFor(func() error {
		err := reconciler.reconcileOAuthService(ctx, instance, caBundle)
		return err
	}, "failed to create oauth service")

	desiredOAuthService, err := generateTrustyAIOAuthService(ctx, instance, caBundle)
	Expect(err).ToNot(HaveOccurred())

	oauthService := &corev1.Service{}
	WaitFor(func() error {
		return k8sClient.Get(ctx, types.NamespacedName{Name: desiredOAuthService.Name, Namespace: namespace}, oauthService)
	}, "failed to get OAuth Service")

	// Check if the OAuth service has the expected labels
	Expect(oauthService.Labels["app"]).Should(Equal(instance.Name))
	Expect(oauthService.Labels["app.kubernetes.io/instance"]).Should(Equal(instance.Name))
	Expect(oauthService.Labels["app.kubernetes.io/name"]).Should(Equal(instance.Name))
	Expect(oauthService.Labels["app.kubernetes.io/part-of"]).Should(Equal(componentName))
	Expect(oauthService.Labels["app.kubernetes.io/version"]).Should(Equal(Version))
	Expect(oauthService.Labels["trustyai-service-name"]).Should(Equal(instance.Name))

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
		It("Creates a deployment and a service with the default configuration in PVC-mode", func() {
			namespace := "trusty-ns-a-1-pvc"
			instance = createDefaultPVCCustomResource(namespace)
			setupAndTestDeploymentDefault(instance, namespace)
		})
		It("Creates a deployment and a service with the default configuration in DB-mode (mysql)", func() {
			namespace := "trusty-ns-a-1-db"
			instance = createDefaultDBCustomResource(namespace)
			WaitFor(func() error {
				secret := createDatabaseConfiguration(namespace, defaultDatabaseConfigurationName, "mysql")
				return k8sClient.Create(ctx, secret)
			}, "failed to create ConfigMap")
			setupAndTestDeploymentDefault(instance, namespace)
		})
		It("Creates a deployment and a service with the default configuration in DB-mode (mariadb)", func() {
			namespace := "trusty-ns-a-1-db"
			instance = createDefaultDBCustomResource(namespace)
			WaitFor(func() error {
				secret := createDatabaseConfiguration(namespace, defaultDatabaseConfigurationName, "mariadb")
				return k8sClient.Create(ctx, secret)
			}, "failed to create ConfigMap")
			setupAndTestDeploymentDefault(instance, namespace)
		})

	})

	Context("When deploying with a ConfigMap and without an InferenceService", func() {
		var instance *trustyaiopendatahubiov1alpha1.TrustyAIService

		It("Creates a deployment and a service with the ConfigMap configuration in PVC-mode", func() {
			namespace := "trusty-ns-a-1-cm-pvc"
			instance = createDefaultPVCCustomResource(namespace)
			setupAndTestDeploymentConfigMap(instance, namespace)
		})
		It("Creates a deployment and a service with the ConfigMap configuration in DB-mode", func() {
			namespace := "trusty-ns-a-1-cm-db"
			instance = createDefaultDBCustomResource(namespace)
			setupAndTestDeploymentConfigMap(instance, namespace)
		})

	})

	Context("When deploying with default settings without an InferenceService", func() {
		var instance *trustyaiopendatahubiov1alpha1.TrustyAIService

		It("should set environment variables correctly in PVC mode", func() {

			namespace := "trusty-ns-a-4-pvc"
			instance = createDefaultPVCCustomResource(namespace)
			//printKubeObject(instance)
			Expect(createNamespace(ctx, k8sClient, namespace)).To(Succeed())

			caBundle := reconciler.GetCustomCertificatesBundle(ctx, instance)

			Expect(createTestPVC(ctx, k8sClient, instance)).To(Succeed())
			Expect(reconciler.createServiceAccount(ctx, instance)).To(Succeed())
			Expect(reconciler.ensureDeployment(ctx, instance, caBundle, false)).To(Succeed())

			deployment := &appsv1.Deployment{}
			namespacedName := types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}
			Expect(k8sClient.Get(ctx, namespacedName, deployment)).Should(Succeed())

			//printKubeObject(deployment)

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

			//envVar = foundEnvVar(trustyaiServiceContainer.Env, "SERVICE_BATCH_SIZE")
			//Expect(envVar).NotTo(BeNil(), "Env var SERVICE_BATCH_SIZE not found")
			//Expect(envVar.Value).To(Equal("5000"))

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

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "QUARKUS_HIBERNATE_ORM_ACTIVE")
			Expect(envVar).NotTo(BeNil(), "Env var QUARKUS_HIBERNATE_ORM_ACTIVE not found")
			Expect(envVar.Value).To(Equal("false"))

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "QUARKUS_DATASOURCE_DB_KIND")
			Expect(envVar).To(BeNil())

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "QUARKUS_DATASOURCE_JDBC_MAX_SIZE")
			Expect(envVar).To(BeNil())

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "QUARKUS_DATASOURCE_USERNAME")
			Expect(envVar).To(BeNil())

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "QUARKUS_DATASOURCE_PASSWORD")
			Expect(envVar).To(BeNil())

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "DATABASE_SERVICE")
			Expect(envVar).To(BeNil())

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "DATABASE_PORT")
			Expect(envVar).To(BeNil())

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "QUARKUS_DATASOURCE_JDBC_URL")
			Expect(envVar).To(BeNil())

		})

		It("should set environment variables correctly in DB mode", func() {

			namespace := "trusty-ns-a-4-db"
			instance = createDefaultDBCustomResource(namespace)
			Expect(createNamespace(ctx, k8sClient, namespace)).To(Succeed())
			caBundle := reconciler.GetCustomCertificatesBundle(ctx, instance)

			Expect(createTestPVC(ctx, k8sClient, instance)).To(Succeed())
			Expect(reconciler.createServiceAccount(ctx, instance)).To(Succeed())
			Expect(reconciler.ensureDeployment(ctx, instance, caBundle, false)).To(Succeed())

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

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "STORAGE_DATA_FILENAME")
			Expect(envVar).To(BeNil())

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "SERVICE_STORAGE_FORMAT")
			Expect(envVar).NotTo(BeNil(), "Env var SERVICE_STORAGE_FORMAT not found")
			Expect(envVar.Value).To(Equal(STORAGE_DATABASE))

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "STORAGE_DATA_FOLDER")
			Expect(envVar).To(BeNil())

			//envVar = foundEnvVar(trustyaiServiceContainer.Env, "SERVICE_DATA_FORMAT")
			//Expect(envVar).To(BeNil())

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "SERVICE_METRICS_SCHEDULE")
			Expect(envVar).NotTo(BeNil(), "Env var SERVICE_METRICS_SCHEDULE not found")
			Expect(envVar.Value).To(Equal("5s"))

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "QUARKUS_HIBERNATE_ORM_ACTIVE")
			Expect(envVar).NotTo(BeNil(), "Env var QUARKUS_HIBERNATE_ORM_ACTIVE not found")
			Expect(envVar.Value).To(Equal("true"))

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "QUARKUS_DATASOURCE_DB_KIND")
			Expect(envVar).NotTo(BeNil(), "Env var QUARKUS_DATASOURCE_DB_KIND not found")
			Expect(envVar.ValueFrom).NotTo(BeNil(), "Env var QUARKUS_DATASOURCE_DB_KIND does not have ValueFrom set")
			Expect(envVar.ValueFrom.SecretKeyRef).NotTo(BeNil(), "Env var QUARKUS_DATASOURCE_DB_KIND is not using SecretKeyRef")
			Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(defaultDatabaseConfigurationName), "Secret name does not match")
			Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("databaseKind"), "Secret key does not match")

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "QUARKUS_DATASOURCE_JDBC_MAX_SIZE")
			Expect(envVar).NotTo(BeNil(), "Env var QUARKUS_DATASOURCE_JDBC_MAX_SIZE not found")
			Expect(envVar.Value).To(Equal("16"))

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "QUARKUS_DATASOURCE_USERNAME")
			Expect(envVar).NotTo(BeNil(), "Env var QUARKUS_DATASOURCE_USERNAME not found")
			Expect(envVar.ValueFrom).NotTo(BeNil(), "Env var QUARKUS_DATASOURCE_USERNAME does not have ValueFrom set")
			Expect(envVar.ValueFrom.SecretKeyRef).NotTo(BeNil(), "Env var QUARKUS_DATASOURCE_USERNAME is not using SecretKeyRef")
			Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(defaultDatabaseConfigurationName), "Secret name does not match")
			Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("databaseUsername"), "Secret key does not match")

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "QUARKUS_DATASOURCE_PASSWORD")
			Expect(envVar).NotTo(BeNil(), "Env var QUARKUS_DATASOURCE_PASSWORD not found")
			Expect(envVar.ValueFrom).NotTo(BeNil(), "Env var QUARKUS_DATASOURCE_PASSWORD does not have ValueFrom set")
			Expect(envVar.ValueFrom.SecretKeyRef).NotTo(BeNil(), "Env var QUARKUS_DATASOURCE_PASSWORD is not using SecretKeyRef")
			Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(defaultDatabaseConfigurationName), "Secret name does not match")
			Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("databasePassword"), "Secret key does not match")

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "DATABASE_SERVICE")
			Expect(envVar).NotTo(BeNil(), "Env var DATABASE_SERVICE not found")
			Expect(envVar.ValueFrom).NotTo(BeNil(), "Env var DATABASE_SERVICE does not have ValueFrom set")
			Expect(envVar.ValueFrom.SecretKeyRef).NotTo(BeNil(), "Env var DATABASE_SERVICE is not using SecretKeyRef")
			Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(defaultDatabaseConfigurationName), "Secret name does not match")
			Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("databaseService"), "Secret key does not match")

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "DATABASE_PORT")
			Expect(envVar).NotTo(BeNil(), "Env var DATABASE_PORT not found")
			Expect(envVar.ValueFrom).NotTo(BeNil(), "Env var DATABASE_PORT does not have ValueFrom set")
			Expect(envVar.ValueFrom.SecretKeyRef).NotTo(BeNil(), "Env var DATABASE_PORT is not using SecretKeyRef")
			Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(defaultDatabaseConfigurationName), "Secret name does not match")
			Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("databasePort"), "Secret key does not match")

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "QUARKUS_DATASOURCE_JDBC_URL")
			Expect(envVar).NotTo(BeNil(), "Env var QUARKUS_DATASOURCE_JDBC_URL not found")
			Expect(envVar.Value).To(Equal("jdbc:${QUARKUS_DATASOURCE_DB_KIND}://${DATABASE_SERVICE}:${DATABASE_PORT}/trustyai_database"))

		})

		It("should set environment variables correctly in migration mode", func() {

			namespace := "trusty-ns-a-4-migration"
			instance = createDefaultMigrationCustomResource(namespace)
			Expect(createNamespace(ctx, k8sClient, namespace)).To(Succeed())
			//printKubeObject(instance)
			caBundle := reconciler.GetCustomCertificatesBundle(ctx, instance)

			Expect(createTestPVC(ctx, k8sClient, instance)).To(Succeed())
			Expect(reconciler.createServiceAccount(ctx, instance)).To(Succeed())
			Expect(reconciler.ensureDeployment(ctx, instance, caBundle, false)).To(Succeed())

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

			//envVar = foundEnvVar(trustyaiServiceContainer.Env, "SERVICE_BATCH_SIZE")
			//Expect(envVar).To(BeNil(), "Env var SERVICE_BATCH_SIZE not found")
			//Expect(envVar.Value).To(Equal("5000"))

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "STORAGE_DATA_FILENAME")
			Expect(envVar).ToNot(BeNil())
			Expect(envVar.Value).To(Equal("data.csv"))

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "SERVICE_STORAGE_FORMAT")
			Expect(envVar).NotTo(BeNil(), "Env var SERVICE_STORAGE_FORMAT not found")
			Expect(envVar.Value).To(Equal(STORAGE_DATABASE))

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "STORAGE_DATA_FOLDER")
			Expect(envVar).ToNot(BeNil())
			Expect(envVar.Value).To(Equal("/data"))

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "SERVICE_DATA_FORMAT")
			Expect(envVar).ToNot(BeNil())
			Expect(envVar.Value).To(Equal("CSV"))

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "SERVICE_METRICS_SCHEDULE")
			Expect(envVar).NotTo(BeNil(), "Env var SERVICE_METRICS_SCHEDULE not found")
			Expect(envVar.Value).To(Equal("5s"))

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "QUARKUS_HIBERNATE_ORM_ACTIVE")
			Expect(envVar).NotTo(BeNil(), "Env var QUARKUS_HIBERNATE_ORM_ACTIVE not found")
			Expect(envVar.Value).To(Equal("true"))

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "QUARKUS_DATASOURCE_DB_KIND")
			Expect(envVar).NotTo(BeNil(), "Env var QUARKUS_DATASOURCE_DB_KIND not found")
			Expect(envVar.ValueFrom).NotTo(BeNil(), "Env var QUARKUS_DATASOURCE_DB_KIND does not have ValueFrom set")
			Expect(envVar.ValueFrom.SecretKeyRef).NotTo(BeNil(), "Env var QUARKUS_DATASOURCE_DB_KIND is not using SecretKeyRef")
			Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(defaultDatabaseConfigurationName), "Secret name does not match")
			Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("databaseKind"), "Secret key does not match")

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "QUARKUS_DATASOURCE_JDBC_MAX_SIZE")
			Expect(envVar).NotTo(BeNil(), "Env var QUARKUS_DATASOURCE_JDBC_MAX_SIZE not found")
			Expect(envVar.Value).To(Equal("16"))

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "QUARKUS_DATASOURCE_USERNAME")
			Expect(envVar).NotTo(BeNil(), "Env var QUARKUS_DATASOURCE_USERNAME not found")
			Expect(envVar.ValueFrom).NotTo(BeNil(), "Env var QUARKUS_DATASOURCE_USERNAME does not have ValueFrom set")
			Expect(envVar.ValueFrom.SecretKeyRef).NotTo(BeNil(), "Env var QUARKUS_DATASOURCE_USERNAME is not using SecretKeyRef")
			Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(defaultDatabaseConfigurationName), "Secret name does not match")
			Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("databaseUsername"), "Secret key does not match")

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "QUARKUS_DATASOURCE_PASSWORD")
			Expect(envVar).NotTo(BeNil(), "Env var QUARKUS_DATASOURCE_PASSWORD not found")
			Expect(envVar.ValueFrom).NotTo(BeNil(), "Env var QUARKUS_DATASOURCE_PASSWORD does not have ValueFrom set")
			Expect(envVar.ValueFrom.SecretKeyRef).NotTo(BeNil(), "Env var QUARKUS_DATASOURCE_PASSWORD is not using SecretKeyRef")
			Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(defaultDatabaseConfigurationName), "Secret name does not match")
			Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("databasePassword"), "Secret key does not match")

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "DATABASE_SERVICE")
			Expect(envVar).NotTo(BeNil(), "Env var DATABASE_SERVICE not found")
			Expect(envVar.ValueFrom).NotTo(BeNil(), "Env var DATABASE_SERVICE does not have ValueFrom set")
			Expect(envVar.ValueFrom.SecretKeyRef).NotTo(BeNil(), "Env var DATABASE_SERVICE is not using SecretKeyRef")
			Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(defaultDatabaseConfigurationName), "Secret name does not match")
			Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("databaseService"), "Secret key does not match")

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "DATABASE_PORT")
			Expect(envVar).NotTo(BeNil(), "Env var DATABASE_PORT not found")
			Expect(envVar.ValueFrom).NotTo(BeNil(), "Env var DATABASE_PORT does not have ValueFrom set")
			Expect(envVar.ValueFrom.SecretKeyRef).NotTo(BeNil(), "Env var DATABASE_PORT is not using SecretKeyRef")
			Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(defaultDatabaseConfigurationName), "Secret name does not match")
			Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("databasePort"), "Secret key does not match")

			envVar = foundEnvVar(trustyaiServiceContainer.Env, "QUARKUS_DATASOURCE_JDBC_URL")
			Expect(envVar).NotTo(BeNil(), "Env var QUARKUS_DATASOURCE_JDBC_URL not found")
			Expect(envVar.Value).To(Equal("jdbc:${QUARKUS_DATASOURCE_DB_KIND}://${DATABASE_SERVICE}:${DATABASE_PORT}/trustyai_database"))

		})

	})

	Context("When deploying with no custom CA bundle ConfigMap", func() {
		var instance *trustyaiopendatahubiov1alpha1.TrustyAIService

		It("should use the correct service account and not include CustomCertificatesBundle in PVC-mode", func() {

			namespace := "trusty-ns-a-7-pvc"
			instance = createDefaultPVCCustomResource(namespace)
			setupAndTestDeploymentNoCustomCABundle(instance, namespace)
		})
		It("should use the correct service account and not include CustomCertificatesBundle in DB-mode", func() {

			namespace := "trusty-ns-a-7-db"
			instance = createDefaultDBCustomResource(namespace)
			setupAndTestDeploymentNoCustomCABundle(instance, namespace)
		})
		It("should use the correct service account and not include CustomCertificatesBundle in migration-mode", func() {

			namespace := "trusty-ns-a-7-migration"
			instance = createDefaultMigrationCustomResource(namespace)
			setupAndTestDeploymentNoCustomCABundle(instance, namespace)
		})

	})

	Context("When deploying with a custom CA bundle ConfigMap", func() {
		var instance *trustyaiopendatahubiov1alpha1.TrustyAIService

		It("should use the correct service account and include CustomCertificatesBundle in PVC-mode", func() {

			namespace := "trusty-ns-a-8-pvc"
			instance = createDefaultPVCCustomResource(namespace)
			setupAndTestDeploymentCustomCABundle(instance, namespace)
		})
		It("should use the correct service account and include CustomCertificatesBundle in DB-mode", func() {

			namespace := "trusty-ns-a-8-db"
			instance = createDefaultDBCustomResource(namespace)
			setupAndTestDeploymentCustomCABundle(instance, namespace)
		})
		It("should use the correct service account and include CustomCertificatesBundle in migration-mode", func() {

			namespace := "trusty-ns-a-8-migration"
			instance = createDefaultMigrationCustomResource(namespace)
			setupAndTestDeploymentCustomCABundle(instance, namespace)
		})
	})

	Context("When deploying with default settings without an InferenceService", func() {
		var instance *trustyaiopendatahubiov1alpha1.TrustyAIService

		It("should use the correct service account in PVC-mode", func() {

			namespace := "trusty-ns-a-6-pvc"
			instance = createDefaultPVCCustomResource(namespace)
			setupAndTestDeploymentServiceAccount(instance, namespace, "PVC")

		})
		It("should use the correct service account in DB-mode", func() {

			namespace := "trusty-ns-a-6-db"
			instance = createDefaultDBCustomResource(namespace)
			setupAndTestDeploymentServiceAccount(instance, namespace, "DATABASE")

		})
		It("should use the correct service account in migration-mode", func() {

			namespace := "trusty-ns-a-6-migration"
			instance = createDefaultMigrationCustomResource(namespace)
			setupAndTestDeploymentServiceAccount(instance, namespace, "DATABASE")

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

		It("Sets up the InferenceService and links it to the TrustyAIService deployment in PVC-mode", func() {

			namespace := "trusty-ns-2-pvc"
			instance := createDefaultPVCCustomResource(namespace)
			setupAndTestDeploymentInferenceService(instance, namespace, "PVC")

		})
		It("Sets up the InferenceService and links it to the TrustyAIService deployment in DB-mode", func() {

			namespace := "trusty-ns-2-db"
			instance := createDefaultDBCustomResource(namespace)
			setupAndTestDeploymentInferenceService(instance, namespace, "DATABASE")

		})
		It("Sets up the InferenceService and links it to the TrustyAIService deployment in migration-mode", func() {

			namespace := "trusty-ns-2-migration"
			instance := createDefaultMigrationCustomResource(namespace)
			setupAndTestDeploymentInferenceService(instance, namespace, "DATABASE")

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
				instances[i] = createDefaultPVCCustomResource(namespace)
				instances[i].Namespace = namespace
				WaitFor(func() error {
					return createNamespace(ctx, k8sClient, namespace)
				}, "failed to create namespace")
			}

			for _, instance := range instances {
				caBundle := reconciler.GetCustomCertificatesBundle(ctx, instance)

				WaitFor(func() error {
					return createTestPVC(ctx, k8sClient, instance)
				}, "failed to create PVC")
				WaitFor(func() error {
					return reconciler.ensureDeployment(ctx, instance, caBundle, false)
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
				Expect(deployment.Labels["app.kubernetes.io/version"]).Should(Equal(Version))

				Expect(len(deployment.Spec.Template.Spec.Containers)).Should(Equal(2))
				Expect(deployment.Spec.Template.Spec.Containers[0].Image).Should(Equal("quay.io/trustyai/trustyai-service:latest"))
				Expect(deployment.Spec.Template.Spec.Containers[1].Image).Should(Equal("registry.redhat.io/openshift4/ose-oauth-proxy:latest"))

				WaitFor(func() error {
					err := reconciler.reconcileOAuthService(ctx, instance, caBundle)
					return err
				}, "failed to create oauth service")

				desiredOAuthService, err := generateTrustyAIOAuthService(ctx, instance, caBundle)
				Expect(err).ToNot(HaveOccurred())

				oauthService := &corev1.Service{}
				WaitFor(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: desiredOAuthService.Name, Namespace: instance.Namespace}, oauthService)
				}, "failed to get OAuth Service")

				// Check if the OAuth service has the expected labels
				Expect(oauthService.Labels["app"]).Should(Equal(instance.Name))
				Expect(oauthService.Labels["app.kubernetes.io/instance"]).Should(Equal(instance.Name))
				Expect(oauthService.Labels["app.kubernetes.io/name"]).Should(Equal(instance.Name))
				Expect(oauthService.Labels["app.kubernetes.io/part-of"]).Should(Equal(componentName))
				Expect(oauthService.Labels["app.kubernetes.io/version"]).Should(Equal(Version))
				Expect(oauthService.Labels["trustyai-service-name"]).Should(Equal(instance.Name))

			}
		})
	})

})

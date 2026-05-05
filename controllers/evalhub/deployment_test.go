package evalhub

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
)

var _ = Describe("EvalHub Deployment", func() {
	const (
		testNamespacePrefix = "evalhub-deployment-test"
		evalHubName         = "deployment-evalhub"
		configMapName       = "trustyai-service-operator-config"
	)

	var (
		testNamespace string
		namespace     *corev1.Namespace
		configMap     *corev1.ConfigMap
		evalHub       *evalhubv1alpha1.EvalHub
		reconciler    *EvalHubReconciler
	)

	BeforeEach(func() {
		// Create unique namespace name to avoid conflicts
		testNamespace = fmt.Sprintf("%s-%d", testNamespacePrefix, time.Now().UnixNano())

		// Create test namespace
		namespace = createNamespace(testNamespace)
		Expect(k8sClient.Create(ctx, namespace)).Should(Succeed())

		// Create config map for image configuration
		configMap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: testNamespace,
			},
			Data: map[string]string{
				"evalHubImage":    "quay.io/ruimvieira/eval-hub:test",
				"kube-rbac-proxy": "quay.io/openshift/origin-kube-rbac-proxy:4.19",
			},
		}
		Expect(k8sClient.Create(ctx, configMap)).Should(Succeed())

		// Setup reconciler
		reconciler, _ = setupReconciler(testNamespace)
	})

	AfterEach(func() {
		// Clean up resources in namespace first
		cleanupResourcesInNamespace(testNamespace, evalHub, configMap)

		// Then delete namespace
		deleteNamespace(namespace)

		// Reset variables
		evalHub = nil
		configMap = nil
		namespace = nil
	})

	Context("When reconciling deployment", func() {
		BeforeEach(func() {
			evalHub = createEvalHubInstance(evalHubName, testNamespace)
			Expect(k8sClient.Create(ctx, evalHub)).Should(Succeed())
		})

		It("should create deployment with correct specifications", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub, nil, nil)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying deployment exists")
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName,
				Namespace: testNamespace,
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Checking deployment specifications")
			Expect(deployment.Name).To(Equal(evalHubName))
			Expect(deployment.Namespace).To(Equal(testNamespace))
			Expect(*deployment.Spec.Replicas).To(Equal(*evalHub.Spec.Replicas))

			// Check owner reference
			Expect(deployment.OwnerReferences).To(HaveLen(1))
			Expect(deployment.OwnerReferences[0].Name).To(Equal(evalHub.Name))
			Expect(deployment.OwnerReferences[0].Kind).To(Equal("EvalHub"))

			// Check labels
			labels := deployment.Spec.Selector.MatchLabels
			Expect(labels["app"]).To(Equal("eval-hub"))
			Expect(labels["instance"]).To(Equal(evalHubName))
			Expect(labels["component"]).To(Equal("api"))
		})

		It("should configure evalhub container correctly", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub, nil, nil)
			Expect(err).NotTo(HaveOccurred())

			By("Getting deployment")
			deployment := waitForDeployment(evalHubName, testNamespace)

			By("Finding evalhub and kube-rbac-proxy containers")
			Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(2))
			var evalHubContainer *corev1.Container
			for _, container := range deployment.Spec.Template.Spec.Containers {
				if container.Name == "evalhub" {
					evalHubContainer = &container
					break
				}
			}
			Expect(evalHubContainer).NotTo(BeNil(), "evalhub container should be present")

			By("Checking evalhub container configuration")
			Expect(evalHubContainer.Image).To(Equal("quay.io/ruimvieira/eval-hub:test")) // From test configmap
			Expect(evalHubContainer.ImagePullPolicy).To(Equal(corev1.PullAlways))

			// Check ports (loopback app port; Service targets kube-rbac-proxy on 8443)
			Expect(evalHubContainer.Ports).To(HaveLen(1))
			Expect(evalHubContainer.Ports[0].Name).To(Equal("evalhub"))
			Expect(evalHubContainer.Ports[0].ContainerPort).To(Equal(int32(evalHubAppPort)))
			Expect(evalHubContainer.Ports[0].Protocol).To(Equal(corev1.ProtocolTCP))

			var krp *corev1.Container
			for i := range deployment.Spec.Template.Spec.Containers {
				if deployment.Spec.Template.Spec.Containers[i].Name == kubeRBACProxyContainerName {
					krp = &deployment.Spec.Template.Spec.Containers[i]
					break
				}
			}
			Expect(krp).NotTo(BeNil())
			Expect(krp.Image).To(Equal("quay.io/openshift/origin-kube-rbac-proxy:4.19"))
			Expect(krp.Args).To(ContainElement("--config-file=" + kubeRBACProxyConfigMountPath))
			Expect(strings.Join(krp.Args, " ")).To(ContainSubstring(fmt.Sprintf("--upstream=http://127.0.0.1:%d/", evalHubAppPort)))
			var hasAuthMount bool
			for _, m := range krp.VolumeMounts {
				if m.Name == "evalhub-config" && m.MountPath == kubeRBACProxyConfigMountPath && m.SubPath == evalHubAuthConfigMapKey {
					hasAuthMount = true
				}
			}
			Expect(hasAuthMount).To(BeTrue())
		})

		It("should set default environment variables", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub, nil, nil)
			Expect(err).NotTo(HaveOccurred())

			By("Getting deployment")
			deployment := waitForDeployment(evalHubName, testNamespace)

			By("Finding evalhub container")
			var evalHubContainer *corev1.Container
			for _, container := range deployment.Spec.Template.Spec.Containers {
				if container.Name == "evalhub" {
					evalHubContainer = &container
					break
				}
			}
			Expect(evalHubContainer).NotTo(BeNil())

			By("Checking default environment variables")
			envVars := make(map[string]string)
			for _, env := range evalHubContainer.Env {
				envVars[env.Name] = env.Value
			}

			// EvalHub serves TLS on loopback; kube-rbac-proxy fronts the Service on 8443
			Expect(envVars["API_HOST"]).To(Equal("127.0.0.1"))
			Expect(envVars["PORT"]).To(Equal(fmt.Sprintf("%d", evalHubAppPort)))
			Expect(envVars["TLS_CERT_FILE"]).To(Equal("/etc/tls/private/tls.crt"))
			Expect(envVars["TLS_KEY_FILE"]).To(Equal("/etc/tls/private/tls.key"))
			Expect(envVars["LOG_LEVEL"]).To(Equal("INFO"))
			Expect(envVars["MAX_CONCURRENT_EVALUATIONS"]).To(Equal("10"))
			Expect(envVars["DEFAULT_TIMEOUT_MINUTES"]).To(Equal("60"))
			Expect(envVars["MAX_RETRY_ATTEMPTS"]).To(Equal("3"))
			Expect(envVars["SERVICE_URL"]).To(Equal(fmt.Sprintf("https://%s.%s.svc.cluster.local:8443", evalHubName, testNamespace)))
			Expect(envVars["EVALHUB_INSTANCE_NAME"]).To(Equal(evalHubName))
		})

		It("should include custom environment variables", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub, nil, nil)
			Expect(err).NotTo(HaveOccurred())

			By("Getting deployment")
			deployment := waitForDeployment(evalHubName, testNamespace)

			By("Finding evalhub container")
			var evalHubContainer *corev1.Container
			for _, container := range deployment.Spec.Template.Spec.Containers {
				if container.Name == "evalhub" {
					evalHubContainer = &container
					break
				}
			}
			Expect(evalHubContainer).NotTo(BeNil())

			By("Checking custom environment variables")
			var hasTestEnv bool
			for _, env := range evalHubContainer.Env {
				if env.Name == "TEST_ENV" && env.Value == "test-value" {
					hasTestEnv = true
					break
				}
			}
			Expect(hasTestEnv).To(BeTrue())
		})

		It("should configure resource requirements", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub, nil, nil)
			Expect(err).NotTo(HaveOccurred())

			By("Getting deployment")
			deployment := waitForDeployment(evalHubName, testNamespace)

			By("Finding evalhub container")
			var evalHubContainer *corev1.Container
			for _, container := range deployment.Spec.Template.Spec.Containers {
				if container.Name == "evalhub" {
					evalHubContainer = &container
					break
				}
			}
			Expect(evalHubContainer).NotTo(BeNil())

			By("Checking resource requirements")
			Expect(evalHubContainer.Resources.Requests).NotTo(BeNil())
			Expect(evalHubContainer.Resources.Limits).NotTo(BeNil())

			// Check CPU and memory are set (exact values from constants.go)
			cpuRequest := evalHubContainer.Resources.Requests[corev1.ResourceCPU]
			memRequest := evalHubContainer.Resources.Requests[corev1.ResourceMemory]
			cpuLimit := evalHubContainer.Resources.Limits[corev1.ResourceCPU]
			memLimit := evalHubContainer.Resources.Limits[corev1.ResourceMemory]

			Expect((&cpuRequest).Cmp(resource.MustParse("500m"))).To(Equal(0))
			Expect((&memRequest).Cmp(resource.MustParse("512Mi"))).To(Equal(0))
			Expect((&cpuLimit).Cmp(resource.MustParse("2000m"))).To(Equal(0))
			Expect((&memLimit).Cmp(resource.MustParse("2Gi"))).To(Equal(0))
		})

		It("should configure security contexts", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub, nil, nil)
			Expect(err).NotTo(HaveOccurred())

			By("Getting deployment")
			deployment := waitForDeployment(evalHubName, testNamespace)

			By("Checking pod security context")
			podSecurityContext := deployment.Spec.Template.Spec.SecurityContext
			Expect(podSecurityContext).NotTo(BeNil())
			Expect(*podSecurityContext.RunAsNonRoot).To(BeTrue())

			By("Checking container security context")
			var evalHubContainer *corev1.Container
			for i, container := range deployment.Spec.Template.Spec.Containers {
				if container.Name == "evalhub" {
					evalHubContainer = &deployment.Spec.Template.Spec.Containers[i]
					break
				}
			}
			Expect(evalHubContainer).NotTo(BeNil())
			containerSecurityContext := evalHubContainer.SecurityContext
			Expect(containerSecurityContext).NotTo(BeNil())
			Expect(*containerSecurityContext.AllowPrivilegeEscalation).To(BeFalse())
			Expect(*containerSecurityContext.RunAsNonRoot).To(BeTrue())
			Expect(containerSecurityContext.Capabilities.Drop).To(ContainElement(corev1.Capability("ALL")))
		})

		It("should update existing deployment", func() {
			By("Creating initial deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub, nil, nil)
			Expect(err).NotTo(HaveOccurred())

			By("Modifying EvalHub replicas")
			newReplicas := int32(3)
			evalHub.Spec.Replicas = &newReplicas
			err = k8sClient.Update(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Reconciling deployment again")
			err = reconciler.reconcileDeployment(ctx, evalHub, nil, nil)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying deployment is updated")
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName,
				Namespace: testNamespace,
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(*deployment.Spec.Replicas).To(Equal(int32(3)))
		})

		It("should reconcile deployment with default images when operator ConfigMap is missing", func() {
			By("Deleting the operator ConfigMap so image keys are unavailable")
			err := k8sClient.Delete(ctx, configMap)
			Expect(err).NotTo(HaveOccurred())

			By("Reconciling deployment; getEvalHubImage/getKubeRBACProxyImage fall back to built-in defaults")
			err = reconciler.reconcileDeployment(ctx, evalHub, nil, nil)
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: evalHubName, Namespace: testNamespace}, deployment)
			Expect(err).NotTo(HaveOccurred())

			var evalHubC, krp *corev1.Container
			for i := range deployment.Spec.Template.Spec.Containers {
				c := &deployment.Spec.Template.Spec.Containers[i]
				switch c.Name {
				case containerName:
					evalHubC = c
				case kubeRBACProxyContainerName:
					krp = c
				}
			}
			Expect(evalHubC).NotTo(BeNil())
			Expect(krp).NotTo(BeNil())
			Expect(evalHubC.Image).To(Equal(defaultEvalHubImage))
			Expect(krp.Image).To(Equal(defaultKubeRBACProxyImage))
		})

		It("should configure rolling update strategy", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub, nil, nil)
			Expect(err).NotTo(HaveOccurred())

			By("Getting deployment")
			deployment := waitForDeployment(evalHubName, testNamespace)

			By("Checking deployment strategy")
			Expect(deployment.Spec.Strategy.Type).To(Equal(appsv1.RollingUpdateDeploymentStrategyType))
			Expect(deployment.Spec.Strategy.RollingUpdate).NotTo(BeNil())
			Expect(deployment.Spec.Strategy.RollingUpdate.MaxUnavailable.StrVal).To(Equal("25%"))
			Expect(deployment.Spec.Strategy.RollingUpdate.MaxSurge.StrVal).To(Equal("25%"))
		})
	})

	Context("When database is configured", func() {
		It("should add DB secret volume and mount to deployment", func() {
			By("Creating EvalHub with database config")
			dbEvalHub := createEvalHubInstanceWithDB("db-"+evalHubName, testNamespace, "evalhub-db-credentials")
			Expect(k8sClient.Create(ctx, dbEvalHub)).Should(Succeed())
			defer k8sClient.Delete(ctx, dbEvalHub)

			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, dbEvalHub, nil, nil)
			Expect(err).NotTo(HaveOccurred())

			By("Getting deployment")
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "db-" + evalHubName,
				Namespace: testNamespace,
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Checking deployment has 5 volumes (4 base + DB secret)")
			Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(5))

			By("Finding DB secret volume")
			var dbVolume *corev1.Volume
			for i, volume := range deployment.Spec.Template.Spec.Volumes {
				if volume.Name == "evalhub-db-secret" {
					dbVolume = &deployment.Spec.Template.Spec.Volumes[i]
					break
				}
			}
			Expect(dbVolume).NotTo(BeNil(), "DB secret volume should be present")
			Expect(dbVolume.VolumeSource.Secret).NotTo(BeNil())
			Expect(dbVolume.VolumeSource.Secret.SecretName).To(Equal("evalhub-db-credentials"))
			Expect(dbVolume.VolumeSource.Secret.Items).To(HaveLen(1))
			Expect(dbVolume.VolumeSource.Secret.Items[0].Key).To(Equal("db-url"))
			Expect(dbVolume.VolumeSource.Secret.Items[0].Path).To(Equal("db-url"))

			By("Finding evalhub container")
			var evalHubContainer *corev1.Container
			for i, container := range deployment.Spec.Template.Spec.Containers {
				if container.Name == "evalhub" {
					evalHubContainer = &deployment.Spec.Template.Spec.Containers[i]
					break
				}
			}
			Expect(evalHubContainer).NotTo(BeNil())

			By("Checking evalhub container has 5 volume mounts (config + service-ca + mlflow-token + TLS + DB secret)")
			Expect(evalHubContainer.VolumeMounts).To(HaveLen(5))

			var dbMount *corev1.VolumeMount
			for i, mount := range evalHubContainer.VolumeMounts {
				if mount.Name == "evalhub-db-secret" {
					dbMount = &evalHubContainer.VolumeMounts[i]
					break
				}
			}
			Expect(dbMount).NotTo(BeNil(), "DB secret volume mount should be present")
			Expect(dbMount.MountPath).To(Equal("/etc/evalhub/secrets"))
			Expect(dbMount.ReadOnly).To(BeTrue())
		})

		It("should not add DB secret volume when database is not configured", func() {
			By("Creating standard EvalHub (no DB)")
			evalHub = createEvalHubInstance(evalHubName, testNamespace)
			Expect(k8sClient.Create(ctx, evalHub)).Should(Succeed())

			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub, nil, nil)
			Expect(err).NotTo(HaveOccurred())

			By("Getting deployment")
			deployment := waitForDeployment(evalHubName, testNamespace)

			By("Checking deployment has only 4 base volumes")
			Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(4))

			By("Finding evalhub container")
			var evalHubContainer *corev1.Container
			for _, container := range deployment.Spec.Template.Spec.Containers {
				if container.Name == "evalhub" {
					evalHubContainer = &container
					break
				}
			}
			Expect(evalHubContainer).NotTo(BeNil())

			By("Checking evalhub container has 4 base volume mounts (config + service-ca + mlflow-token + TLS)")
			Expect(evalHubContainer.VolumeMounts).To(HaveLen(4))
			Expect(evalHubContainer.VolumeMounts[0].Name).To(Equal("evalhub-config"))
		})
	})

	Context("When handling deployment errors", func() {
		It("should handle missing EvalHub instance", func() {
			By("Creating deployment spec for non-existent EvalHub")
			nonExistentEvalHub := &evalhubv1alpha1.EvalHub{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "non-existent",
					Namespace: testNamespace,
				},
			}

			By("Attempting to reconcile deployment")
			err := reconciler.reconcileDeployment(ctx, nonExistentEvalHub, nil, nil)
			Expect(err).To(HaveOccurred())
		})

	})

	Context("When configuring TLS and volumes", func() {
		BeforeEach(func() {
			evalHub = createEvalHubInstance(evalHubName, testNamespace)
			Expect(k8sClient.Create(ctx, evalHub)).Should(Succeed())
		})

		It("should mount TLS secret on evalhub container", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub, nil, nil)
			Expect(err).NotTo(HaveOccurred())

			By("Getting deployment")
			deployment := waitForDeployment(evalHubName, testNamespace)

			By("Finding evalhub container")
			var evalHubContainer *corev1.Container
			for _, container := range deployment.Spec.Template.Spec.Containers {
				if container.Name == "evalhub" {
					evalHubContainer = &container
					break
				}
			}
			Expect(evalHubContainer).NotTo(BeNil())

			By("Checking TLS volume mount on evalhub container")
			var tlsMount *corev1.VolumeMount
			for _, mount := range evalHubContainer.VolumeMounts {
				if mount.Name == evalHubName+"-tls" {
					tlsMount = &mount
				}
			}
			Expect(tlsMount).NotTo(BeNil())
			Expect(tlsMount.MountPath).To(Equal("/etc/tls/private"))
			Expect(tlsMount.ReadOnly).To(BeTrue())
		})

		It("should configure deployment volumes", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub, nil, nil)
			Expect(err).NotTo(HaveOccurred())

			By("Getting deployment")
			deployment := waitForDeployment(evalHubName, testNamespace)

			By("Checking deployment volumes (config + TLS + service-ca + mlflow-token)")
			Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(4))

			var evalHubConfigVolume, tlsVolume *corev1.Volume
			for _, volume := range deployment.Spec.Template.Spec.Volumes {
				if volume.Name == "evalhub-config" {
					evalHubConfigVolume = &volume
				} else if volume.Name == evalHubName+"-tls" {
					tlsVolume = &volume
				}
			}

			By("Checking evalhub config volume")
			Expect(evalHubConfigVolume).NotTo(BeNil())
			Expect(evalHubConfigVolume.VolumeSource.ConfigMap).NotTo(BeNil())
			Expect(evalHubConfigVolume.VolumeSource.ConfigMap.Name).To(Equal(evalHubName + "-config"))

			By("Checking TLS volume")
			Expect(tlsVolume).NotTo(BeNil())
			Expect(tlsVolume.VolumeSource.Secret).NotTo(BeNil())
			Expect(tlsVolume.VolumeSource.Secret.SecretName).To(Equal(evalHubName + "-tls"))
		})

		It("should configure service account for API", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub, nil, nil)
			Expect(err).NotTo(HaveOccurred())

			By("Getting deployment")
			deployment := waitForDeployment(evalHubName, testNamespace)

			By("Checking service account name uses -service suffix")
			Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(Equal(evalHubName + "-service"))
		})
	})
})

// EvalHubReconciler reconcileDeployment specs (migrated from unit_test fake-client tests) use the suite
// envtest cluster, real client-go scheme, and controller-runtime client from BeforeSuite.
var _ = Describe("EvalHubReconciler reconcileDeployment", func() {
	const parityEvalHubName = "reconcile-parity-evalhub"

	var (
		testNamespace string
		namespace     *corev1.Namespace
		evalHubInst   *evalhubv1alpha1.EvalHub
		operatorCM    *corev1.ConfigMap
		reconciler    *EvalHubReconciler
	)

	BeforeEach(func() {
		testNamespace = fmt.Sprintf("evalhub-reconcile-parity-%d", time.Now().UnixNano())
		namespace = createNamespace(testNamespace)
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

		operatorCM = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: testNamespace,
			},
			Data: map[string]string{
				configMapEvalHubImageKey:       "quay.io/test/eval-hub:latest",
				configMapKubeRBACProxyImageKey: "quay.io/test/kube-rbac-proxy:latest",
			},
		}
		Expect(k8sClient.Create(ctx, operatorCM)).To(Succeed())

		replicas := int32(2)
		evalHubInst = &evalhubv1alpha1.EvalHub{
			TypeMeta: metav1.TypeMeta{
				APIVersion: evalhubv1alpha1.GroupVersion.String(),
				Kind:       "EvalHub",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      parityEvalHubName,
				Namespace: testNamespace,
			},
			Spec: evalhubv1alpha1.EvalHubSpec{
				Replicas:  &replicas,
				Providers: []string{},
				Env: []corev1.EnvVar{
					{Name: "TEST_VAR", Value: "test-value"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, evalHubInst)).To(Succeed())

		reconciler = &EvalHubReconciler{
			Client:                k8sClient,
			Scheme:                scheme.Scheme,
			Namespace:             testNamespace,
			OperatorConfigMapName: configMapName,
			EventRecorder:         record.NewFakeRecorder(100),
		}
	})

	AfterEach(func() {
		cleanupResourcesInNamespace(testNamespace, evalHubInst, operatorCM)
		_ = k8sClient.Delete(ctx, &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: parityEvalHubName, Namespace: testNamespace},
		})
		deleteNamespace(namespace)
		evalHubInst, operatorCM, namespace = nil, nil, nil
	})

	It("creates the Deployment with expected pod spec from reconcileDeployment", func() {
		Expect(reconciler.reconcileDeployment(ctx, evalHubInst, nil, nil)).To(Succeed())

		deployment := waitForDeployment(parityEvalHubName, testNamespace)
		Expect(deployment.Name).To(Equal(parityEvalHubName))
		Expect(*deployment.Spec.Replicas).To(Equal(int32(2)))
		Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(2))

		var evalHubC *corev1.Container
		var krp *corev1.Container
		for i := range deployment.Spec.Template.Spec.Containers {
			c := &deployment.Spec.Template.Spec.Containers[i]
			switch c.Name {
			case containerName:
				evalHubC = c
			case kubeRBACProxyContainerName:
				krp = c
			}
		}
		Expect(evalHubC).NotTo(BeNil())
		Expect(krp).NotTo(BeNil())

		Expect(evalHubC.Image).To(Equal("quay.io/test/eval-hub:latest"))
		Expect(evalHubC.ImagePullPolicy).To(Equal(corev1.PullAlways))
		Expect(evalHubC.Ports).To(HaveLen(1))
		Expect(evalHubC.Ports[0].Name).To(Equal("evalhub"))
		Expect(evalHubC.Ports[0].ContainerPort).To(Equal(int32(evalHubAppPort)))

		envs := map[string]string{}
		for _, e := range evalHubC.Env {
			envs[e.Name] = e.Value
		}
		Expect(envs["API_HOST"]).To(Equal("127.0.0.1"))
		Expect(envs["PORT"]).To(Equal(fmt.Sprintf("%d", evalHubAppPort)))
		Expect(envs["TEST_VAR"]).To(Equal("test-value"))
		Expect(envs["SERVICE_URL"]).To(Equal(fmt.Sprintf("https://%s.%s.svc.cluster.local:%d", parityEvalHubName, testNamespace, servicePort)))
		Expect(envs["EVALHUB_INSTANCE_NAME"]).To(Equal(parityEvalHubName))

		Expect(evalHubC.Resources.Requests[corev1.ResourceCPU]).To(Equal(resource.MustParse("500m")))
		Expect(evalHubC.Resources.Requests[corev1.ResourceMemory]).To(Equal(resource.MustParse("512Mi")))

		Expect(evalHubC.ReadinessProbe).To(BeNil())
		Expect(evalHubC.LivenessProbe).To(BeNil())

		Expect(krp.Image).To(Equal("quay.io/test/kube-rbac-proxy:latest"))
		Expect(strings.Join(krp.Args, " ")).To(ContainSubstring(fmt.Sprintf("--upstream=http://127.0.0.1:%d/", evalHubAppPort)))
		Expect(krp.Args).To(ContainElement("--config-file=" + kubeRBACProxyConfigMountPath))

		By("kube-rbac-proxy: kubelet probes hit HTTPS on servicePort (same path as clients; --ignore-paths on eval-hub)")
		Expect(krp.ReadinessProbe).NotTo(BeNil())
		Expect(krp.ReadinessProbe.HTTPGet).NotTo(BeNil())
		Expect(krp.ReadinessProbe.HTTPGet.Path).To(Equal(evalHubHealthPath))
		Expect(krp.ReadinessProbe.HTTPGet.Port.IntVal).To(Equal(int32(servicePort)))
		Expect(krp.ReadinessProbe.HTTPGet.Scheme).To(Equal(corev1.URISchemeHTTPS))
		Expect(krp.ReadinessProbe.InitialDelaySeconds).To(Equal(int32(10)))
		Expect(krp.ReadinessProbe.PeriodSeconds).To(Equal(int32(5)))
		Expect(krp.ReadinessProbe.TimeoutSeconds).To(Equal(int32(5)))

		Expect(krp.LivenessProbe).NotTo(BeNil())
		Expect(krp.LivenessProbe.HTTPGet).NotTo(BeNil())
		Expect(krp.LivenessProbe.HTTPGet.Path).To(Equal(evalHubHealthPath))
		Expect(krp.LivenessProbe.HTTPGet.Port.IntVal).To(Equal(int32(servicePort)))
		Expect(krp.LivenessProbe.HTTPGet.Scheme).To(Equal(corev1.URISchemeHTTPS))
		Expect(krp.LivenessProbe.InitialDelaySeconds).To(Equal(int32(30)))
		Expect(krp.LivenessProbe.PeriodSeconds).To(Equal(int32(10)))
		Expect(krp.LivenessProbe.TimeoutSeconds).To(Equal(int32(5)))
	})

	It("uses default EvalHub and kube-rbac-proxy images when operator ConfigMap is absent", func() {
		fallbackNS := fmt.Sprintf("evalhub-reconcile-fallback-%d", time.Now().UnixNano())
		ns := createNamespace(fallbackNS)
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		defer deleteNamespace(ns)

		r1 := int32(1)
		fh := &evalhubv1alpha1.EvalHub{
			TypeMeta: metav1.TypeMeta{
				APIVersion: evalhubv1alpha1.GroupVersion.String(),
				Kind:       "EvalHub",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "fallback-evalhub",
				Namespace: fallbackNS,
			},
			Spec: evalhubv1alpha1.EvalHubSpec{
				Replicas:  &r1,
				Providers: []string{},
			},
		}
		Expect(k8sClient.Create(ctx, fh)).To(Succeed())

		r := &EvalHubReconciler{
			Client:                k8sClient,
			Scheme:                scheme.Scheme,
			Namespace:             fallbackNS,
			OperatorConfigMapName: configMapName,
			EventRecorder:         record.NewFakeRecorder(100),
		}
		Expect(r.reconcileDeployment(ctx, fh, nil, nil)).To(Succeed())

		deployment := waitForDeployment("fallback-evalhub", fallbackNS)

		var evalHubC *corev1.Container
		var krp *corev1.Container
		for i := range deployment.Spec.Template.Spec.Containers {
			c := &deployment.Spec.Template.Spec.Containers[i]
			switch c.Name {
			case containerName:
				evalHubC = c
			case kubeRBACProxyContainerName:
				krp = c
			}
		}
		Expect(evalHubC).NotTo(BeNil())
		Expect(krp).NotTo(BeNil())
		Expect(evalHubC.Image).To(Equal(defaultEvalHubImage))
		Expect(krp.Image).To(Equal(defaultKubeRBACProxyImage))
	})
})

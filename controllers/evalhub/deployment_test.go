package evalhub

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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

		// Create config map for image configuration including kube-rbac-proxy image
		configMap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: testNamespace,
			},
			Data: map[string]string{
				"evalHubImage":    "quay.io/ruimvieira/eval-hub:test",
				"kube-rbac-proxy": "gcr.io/kubebuilder/kube-rbac-proxy:v0.13.1",
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
			err := reconciler.reconcileDeployment(ctx, evalHub)
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
			err := reconciler.reconcileDeployment(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting deployment")
			deployment := waitForDeployment(evalHubName, testNamespace)

			By("Finding evalhub container")
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

			// Check ports
			Expect(evalHubContainer.Ports).To(HaveLen(1))
			Expect(evalHubContainer.Ports[0].Name).To(Equal("http"))
			Expect(evalHubContainer.Ports[0].ContainerPort).To(Equal(int32(8080)))
			Expect(evalHubContainer.Ports[0].Protocol).To(Equal(corev1.ProtocolTCP))
		})

		It("should set default environment variables", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub)
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

			// API_HOST is 127.0.0.1 to ensure only kube-rbac-proxy can reach the API (security hardening)
			Expect(envVars["API_HOST"]).To(Equal("127.0.0.1"))
			Expect(envVars["API_PORT"]).To(Equal("8080"))
			Expect(envVars["LOG_LEVEL"]).To(Equal("INFO"))
			Expect(envVars["MAX_CONCURRENT_EVALUATIONS"]).To(Equal("10"))
			Expect(envVars["DEFAULT_TIMEOUT_MINUTES"]).To(Equal("60"))
			Expect(envVars["MAX_RETRY_ATTEMPTS"]).To(Equal("3"))
			// SERVICE_URL uses https on port 8443 via kube-rbac-proxy
			Expect(envVars["SERVICE_URL"]).To(Equal(fmt.Sprintf("https://%s.%s.svc.cluster.local:8443", evalHubName, testNamespace)))
			Expect(envVars["EVALHUB_INSTANCE_NAME"]).To(Equal(evalHubName))
		})

		It("should include custom environment variables", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub)
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
			err := reconciler.reconcileDeployment(ctx, evalHub)
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

		It("should configure health probes", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub)
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

			// Exec probes are used because the API listens on 127.0.0.1 only (security hardening).
			// HTTPGet probes from kubelet wouldn't reach localhost, so we use curl from inside the container.
			By("Checking liveness probe (exec-based)")
			Expect(evalHubContainer.LivenessProbe).NotTo(BeNil())
			Expect(evalHubContainer.LivenessProbe.Exec).NotTo(BeNil())
			Expect(evalHubContainer.LivenessProbe.Exec.Command).To(Equal([]string{
				"/usr/bin/curl", "--fail", "--silent", "--max-time", "3", "http://127.0.0.1:8080/api/v1/health",
			}))
			Expect(evalHubContainer.LivenessProbe.InitialDelaySeconds).To(Equal(int32(30)))
			Expect(evalHubContainer.LivenessProbe.PeriodSeconds).To(Equal(int32(10)))

			By("Checking readiness probe (exec-based)")
			Expect(evalHubContainer.ReadinessProbe).NotTo(BeNil())
			Expect(evalHubContainer.ReadinessProbe.Exec).NotTo(BeNil())
			Expect(evalHubContainer.ReadinessProbe.Exec.Command).To(Equal([]string{
				"/usr/bin/curl", "--fail", "--silent", "--max-time", "2", "http://127.0.0.1:8080/api/v1/health",
			}))
			Expect(evalHubContainer.ReadinessProbe.InitialDelaySeconds).To(Equal(int32(10)))
			Expect(evalHubContainer.ReadinessProbe.PeriodSeconds).To(Equal(int32(5)))
		})

		It("should configure security contexts", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub)
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
			err := reconciler.reconcileDeployment(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Modifying EvalHub replicas")
			newReplicas := int32(3)
			evalHub.Spec.Replicas = &newReplicas
			err = k8sClient.Update(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Reconciling deployment again")
			err = reconciler.reconcileDeployment(ctx, evalHub)
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

		It("should fail when config map is missing", func() {
			By("Deleting config map")
			err := k8sClient.Delete(ctx, configMap)
			Expect(err).NotTo(HaveOccurred())

			By("Reconciling deployment without config map")
			err = reconciler.reconcileDeployment(ctx, evalHub)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("kube-rbac-proxy configuration error"))
		})

		It("should configure rolling update strategy", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub)
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
			err := reconciler.reconcileDeployment(ctx, nonExistentEvalHub)
			Expect(err).To(HaveOccurred())
		})

		It("should fail when kube-rbac-proxy image is missing from ConfigMap", func() {
			By("Deleting the existing ConfigMap")
			err := k8sClient.Delete(ctx, configMap)
			Expect(err).NotTo(HaveOccurred())

			By("Creating ConfigMap without kube-rbac-proxy image")
			badConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: testNamespace,
				},
				Data: map[string]string{
					"evalHubImage": "quay.io/ruimvieira/eval-hub:test",
					// Missing kube-rbac-proxy key
				},
			}
			Expect(k8sClient.Create(ctx, badConfigMap)).Should(Succeed())

			evalHub := createEvalHubInstance(evalHubName, testNamespace)
			Expect(k8sClient.Create(ctx, evalHub)).Should(Succeed())

			By("Attempting to reconcile deployment")
			err = reconciler.reconcileDeployment(ctx, evalHub)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("kube-rbac-proxy configuration error"))

			// Cleanup
			k8sClient.Delete(ctx, evalHub)
			k8sClient.Delete(ctx, badConfigMap)
		})
	})

	Context("When configuring kube-rbac-proxy", func() {
		BeforeEach(func() {
			evalHub = createEvalHubInstance(evalHubName, testNamespace)
			Expect(k8sClient.Create(ctx, evalHub)).Should(Succeed())
		})

		It("should include kube-rbac-proxy sidecar container", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting deployment")
			deployment := waitForDeployment(evalHubName, testNamespace)

			By("Checking deployment has both containers")
			Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(2))

			By("Finding kube-rbac-proxy container")
			var proxyContainer *corev1.Container
			for _, container := range deployment.Spec.Template.Spec.Containers {
				if container.Name == "kube-rbac-proxy" {
					proxyContainer = &container
					break
				}
			}
			Expect(proxyContainer).NotTo(BeNil(), "kube-rbac-proxy container should be present")

			By("Checking kube-rbac-proxy container configuration")
			Expect(proxyContainer.Image).To(Equal("gcr.io/kubebuilder/kube-rbac-proxy:v0.13.1"))
			Expect(proxyContainer.Args).To(ContainElements(
				"--secure-listen-address=0.0.0.0:8443",
				"--upstream=http://127.0.0.1:8080",
				"--tls-cert-file=/etc/tls/private/tls.crt",
				"--tls-private-key-file=/etc/tls/private/tls.key",
				"--config-file=/etc/kube-rbac-proxy/config.yaml",
				"--logtostderr=true",
				"--v=0",
			))

			By("Checking kube-rbac-proxy ports")
			Expect(proxyContainer.Ports).To(HaveLen(1))
			Expect(proxyContainer.Ports[0].Name).To(Equal("https"))
			Expect(proxyContainer.Ports[0].ContainerPort).To(Equal(int32(8443)))
			Expect(proxyContainer.Ports[0].Protocol).To(Equal(corev1.ProtocolTCP))
		})

		It("should configure kube-rbac-proxy resource requirements", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting deployment")
			deployment := waitForDeployment(evalHubName, testNamespace)

			By("Finding kube-rbac-proxy container")
			var proxyContainer *corev1.Container
			for _, container := range deployment.Spec.Template.Spec.Containers {
				if container.Name == "kube-rbac-proxy" {
					proxyContainer = &container
					break
				}
			}
			Expect(proxyContainer).NotTo(BeNil())

			By("Checking resource requirements")
			Expect(proxyContainer.Resources.Limits).To(HaveKeyWithValue(
				corev1.ResourceCPU, resource.MustParse("200m"),
			))
			Expect(proxyContainer.Resources.Limits).To(HaveKeyWithValue(
				corev1.ResourceMemory, resource.MustParse("100Mi"),
			))
			Expect(proxyContainer.Resources.Requests).To(HaveKeyWithValue(
				corev1.ResourceCPU, resource.MustParse("100m"),
			))
			Expect(proxyContainer.Resources.Requests).To(HaveKeyWithValue(
				corev1.ResourceMemory, resource.MustParse("50Mi"),
			))
		})

		It("should configure kube-rbac-proxy security context", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting deployment")
			deployment := waitForDeployment(evalHubName, testNamespace)

			By("Finding kube-rbac-proxy container")
			var proxyContainer *corev1.Container
			for _, container := range deployment.Spec.Template.Spec.Containers {
				if container.Name == "kube-rbac-proxy" {
					proxyContainer = &container
					break
				}
			}
			Expect(proxyContainer).NotTo(BeNil())

			By("Checking security context")
			Expect(proxyContainer.SecurityContext).NotTo(BeNil())
			Expect(*proxyContainer.SecurityContext.AllowPrivilegeEscalation).To(BeFalse())
			Expect(*proxyContainer.SecurityContext.RunAsNonRoot).To(BeTrue())
			Expect(proxyContainer.SecurityContext.Capabilities.Drop).To(ContainElement(corev1.Capability("ALL")))
			Expect(proxyContainer.SecurityContext.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeRuntimeDefault))
		})

		It("should configure kube-rbac-proxy volume mounts", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting deployment")
			deployment := waitForDeployment(evalHubName, testNamespace)

			By("Finding kube-rbac-proxy container")
			var proxyContainer *corev1.Container
			for _, container := range deployment.Spec.Template.Spec.Containers {
				if container.Name == "kube-rbac-proxy" {
					proxyContainer = &container
					break
				}
			}
			Expect(proxyContainer).NotTo(BeNil())

			By("Checking volume mounts")
			Expect(proxyContainer.VolumeMounts).To(HaveLen(2))

			var configMount, tlsMount *corev1.VolumeMount
			for _, mount := range proxyContainer.VolumeMounts {
				if mount.Name == "kube-rbac-proxy-config" {
					configMount = &mount
				} else if mount.Name == evalHubName+"-tls" {
					tlsMount = &mount
				}
			}

			Expect(configMount).NotTo(BeNil())
			Expect(configMount.MountPath).To(Equal("/etc/kube-rbac-proxy"))
			Expect(configMount.ReadOnly).To(BeTrue())

			Expect(tlsMount).NotTo(BeNil())
			Expect(tlsMount.MountPath).To(Equal("/etc/tls/private"))
			Expect(tlsMount.ReadOnly).To(BeTrue())
		})

		It("should configure deployment volumes for proxy", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting deployment")
			deployment := waitForDeployment(evalHubName, testNamespace)

			By("Checking deployment volumes")
			Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(3))

			var evalHubConfigVolume, configVolume, tlsVolume *corev1.Volume
			for _, volume := range deployment.Spec.Template.Spec.Volumes {
				if volume.Name == "evalhub-config" {
					evalHubConfigVolume = &volume
				} else if volume.Name == "kube-rbac-proxy-config" {
					configVolume = &volume
				} else if volume.Name == evalHubName+"-tls" {
					tlsVolume = &volume
				}
			}

			By("Checking evalhub config volume")
			Expect(evalHubConfigVolume).NotTo(BeNil())
			Expect(evalHubConfigVolume.VolumeSource.ConfigMap).NotTo(BeNil())
			Expect(evalHubConfigVolume.VolumeSource.ConfigMap.Name).To(Equal(evalHubName + "-config"))

			By("Checking proxy config volume")
			Expect(configVolume).NotTo(BeNil())
			Expect(configVolume.VolumeSource.ConfigMap).NotTo(BeNil())
			Expect(configVolume.VolumeSource.ConfigMap.Name).To(Equal(evalHubName + "-proxy-config"))

			By("Checking TLS volume")
			Expect(tlsVolume).NotTo(BeNil())
			Expect(tlsVolume.VolumeSource.Secret).NotTo(BeNil())
			Expect(tlsVolume.VolumeSource.Secret.SecretName).To(Equal(evalHubName + "-tls"))
		})

		It("should configure service account for proxy", func() {
			By("Reconciling deployment")
			err := reconciler.reconcileDeployment(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting deployment")
			deployment := waitForDeployment(evalHubName, testNamespace)

			By("Checking service account name")
			Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(Equal(evalHubName + "-proxy"))
		})
	})
})

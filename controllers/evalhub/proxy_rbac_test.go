package evalhub

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"
)

var _ = Describe("EvalHub Proxy RBAC", func() {
	const (
		testNamespacePrefix     = "evalhub-proxy-rbac-test"
		operatorNamespacePrefix = "operator-system"
		evalHubName             = "proxy-rbac-evalhub"
		configMapName           = "trustyai-service-operator-config"
	)

	var (
		testNamespace     string
		operatorNamespace string
		namespace         *corev1.Namespace
		evalHub           *evalhubv1alpha1.EvalHub
		reconciler        *EvalHubReconciler
		operatorNS        *corev1.Namespace
		operatorCM        *corev1.ConfigMap
	)

	BeforeEach(func() {
		// Create unique namespace names to avoid conflicts
		timestamp := time.Now().UnixNano()
		testNamespace = fmt.Sprintf("%s-%d", testNamespacePrefix, timestamp)
		operatorNamespace = fmt.Sprintf("%s-%d", operatorNamespacePrefix, timestamp)

		// Create operator namespace for ConfigMap tests
		operatorNS = createNamespace(operatorNamespace)
		Expect(k8sClient.Create(ctx, operatorNS)).Should(Succeed())

		// Create operator ConfigMap with proxy image
		operatorCM = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: operatorNamespace,
			},
			Data: map[string]string{
				"kube-rbac-proxy": "gcr.io/kubebuilder/kube-rbac-proxy:v0.13.1",
				"evalHubImage":    "quay.io/ruimvieira/eval-hub:test",
			},
		}
		Expect(k8sClient.Create(ctx, operatorCM)).Should(Succeed())

		// Create test namespace
		namespace = createNamespace(testNamespace)
		Expect(k8sClient.Create(ctx, namespace)).Should(Succeed())

		// Create EvalHub instance
		evalHub = createEvalHubInstance(evalHubName, testNamespace)
		Expect(k8sClient.Create(ctx, evalHub)).Should(Succeed())

		// Setup reconciler with operator namespace
		reconciler, _ = setupReconciler(operatorNamespace)
	})

	AfterEach(func() {
		// Clean up resources in namespace first
		cleanupResourcesInNamespace(testNamespace, evalHub, nil)

		// Clean up operator namespace resources
		if operatorCM != nil {
			k8sClient.Delete(ctx, operatorCM)
		}

		// Delete namespaces
		deleteNamespace(namespace)
		deleteNamespace(operatorNS)

		// Reset variables
		evalHub = nil
		namespace = nil
		operatorNS = nil
		operatorCM = nil
	})

	Context("ServiceAccount Management", func() {
		It("should generate correct service account name", func() {
			By("Generating service account name")
			saName := generateServiceAccountName(evalHub)
			Expect(saName).To(Equal(evalHubName + "-proxy"))
		})

		It("should create service account with correct configuration", func() {
			By("Creating service account")
			err := reconciler.createServiceAccount(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying service account exists")
			serviceAccount := &corev1.ServiceAccount{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName + "-proxy",
				Namespace: testNamespace,
			}, serviceAccount)
			Expect(err).NotTo(HaveOccurred())

			By("Checking service account specifications")
			Expect(serviceAccount.Name).To(Equal(evalHubName + "-proxy"))
			Expect(serviceAccount.Namespace).To(Equal(testNamespace))

			By("Checking labels")
			Expect(serviceAccount.Labels["app"]).To(Equal("eval-hub"))
			Expect(serviceAccount.Labels["app.kubernetes.io/name"]).To(Equal(evalHubName + "-proxy"))
			Expect(serviceAccount.Labels["app.kubernetes.io/instance"]).To(Equal(evalHub.Name))
			Expect(serviceAccount.Labels["app.kubernetes.io/part-of"]).To(Equal("eval-hub"))

			By("Checking owner references")
			Expect(serviceAccount.OwnerReferences).To(HaveLen(1))
			Expect(serviceAccount.OwnerReferences[0].Name).To(Equal(evalHub.Name))
			Expect(serviceAccount.OwnerReferences[0].Kind).To(Equal("EvalHub"))
		})

		It("should create cluster role binding with correct configuration", func() {
			By("Creating service account (which also creates cluster role binding)")
			err := reconciler.createServiceAccount(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying cluster role binding exists")
			clusterRoleBinding := &rbacv1.ClusterRoleBinding{}
			bindingName := fmt.Sprintf("%s-%s-proxy-rolebinding", evalHub.Name, evalHub.Namespace)
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: bindingName,
			}, clusterRoleBinding)
			Expect(err).NotTo(HaveOccurred())

			By("Checking cluster role binding specifications")
			Expect(clusterRoleBinding.Name).To(Equal(bindingName))

			By("Checking subjects")
			Expect(clusterRoleBinding.Subjects).To(HaveLen(1))
			Expect(clusterRoleBinding.Subjects[0].Kind).To(Equal("ServiceAccount"))
			Expect(clusterRoleBinding.Subjects[0].Name).To(Equal(evalHubName + "-proxy"))
			Expect(clusterRoleBinding.Subjects[0].Namespace).To(Equal(testNamespace))

			By("Checking role reference")
			Expect(clusterRoleBinding.RoleRef.Kind).To(Equal("ClusterRole"))
			Expect(clusterRoleBinding.RoleRef.Name).To(Equal("trustyai-service-operator-evalhub-proxy-role"))
			Expect(clusterRoleBinding.RoleRef.APIGroup).To(Equal("rbac.authorization.k8s.io"))
		})

		It("should handle existing service account gracefully", func() {
			By("Creating service account initially")
			err := reconciler.createServiceAccount(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Creating service account again")
			err = reconciler.createServiceAccount(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying only one service account exists")
			serviceAccount := &corev1.ServiceAccount{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName + "-proxy",
				Namespace: testNamespace,
			}, serviceAccount)
			Expect(err).NotTo(HaveOccurred())
			Expect(serviceAccount.Name).To(Equal(evalHubName + "-proxy"))
		})
	})

	Context("ConfigMap Image Retrieval", func() {
		It("should retrieve kube-rbac-proxy image from operator ConfigMap", func() {
			By("Retrieving image from ConfigMap")
			image, err := reconciler.getImageFromConfigMap(ctx, "kube-rbac-proxy")
			Expect(err).NotTo(HaveOccurred())
			Expect(image).To(Equal("gcr.io/kubebuilder/kube-rbac-proxy:v0.13.1"))
		})

		It("should fail when ConfigMap is not found", func() {
			By("Creating reconciler with non-existent namespace")
			badReconciler := &EvalHubReconciler{
				Client:    k8sClient,
				Namespace: "non-existent-namespace",
			}

			By("Attempting to retrieve image")
			_, err := badReconciler.getImageFromConfigMap(ctx, "kube-rbac-proxy")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("required configmap"))
			Expect(err.Error()).To(ContainSubstring("not found"))
		})

		It("should fail when operator namespace is not set", func() {
			By("Creating reconciler with empty namespace")
			badReconciler := &EvalHubReconciler{
				Client:    k8sClient,
				Namespace: "",
			}

			By("Attempting to retrieve image")
			_, err := badReconciler.getImageFromConfigMap(ctx, "kube-rbac-proxy")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("operator namespace not set"))
		})

		It("should fail when required key is missing", func() {
			By("Attempting to retrieve non-existent key")
			_, err := reconciler.getImageFromConfigMap(ctx, "nonExistentImageKey")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("does not contain required key"))
		})

		It("should fail when key has empty value", func() {
			By("Updating ConfigMap with empty value")
			operatorCM.Data["emptyKey"] = ""
			err := k8sClient.Update(ctx, operatorCM)
			Expect(err).NotTo(HaveOccurred())

			By("Attempting to retrieve empty value")
			_, err = reconciler.getImageFromConfigMap(ctx, "emptyKey")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("contains empty value"))
		})

		It("should validate image configuration successfully", func() {
			By("Validating good image configuration")
			err := reconciler.validateImageConfiguration(ctx, "registry/image:tag", "test-image")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should warn about latest tag usage", func() {
			By("Validating image with latest tag")
			err := reconciler.validateImageConfiguration(ctx, "registry/image:latest", "test-image")
			Expect(err).NotTo(HaveOccurred()) // Should not error, just warn
		})

		It("should handle image without explicit tag", func() {
			By("Validating image without tag")
			err := reconciler.validateImageConfiguration(ctx, "registry/image", "test-image")
			Expect(err).NotTo(HaveOccurred()) // Should not error, just log
		})
	})

	Context("Proxy ConfigMap Management", func() {
		It("should create proxy config map with correct specifications", func() {
			By("Reconciling proxy configmap")
			err := reconciler.reconcileProxyConfigMap(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying proxy configmap exists")
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName + "-proxy-config",
				Namespace: testNamespace,
			}, configMap)
			Expect(err).NotTo(HaveOccurred())

			By("Checking configmap specifications")
			Expect(configMap.Name).To(Equal(evalHubName + "-proxy-config"))
			Expect(configMap.Namespace).To(Equal(testNamespace))

			By("Checking owner references")
			Expect(configMap.OwnerReferences).To(HaveLen(1))
			Expect(configMap.OwnerReferences[0].Name).To(Equal(evalHub.Name))
			Expect(configMap.OwnerReferences[0].Kind).To(Equal("EvalHub"))
		})

		It("should contain valid proxy configuration", func() {
			By("Reconciling proxy configmap")
			err := reconciler.reconcileProxyConfigMap(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting proxy configmap")
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName + "-proxy-config",
				Namespace: testNamespace,
			}, configMap)
			Expect(err).NotTo(HaveOccurred())

			By("Checking required keys exist")
			Expect(configMap.Data).To(HaveKey("config.yaml"))

			By("Parsing proxy configuration")
			var proxyConfig map[string]interface{}
			err = yaml.Unmarshal([]byte(configMap.Data["config.yaml"]), &proxyConfig)
			Expect(err).NotTo(HaveOccurred())

			By("Checking proxy configuration structure")
			Expect(proxyConfig).To(HaveKey("authorization"))
			Expect(proxyConfig).To(HaveKey("upstreams"))

			// Check authorization configuration
			authorization, ok := proxyConfig["authorization"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(authorization).To(HaveKey("resourceAttributes"))

			resourceAttrs, ok := authorization["resourceAttributes"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(resourceAttrs["namespace"]).To(Equal(testNamespace))
			Expect(resourceAttrs["apiVersion"]).To(Equal("trustyai.opendatahub.io/v1alpha1"))
			Expect(resourceAttrs["resource"]).To(Equal("evalhubs"))
			Expect(resourceAttrs["name"]).To(Equal(evalHubName))
			Expect(resourceAttrs["subresource"]).To(Equal("proxy"))

			// Check upstreams configuration
			upstreams, ok := proxyConfig["upstreams"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(upstreams).To(HaveLen(1))

			upstream, ok := upstreams[0].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(upstream["upstream"]).To(Equal("http://127.0.0.1:8000/"))
			Expect(upstream["path"]).To(Equal("/"))
			Expect(upstream["rewriteTarget"]).To(Equal("/"))

			// Check allowed paths
			allowedPaths, ok := upstream["allowedPaths"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(allowedPaths).To(ContainElements(
				"/api/v1/health", "/api/v1/providers", "/api/v1/benchmarks",
				"/api/v1/evaluations", "/api/v1/evaluations/jobs", "/api/v1/evaluations/jobs/*",
				"/api/v1/evaluations/*/status", "/api/v1/evaluations/*/results",
				"/openapi.json", "/docs", "/redoc",
			))
		})

		It("should update existing proxy configmap", func() {
			By("Creating initial proxy configmap")
			err := reconciler.reconcileProxyConfigMap(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting initial proxy configmap")
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName + "-proxy-config",
				Namespace: testNamespace,
			}, configMap)
			Expect(err).NotTo(HaveOccurred())

			By("Manually modifying proxy configmap data")
			configMap.Data["config.yaml"] = "bad: data"
			err = k8sClient.Update(ctx, configMap)
			Expect(err).NotTo(HaveOccurred())

			initialResourceVersion := configMap.ResourceVersion

			By("Reconciling proxy configmap again")
			err = reconciler.reconcileProxyConfigMap(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying proxy configmap is updated")
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName + "-proxy-config",
				Namespace: testNamespace,
			}, configMap)
			Expect(err).NotTo(HaveOccurred())

			Expect(configMap.ResourceVersion).NotTo(Equal(initialResourceVersion))
			Expect(configMap.Data["config.yaml"]).NotTo(Equal("bad: data"))
		})

		It("should generate proxy configuration data correctly", func() {
			By("Generating proxy configuration data")
			proxyConfigData := reconciler.generateProxyConfigData(evalHub)

			By("Checking required keys are present")
			Expect(proxyConfigData).To(HaveKey("config.yaml"))

			By("Validating proxy configuration content")
			var proxyConfig map[string]interface{}
			err := yaml.Unmarshal([]byte(proxyConfigData["config.yaml"]), &proxyConfig)
			Expect(err).NotTo(HaveOccurred())

			By("Checking configuration contains EvalHub-specific settings")
			authorization := proxyConfig["authorization"].(map[string]interface{})
			resourceAttrs := authorization["resourceAttributes"].(map[string]interface{})
			Expect(resourceAttrs["namespace"]).To(Equal(evalHub.Namespace))
			Expect(resourceAttrs["name"]).To(Equal(evalHub.Name))
		})
	})

	Context("Error Handling and Edge Cases", func() {
		It("should handle missing operator ConfigMap gracefully in getImageFromConfigMap", func() {
			By("Deleting operator ConfigMap")
			err := k8sClient.Delete(ctx, operatorCM)
			Expect(err).NotTo(HaveOccurred())

			By("Attempting to get image from non-existent ConfigMap")
			_, err = reconciler.getImageFromConfigMap(ctx, "kube-rbac-proxy")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})

		It("should handle proxy configmap creation in non-existent namespace", func() {
			By("Creating EvalHub in non-existent namespace")
			badEvalHub := createEvalHubInstance("bad-proxy-evalhub", "non-existent-namespace")

			By("Attempting to reconcile proxy configmap")
			badReconciler, _ := setupReconciler("non-existent-namespace")
			err := badReconciler.reconcileProxyConfigMap(ctx, badEvalHub)
			Expect(err).To(HaveOccurred())
		})

		It("should create proxy configmap even when EvalHub instance is not persisted", func() {
			By("Creating proxy configmap for non-persisted EvalHub")
			nonPersistedEvalHub := &evalhubv1alpha1.EvalHub{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "non-persisted-proxy",
					Namespace: testNamespace,
				},
			}

			By("Attempting to reconcile proxy configmap")
			err := reconciler.reconcileProxyConfigMap(ctx, nonPersistedEvalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying proxy configmap was created")
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "non-persisted-proxy-proxy-config",
				Namespace: testNamespace,
			}, configMap)
			Expect(err).NotTo(HaveOccurred())
			Expect(configMap.OwnerReferences).To(BeEmpty())
		})
	})
})

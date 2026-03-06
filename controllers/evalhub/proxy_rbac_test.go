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
)

var _ = Describe("EvalHub API RBAC", func() {
	const (
		testNamespacePrefix     = "evalhub-service-rbac-test"
		operatorNamespacePrefix = "operator-system"
		evalHubName             = "api-rbac-evalhub"
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

		// Create operator ConfigMap with EvalHub image
		operatorCM = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: operatorNamespace,
			},
			Data: map[string]string{
				"evalHubImage": "quay.io/ruimvieira/eval-hub:test",
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
		It("should generate correct service account name with -service suffix", func() {
			By("Generating service account name")
			saName := generateServiceAccountName(evalHub)
			Expect(saName).To(Equal(evalHubName + "-service"))
		})

		It("should create service account with correct configuration", func() {
			By("Creating service account")
			err := reconciler.createServiceAccount(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying service account exists with -service suffix")
			serviceAccount := &corev1.ServiceAccount{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName + "-service",
				Namespace: testNamespace,
			}, serviceAccount)
			Expect(err).NotTo(HaveOccurred())

			By("Checking service account specifications")
			Expect(serviceAccount.Name).To(Equal(evalHubName + "-service"))
			Expect(serviceAccount.Namespace).To(Equal(testNamespace))

			By("Checking labels")
			Expect(serviceAccount.Labels["app"]).To(Equal("eval-hub"))
			Expect(serviceAccount.Labels["app.kubernetes.io/name"]).To(Equal(evalHubName + "-service"))
			Expect(serviceAccount.Labels["app.kubernetes.io/instance"]).To(Equal(evalHub.Name))
			Expect(serviceAccount.Labels["app.kubernetes.io/part-of"]).To(Equal("eval-hub"))

			By("Checking owner references")
			Expect(serviceAccount.OwnerReferences).To(HaveLen(1))
			Expect(serviceAccount.OwnerReferences[0].Name).To(Equal(evalHub.Name))
			Expect(serviceAccount.OwnerReferences[0].Kind).To(Equal("EvalHub"))
		})

		It("should create auth reviewer ClusterRoleBinding (not full proxy role)", func() {
			By("Creating service account (which also creates auth reviewer ClusterRoleBinding)")
			err := reconciler.createServiceAccount(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying auth reviewer ClusterRoleBinding exists")
			clusterRoleBinding := &rbacv1.ClusterRoleBinding{}
			bindingName := fmt.Sprintf("%s-%s-auth-reviewer-crb", evalHub.Name, evalHub.Namespace)
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: bindingName,
			}, clusterRoleBinding)
			Expect(err).NotTo(HaveOccurred())

			By("Checking ClusterRoleBinding specifications")
			Expect(clusterRoleBinding.Name).To(Equal(bindingName))

			By("Checking subjects use -service SA")
			Expect(clusterRoleBinding.Subjects).To(HaveLen(1))
			Expect(clusterRoleBinding.Subjects[0].Kind).To(Equal("ServiceAccount"))
			Expect(clusterRoleBinding.Subjects[0].Name).To(Equal(evalHubName + "-service"))
			Expect(clusterRoleBinding.Subjects[0].Namespace).To(Equal(testNamespace))

			By("Checking role reference points to auth-reviewer (not proxy-role)")
			Expect(clusterRoleBinding.RoleRef.Kind).To(Equal("ClusterRole"))
			Expect(clusterRoleBinding.RoleRef.Name).To(Equal(authReviewerClusterRoleName))
			Expect(clusterRoleBinding.RoleRef.APIGroup).To(Equal("rbac.authorization.k8s.io"))
		})

		It("should create per-instance API access Role with resourceNames", func() {
			By("Creating service account")
			err := reconciler.createServiceAccount(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying per-instance Role exists")
			role := &rbacv1.Role{}
			roleName := generateAPIAccessRoleName(evalHub)
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      roleName,
				Namespace: testNamespace,
			}, role)
			Expect(err).NotTo(HaveOccurred())

			By("Checking Role has resourceNames set to instance name")
			Expect(role.Rules).To(HaveLen(2))
			Expect(role.Rules[0].ResourceNames).To(Equal([]string{evalHubName}))
			Expect(role.Rules[1].ResourceNames).To(Equal([]string{evalHubName}))
		})

		It("should create namespace-scoped API access RoleBinding referencing per-instance Role", func() {
			By("Creating service account")
			err := reconciler.createServiceAccount(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying API access RoleBinding exists in namespace")
			roleBinding := &rbacv1.RoleBinding{}
			rbName := evalHubName + "-service-access-rb"
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      rbName,
				Namespace: testNamespace,
			}, roleBinding)
			Expect(err).NotTo(HaveOccurred())

			By("Checking RoleBinding is namespace-scoped")
			Expect(roleBinding.Namespace).To(Equal(testNamespace))

			By("Checking subjects use -service SA")
			Expect(roleBinding.Subjects).To(HaveLen(1))
			Expect(roleBinding.Subjects[0].Kind).To(Equal("ServiceAccount"))
			Expect(roleBinding.Subjects[0].Name).To(Equal(evalHubName + "-service"))

			By("Checking role reference points to per-instance Role (not ClusterRole)")
			Expect(roleBinding.RoleRef.Kind).To(Equal("Role"))
			Expect(roleBinding.RoleRef.Name).To(Equal(generateAPIAccessRoleName(evalHub)))
		})

		It("should create namespace-scoped jobs API access RoleBinding referencing per-instance Role", func() {
			By("Creating service account")
			err := reconciler.createServiceAccount(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Creating job service account (which creates job Role and RoleBindings)")
			err = reconciler.createJobsServiceAccount(ctx, evalHub, testNamespace)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying jobs API access RoleBinding exists in namespace")
			roleBinding := &rbacv1.RoleBinding{}
			rbName := normalizeDNS1123LabelValue(evalHubName + "-" + testNamespace + "-job-access-rb")
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      rbName,
				Namespace: testNamespace,
			}, roleBinding)
			Expect(err).NotTo(HaveOccurred())

			By("Checking RoleBinding is namespace-scoped")
			Expect(roleBinding.Namespace).To(Equal(testNamespace))

			By("Checking subjects use -job SA")
			Expect(roleBinding.Subjects).To(HaveLen(1))
			Expect(roleBinding.Subjects[0].Kind).To(Equal("ServiceAccount"))
			Expect(roleBinding.Subjects[0].Name).To(Equal(generateJobsServiceAccountName(evalHub)))

			By("Checking role reference points to per-instance Role (not ClusterRole)")
			Expect(roleBinding.RoleRef.Kind).To(Equal("Role"))
			Expect(roleBinding.RoleRef.Name).To(Equal(generateJobsAPIAccessRoleName(evalHub)))

			By("Verifying job Role only grants status-events create, not evalhubs/proxy")
			jobRole := &rbacv1.Role{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      generateJobsAPIAccessRoleName(evalHub),
				Namespace: testNamespace,
			}, jobRole)
			Expect(err).NotTo(HaveOccurred())
			Expect(jobRole.Rules).To(HaveLen(1))
			Expect(jobRole.Rules[0].Resources).To(Equal([]string{"status-events"}))
			Expect(jobRole.Rules[0].Verbs).To(Equal([]string{"create"}))
			for _, rule := range jobRole.Rules {
				Expect(rule.Resources).NotTo(ContainElement("evalhubs/proxy"),
					"Job Role must not grant access to evalhubs/proxy")
			}
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
				Name:      evalHubName + "-service",
				Namespace: testNamespace,
			}, serviceAccount)
			Expect(err).NotTo(HaveOccurred())
			Expect(serviceAccount.Name).To(Equal(evalHubName + "-service"))
		})
	})

	Context("Split Resource Manager", func() {
		It("should create two separate RoleBindings for resource management", func() {
			By("Creating service account (which creates all RoleBindings)")
			err := reconciler.createServiceAccount(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying job-writer RoleBinding exists")
			jwRB := &rbacv1.RoleBinding{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName + "-job-writer-rb",
				Namespace: testNamespace,
			}, jwRB)
			Expect(err).NotTo(HaveOccurred())
			Expect(jwRB.RoleRef.Kind).To(Equal("ClusterRole"))
			Expect(jwRB.RoleRef.Name).To(Equal(jobsWriterClusterRoleName))
			Expect(jwRB.Subjects).To(HaveLen(1))
			Expect(jwRB.Subjects[0].Name).To(Equal(evalHubName + "-service"))

			By("Verifying job-config RoleBinding exists")
			jcRB := &rbacv1.RoleBinding{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName + "-job-config-rb",
				Namespace: testNamespace,
			}, jcRB)
			Expect(err).NotTo(HaveOccurred())
			Expect(jcRB.RoleRef.Kind).To(Equal("ClusterRole"))
			Expect(jcRB.RoleRef.Name).To(Equal(jobConfigClusterRoleName))
			Expect(jcRB.Subjects).To(HaveLen(1))
			Expect(jcRB.Subjects[0].Name).To(Equal(evalHubName + "-service"))

			By("Verifying providers-access RoleBinding exists")
			pRB := &rbacv1.RoleBinding{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName + "-providers-access-rb",
				Namespace: testNamespace,
			}, pRB)
			Expect(err).NotTo(HaveOccurred())
			Expect(pRB.RoleRef.Kind).To(Equal("ClusterRole"))
			Expect(pRB.RoleRef.Name).To(Equal(providersAccessClusterRoleName))
			Expect(pRB.Subjects).To(HaveLen(1))
			Expect(pRB.Subjects[0].Name).To(Equal(evalHubName + "-service"))

			By("Verifying collections-access RoleBinding exists")
			cRB := &rbacv1.RoleBinding{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName + "-collections-access-rb",
				Namespace: testNamespace,
			}, cRB)
			Expect(err).NotTo(HaveOccurred())
			Expect(cRB.RoleRef.Kind).To(Equal("ClusterRole"))
			Expect(cRB.RoleRef.Name).To(Equal(collectionsAccessClusterRoleName))
			Expect(cRB.Subjects).To(HaveLen(1))
			Expect(cRB.Subjects[0].Name).To(Equal(evalHubName + "-service"))
		})

		It("should not bind jobs SA to resource-manager roles", func() {
			By("Creating service account")
			err := reconciler.createServiceAccount(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Checking job-writer binding has only service SA")
			jwRB := &rbacv1.RoleBinding{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName + "-job-writer-rb",
				Namespace: testNamespace,
			}, jwRB)
			Expect(err).NotTo(HaveOccurred())
			for _, subject := range jwRB.Subjects {
				Expect(subject.Name).NotTo(Equal(evalHubName+"-job"),
					"Job SA should not be bound to resource-manager roles")
			}
		})
	})

	Context("ConfigMap Image Retrieval", func() {
		It("should retrieve EvalHub image from operator ConfigMap", func() {
			By("Retrieving image from ConfigMap")
			image, err := reconciler.getImageFromConfigMap(ctx, "evalHubImage")
			Expect(err).NotTo(HaveOccurred())
			Expect(image).To(Equal("quay.io/ruimvieira/eval-hub:test"))
		})

		It("should fail when ConfigMap is not found", func() {
			By("Creating reconciler with non-existent namespace")
			badReconciler := &EvalHubReconciler{
				Client:    k8sClient,
				Namespace: "non-existent-namespace",
			}

			By("Attempting to retrieve image")
			_, err := badReconciler.getImageFromConfigMap(ctx, "evalHubImage")
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
			_, err := badReconciler.getImageFromConfigMap(ctx, "evalHubImage")
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

	Context("Error Handling and Edge Cases", func() {
		It("should handle missing operator ConfigMap gracefully in getImageFromConfigMap", func() {
			By("Deleting operator ConfigMap")
			err := k8sClient.Delete(ctx, operatorCM)
			Expect(err).NotTo(HaveOccurred())

			By("Attempting to get image from non-existent ConfigMap")
			_, err = reconciler.getImageFromConfigMap(ctx, "evalHubImage")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})
	})

	Context("RBAC Namespace Scoping", func() {
		It("should not create cross-namespace ClusterRoleBinding for API access", func() {
			By("Creating service account and RBAC resources")
			err := reconciler.createServiceAccount(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying no legacy proxy ClusterRoleBinding exists")
			legacyCRB := &rbacv1.ClusterRoleBinding{}
			legacyBindingName := fmt.Sprintf("%s-%s-proxy-rolebinding", evalHub.Name, evalHub.Namespace)
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: legacyBindingName,
			}, legacyCRB)
			Expect(err).To(HaveOccurred(), "Legacy proxy ClusterRoleBinding should not exist")

			By("Verifying API access is via namespace-scoped RoleBinding referencing per-instance Role")
			apiRB := &rbacv1.RoleBinding{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName + "-service-access-rb",
				Namespace: testNamespace,
			}, apiRB)
			Expect(err).NotTo(HaveOccurred())
			Expect(apiRB.RoleRef.Kind).To(Equal("Role"))
			Expect(apiRB.RoleRef.Name).To(Equal(generateAPIAccessRoleName(evalHub)))
		})
	})
})

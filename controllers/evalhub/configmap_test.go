package evalhub

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"
)

var _ = Describe("EvalHub ConfigMap", func() {
	const (
		testNamespacePrefix = "evalhub-configmap-test"
		evalHubName         = "configmap-evalhub"
	)

	var (
		testNamespace string
		namespace     *corev1.Namespace
		evalHub       *evalhubv1alpha1.EvalHub
		reconciler    *EvalHubReconciler
	)

	BeforeEach(func() {
		// Create unique namespace name to avoid conflicts
		testNamespace = fmt.Sprintf("%s-%d", testNamespacePrefix, time.Now().UnixNano())

		// Create test namespace
		namespace = createNamespace(testNamespace)
		Expect(k8sClient.Create(ctx, namespace)).Should(Succeed())

		// Create EvalHub instance
		evalHub = createEvalHubInstance(evalHubName, testNamespace)
		Expect(k8sClient.Create(ctx, evalHub)).Should(Succeed())

		// Setup reconciler
		reconciler, _ = setupReconciler(testNamespace)
	})

	AfterEach(func() {
		// Clean up resources in namespace first
		cleanupResourcesInNamespace(testNamespace, evalHub, nil)

		// Then delete namespace
		deleteNamespace(namespace)

		// Reset variables
		evalHub = nil
		namespace = nil
	})

	Context("When reconciling configmap", func() {
		It("should create configmap with correct specifications", func() {
			By("Reconciling configmap")
			err := reconciler.reconcileConfigMap(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying configmap exists")
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName + "-config",
				Namespace: testNamespace,
			}, configMap)
			Expect(err).NotTo(HaveOccurred())

			By("Checking configmap specifications")
			Expect(configMap.Name).To(Equal(evalHubName + "-config"))
			Expect(configMap.Namespace).To(Equal(testNamespace))

			// Check owner reference
			Expect(configMap.OwnerReferences).To(HaveLen(1))
			Expect(configMap.OwnerReferences[0].Name).To(Equal(evalHub.Name))
			Expect(configMap.OwnerReferences[0].Kind).To(Equal("EvalHub"))
		})

		It("should contain required configuration files", func() {
			By("Reconciling configmap")
			err := reconciler.reconcileConfigMap(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting configmap")
			configMap := waitForConfigMap(evalHubName+"-config", testNamespace)

			By("Checking required keys exist")
			Expect(configMap.Data).To(HaveKey("config.yaml"))
		})

		It("should have valid YAML configuration", func() {
			By("Reconciling configmap")
			err := reconciler.reconcileConfigMap(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting configmap")
			configMap := waitForConfigMap(evalHubName+"-config", testNamespace)

			By("Parsing config.yaml")
			var config EvalHubConfig
			err = yaml.Unmarshal([]byte(configMap.Data["config.yaml"]), &config)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should update existing configmap", func() {
			By("Creating initial configmap")
			err := reconciler.reconcileConfigMap(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting initial configmap")
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName + "-config",
				Namespace: testNamespace,
			}, configMap)
			Expect(err).NotTo(HaveOccurred())

			By("Manually modifying configmap data")
			configMap.Data["config.yaml"] = "bad: data"
			err = k8sClient.Update(ctx, configMap)
			Expect(err).NotTo(HaveOccurred())

			// Store resource version after manual change
			initialResourceVersion := configMap.ResourceVersion

			By("Reconciling configmap again")
			err = reconciler.reconcileConfigMap(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying configmap is updated")
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName + "-config",
				Namespace: testNamespace,
			}, configMap)
			Expect(err).NotTo(HaveOccurred())

			// Resource version should change when configmap is updated
			Expect(configMap.ResourceVersion).NotTo(Equal(initialResourceVersion))
			Expect(configMap.Data["config.yaml"]).NotTo(Equal("bad: data"))
		})
	})

	Context("When handling edge cases and errors", func() {
		It("should create configmap even when EvalHub instance is not persisted", func() {
			By("Creating configmap for non-persisted EvalHub")
			nonPersistedEvalHub := &evalhubv1alpha1.EvalHub{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "non-persisted",
					Namespace: testNamespace,
				},
			}

			By("Attempting to reconcile configmap")
			err := reconciler.reconcileConfigMap(ctx, nonPersistedEvalHub)
			Expect(err).NotTo(HaveOccurred(), "reconcileConfigMap should succeed with non-persisted EvalHub as it only needs metadata")

			By("Verifying configmap was created")
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "non-persisted-config",
				Namespace: testNamespace,
			}, configMap)
			Expect(err).NotTo(HaveOccurred(), "configmap should be created successfully")
			Expect(configMap.OwnerReferences).To(BeEmpty())
		})

		It("should handle configmap creation in non-existent namespace", func() {
			By("Creating EvalHub in non-existent namespace")
			badEvalHub := createEvalHubInstance("bad-configmap-evalhub", "non-existent-namespace")

			By("Attempting to reconcile configmap")
			badReconciler, _ := setupReconciler("non-existent-namespace")
			err := badReconciler.reconcileConfigMap(ctx, badEvalHub)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("When database is configured", func() {
		It("should include database and secrets sections in config.yaml", func() {
			By("Creating EvalHub with database config")
			dbEvalHub := createEvalHubInstanceWithDB("db-configmap-evalhub", testNamespace, "evalhub-db-credentials")
			Expect(k8sClient.Create(ctx, dbEvalHub)).Should(Succeed())
			defer k8sClient.Delete(ctx, dbEvalHub)

			By("Reconciling configmap")
			err := reconciler.reconcileConfigMap(ctx, dbEvalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting configmap")
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "db-configmap-evalhub-config",
				Namespace: testNamespace,
			}, configMap)
			Expect(err).NotTo(HaveOccurred())

			By("Parsing config.yaml")
			var config EvalHubConfig
			err = yaml.Unmarshal([]byte(configMap.Data["config.yaml"]), &config)
			Expect(err).NotTo(HaveOccurred())

			By("Checking database section")
			Expect(config.Database).NotTo(BeNil())
			Expect(config.Database.Driver).To(Equal("pgx"))
			Expect(config.Database.MaxOpenConns).To(Equal(25))
			Expect(config.Database.MaxIdleConns).To(Equal(5))

			By("Checking secrets section")
			Expect(config.Secrets).NotTo(BeNil())
			Expect(config.Secrets.Dir).To(Equal("/etc/evalhub/secrets"))
			Expect(config.Secrets.Mappings).To(HaveKeyWithValue("db-url", "database.url"))
		})

		It("should default to sqlite when database is not explicitly configured", func() {
			By("Reconciling configmap for standard EvalHub (no DB)")
			err := reconciler.reconcileConfigMap(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting configmap")
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName + "-config",
				Namespace: testNamespace,
			}, configMap)
			Expect(err).NotTo(HaveOccurred())

			By("Parsing config.yaml")
			var config EvalHubConfig
			err = yaml.Unmarshal([]byte(configMap.Data["config.yaml"]), &config)
			Expect(err).NotTo(HaveOccurred())

			By("Checking default sqlite database is set and secrets are absent")
			Expect(config.Database).NotTo(BeNil())
			Expect(config.Database.Driver).To(Equal("sqlite"))
			Expect(config.Secrets).To(BeNil())
		})
	})

	Context("Configuration data generation", func() {
		It("should generate valid configuration data", func() {
			By("Generating configuration data")
			configData, err := reconciler.generateConfigData(evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Checking required keys are present")
			Expect(configData).To(HaveKey("config.yaml"))

			By("Validating config.yaml content")
			var config EvalHubConfig
			err = yaml.Unmarshal([]byte(configData["config.yaml"]), &config)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})

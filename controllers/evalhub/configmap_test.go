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
			Expect(configMap.Data).To(HaveKey("providers.yaml"))
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

			By("Checking default providers are present")
			Expect(config.Providers).To(HaveLen(4))

			var providerNames []string
			for _, provider := range config.Providers {
				providerNames = append(providerNames, provider.Name)
			}
			Expect(providerNames).To(ContainElements(
				"lm-eval-harness", "ragas-provider", "garak-security", "trustyai-custom",
			))

			By("Checking default collections are present")
			Expect(config.Collections).To(ContainElements(
				"healthcare_safety_v1", "automotive_safety_v1",
				"finance_compliance_v1", "general_llm_eval_v1",
			))
		})

		It("should have valid providers.yaml", func() {
			By("Reconciling configmap")
			err := reconciler.reconcileConfigMap(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting configmap")
			configMap := waitForConfigMap(evalHubName+"-config", testNamespace)

			By("Parsing providers.yaml")
			var providersData map[string]interface{}
			err = yaml.Unmarshal([]byte(configMap.Data["providers.yaml"]), &providersData)
			Expect(err).NotTo(HaveOccurred())

			By("Checking providers structure")
			Expect(providersData).To(HaveKey("providers"))
			providers, ok := providersData["providers"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(providers).To(HaveLen(4))
		})

		It("should configure lm-eval-harness provider correctly", func() {
			By("Reconciling configmap")
			err := reconciler.reconcileConfigMap(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting configmap")
			configMap := waitForConfigMap(evalHubName+"-config", testNamespace)

			By("Parsing config.yaml")
			var config EvalHubConfig
			err = yaml.Unmarshal([]byte(configMap.Data["config.yaml"]), &config)
			Expect(err).NotTo(HaveOccurred())

			By("Finding lm-eval-harness provider")
			var lmEvalProvider *ProviderConfig
			for _, provider := range config.Providers {
				if provider.Name == "lm-eval-harness" {
					lmEvalProvider = &provider
					break
				}
			}
			Expect(lmEvalProvider).NotTo(BeNil())

			By("Checking lm-eval-harness configuration")
			Expect(lmEvalProvider.Type).To(Equal("lm_evaluation_harness"))
			Expect(lmEvalProvider.Enabled).To(BeTrue())
			Expect(lmEvalProvider.Benchmarks).To(ContainElements(
				"arc_challenge", "hellaswag", "mmlu", "truthfulqa",
			))
			Expect(lmEvalProvider.Config["batch_size"]).To(Equal("8"))
			Expect(lmEvalProvider.Config["max_length"]).To(Equal("2048"))
		})

		It("should configure ragas provider correctly", func() {
			By("Reconciling configmap")
			err := reconciler.reconcileConfigMap(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting configmap")
			configMap := waitForConfigMap(evalHubName+"-config", testNamespace)

			By("Parsing config.yaml")
			var config EvalHubConfig
			err = yaml.Unmarshal([]byte(configMap.Data["config.yaml"]), &config)
			Expect(err).NotTo(HaveOccurred())

			By("Finding ragas provider")
			var ragasProvider *ProviderConfig
			for _, provider := range config.Providers {
				if provider.Name == "ragas-provider" {
					ragasProvider = &provider
					break
				}
			}
			Expect(ragasProvider).NotTo(BeNil())

			By("Checking ragas configuration")
			Expect(ragasProvider.Type).To(Equal("ragas"))
			Expect(ragasProvider.Enabled).To(BeTrue())
			Expect(ragasProvider.Benchmarks).To(ContainElements(
				"faithfulness", "answer_relevancy", "context_precision", "context_recall",
			))
			Expect(ragasProvider.Config["llm_model"]).To(Equal("gpt-3.5-turbo"))
			Expect(ragasProvider.Config["embeddings_model"]).To(Equal("text-embedding-ada-002"))
		})

		It("should configure garak security provider correctly", func() {
			By("Reconciling configmap")
			err := reconciler.reconcileConfigMap(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting configmap")
			configMap := waitForConfigMap(evalHubName+"-config", testNamespace)

			By("Parsing config.yaml")
			var config EvalHubConfig
			err = yaml.Unmarshal([]byte(configMap.Data["config.yaml"]), &config)
			Expect(err).NotTo(HaveOccurred())

			By("Finding garak provider")
			var garakProvider *ProviderConfig
			for _, provider := range config.Providers {
				if provider.Name == "garak-security" {
					garakProvider = &provider
					break
				}
			}
			Expect(garakProvider).NotTo(BeNil())

			By("Checking garak configuration")
			Expect(garakProvider.Type).To(Equal("garak"))
			Expect(garakProvider.Enabled).To(BeFalse()) // Disabled by default
			Expect(garakProvider.Benchmarks).To(ContainElements(
				"encoding", "injection", "malware", "prompt_injection",
			))
			Expect(garakProvider.Config["probe_set"]).To(Equal("basic"))
		})

		It("should configure trustyai custom provider correctly", func() {
			By("Reconciling configmap")
			err := reconciler.reconcileConfigMap(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Getting configmap")
			configMap := waitForConfigMap(evalHubName+"-config", testNamespace)

			By("Parsing config.yaml")
			var config EvalHubConfig
			err = yaml.Unmarshal([]byte(configMap.Data["config.yaml"]), &config)
			Expect(err).NotTo(HaveOccurred())

			By("Finding trustyai provider")
			var trustyaiProvider *ProviderConfig
			for _, provider := range config.Providers {
				if provider.Name == "trustyai-custom" {
					trustyaiProvider = &provider
					break
				}
			}
			Expect(trustyaiProvider).NotTo(BeNil())

			By("Checking trustyai configuration")
			Expect(trustyaiProvider.Type).To(Equal("trustyai_custom"))
			Expect(trustyaiProvider.Enabled).To(BeTrue())
			Expect(trustyaiProvider.Benchmarks).To(ContainElements(
				"bias_detection", "fairness_metrics",
			))
			Expect(trustyaiProvider.Config["bias_threshold"]).To(Equal("0.1"))
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

	Context("Configuration data generation", func() {
		It("should generate valid configuration data", func() {
			By("Generating configuration data")
			configData, err := reconciler.generateConfigData(evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Checking required keys are present")
			Expect(configData).To(HaveKey("config.yaml"))
			Expect(configData).To(HaveKey("providers.yaml"))

			By("Validating config.yaml content")
			var config EvalHubConfig
			err = yaml.Unmarshal([]byte(configData["config.yaml"]), &config)
			Expect(err).NotTo(HaveOccurred())
			Expect(config.Providers).To(HaveLen(4))
			Expect(config.Collections).To(HaveLen(4))

			By("Validating providers.yaml content")
			var providersData map[string]interface{}
			err = yaml.Unmarshal([]byte(configData["providers.yaml"]), &providersData)
			Expect(err).NotTo(HaveOccurred())
			Expect(providersData).To(HaveKey("providers"))
		})

		It("should generate providers YAML correctly", func() {
			By("Generating configuration data")
			configData, err := reconciler.generateConfigData(evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Parsing config.yaml to get providers")
			var config EvalHubConfig
			err = yaml.Unmarshal([]byte(configData["config.yaml"]), &config)
			Expect(err).NotTo(HaveOccurred())

			By("Generating providers YAML")
			providersYAML, err := reconciler.generateProvidersYAML(config.Providers)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying providers YAML matches configmap data")
			Expect(providersYAML).To(Equal(configData["providers.yaml"]))

			By("Verifying providers YAML is valid")
			var providersData map[string]interface{}
			err = yaml.Unmarshal([]byte(providersYAML), &providersData)
			Expect(err).NotTo(HaveOccurred())
			Expect(providersData).To(HaveKey("providers"))
		})
	})
})

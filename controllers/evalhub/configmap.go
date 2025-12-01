package evalhub

import (
	"context"
	"fmt"

	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

// ProviderConfig represents the provider configuration structure
type ProviderConfig struct {
	Name       string            `yaml:"name"`
	Type       string            `yaml:"type"`
	Enabled    bool              `yaml:"enabled"`
	Benchmarks []string          `yaml:"benchmarks,omitempty"`
	Config     map[string]string `yaml:"config,omitempty"`
}

// EvalHubConfig represents the eval-hub configuration structure
type EvalHubConfig struct {
	Providers   []ProviderConfig `yaml:"providers"`
	Collections []string         `yaml:"collections,omitempty"`
}

// reconcileConfigMap creates or updates the ConfigMap for EvalHub configuration
func (r *EvalHubReconciler) reconcileConfigMap(ctx context.Context, instance *evalhubv1alpha1.EvalHub) error {
	log := log.FromContext(ctx)
	log.Info("Reconciling ConfigMap", "name", instance.Name)

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name + "-config",
			Namespace: instance.Namespace,
		},
	}

	// Check if ConfigMap already exists
	getErr := r.Get(ctx, client.ObjectKeyFromObject(configMap), configMap)
	if getErr != nil && !errors.IsNotFound(getErr) {
		return getErr
	}

	// Generate configuration data
	configData, err := r.generateConfigData(instance)
	if err != nil {
		return fmt.Errorf("failed to generate config data: %w", err)
	}

	if errors.IsNotFound(getErr) {
		// Create new ConfigMap
		configMap.Data = configData
		if err := controllerutil.SetControllerReference(instance, configMap, r.Scheme); err != nil {
			return err
		}
		log.Info("Creating ConfigMap", "name", configMap.Name)
		return r.Create(ctx, configMap)
	} else {
		// Update existing ConfigMap
		configMap.Data = configData
		log.Info("Updating ConfigMap", "name", configMap.Name)
		return r.Update(ctx, configMap)
	}
}

// generateConfigData generates the configuration data for the ConfigMap
func (r *EvalHubReconciler) generateConfigData(instance *evalhubv1alpha1.EvalHub) (map[string]string, error) {
	config := EvalHubConfig{
		Providers:   make([]ProviderConfig, 0),
		Collections: []string{},
	}

	// Default providers configuration set by the controller
	config.Providers = []ProviderConfig{
		{
			Name:    "lm-eval-harness",
			Type:    "lm_evaluation_harness",
			Enabled: true,
			Benchmarks: []string{
				"arc_challenge", "hellaswag", "mmlu", "truthfulqa",
			},
			Config: map[string]string{
				"batch_size": "8",
				"max_length": "2048",
			},
		},
		{
			Name:    "ragas-provider",
			Type:    "ragas",
			Enabled: true,
			Benchmarks: []string{
				"faithfulness", "answer_relevancy", "context_precision", "context_recall",
			},
			Config: map[string]string{
				"llm_model":        "gpt-3.5-turbo",
				"embeddings_model": "text-embedding-ada-002",
			},
		},
		{
			Name:    "garak-security",
			Type:    "garak",
			Enabled: false,
			Benchmarks: []string{
				"encoding", "injection", "malware", "prompt_injection",
			},
			Config: map[string]string{
				"probe_set": "basic",
			},
		},
		{
			Name:    "trustyai-custom",
			Type:    "trustyai_custom",
			Enabled: true,
			Benchmarks: []string{
				"bias_detection", "fairness_metrics",
			},
			Config: map[string]string{
				"bias_threshold": "0.1",
			},
		},
	}

	// Default collections
	config.Collections = []string{
		"healthcare_safety_v1",
		"automotive_safety_v1",
		"finance_compliance_v1",
		"general_llm_eval_v1",
	}

	// Convert to YAML
	configYAML, err := yaml.Marshal(config)
	if err != nil {
		return nil, err
	}

	// Generate providers.yaml content
	providersYAML, err := r.generateProvidersYAML(config.Providers)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"config.yaml":    string(configYAML),
		"providers.yaml": providersYAML,
	}, nil
}

// generateProvidersYAML generates the providers.yaml configuration
func (r *EvalHubReconciler) generateProvidersYAML(providers []ProviderConfig) (string, error) {
	providersData := make(map[string]interface{})
	providersData["providers"] = providers

	yamlData, err := yaml.Marshal(providersData)
	if err != nil {
		return "", err
	}

	return string(yamlData), nil
}

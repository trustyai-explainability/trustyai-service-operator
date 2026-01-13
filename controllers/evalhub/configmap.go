package evalhub

import (
	"context"
	"fmt"
	"strings"

	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
		if instance.UID != "" {
			if err := controllerutil.SetControllerReference(instance, configMap, r.Scheme); err != nil {
				return err
			}
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

// getImageFromConfigMap gets a required image value from the operator's ConfigMap
// Returns error if ConfigMap is not found, key is missing, or value is empty
// This ensures explicit configuration and prevents deployment with unconfigured images
func (r *EvalHubReconciler) getImageFromConfigMap(ctx context.Context, key string) (string, error) {
	log := log.FromContext(ctx)

	if r.Namespace == "" {
		return "", fmt.Errorf("operator namespace not set, cannot retrieve image configuration")
	}

	// Define the key for the ConfigMap
	configMapKey := types.NamespacedName{
		Namespace: r.Namespace,
		Name:      configMapName,
	}

	// Create an empty ConfigMap object
	var cm corev1.ConfigMap

	// Try to get the ConfigMap
	err := r.Client.Get(ctx, configMapKey, &cm)
	if err != nil {
		if errors.IsNotFound(err) {
			// ConfigMap not found - FAIL deployment with clear error
			return "", fmt.Errorf("required configmap '%s' not found in namespace '%s' - operator configuration missing",
				configMapName, r.Namespace)
		}
		// Other error occurred when trying to fetch the ConfigMap
		return "", fmt.Errorf("error reading configmap '%s' in namespace '%s': %w",
			configMapName, r.Namespace, err)
	}

	log.V(1).Info("Found ConfigMap", "configmap", configMapKey)

	// ConfigMap is found, extract the image
	image, ok := cm.Data[key]
	if !ok {
		// Key not present in the ConfigMap - FAIL deployment
		availableKeys := make([]string, 0, len(cm.Data))
		for k := range cm.Data {
			availableKeys = append(availableKeys, k)
		}
		return "", fmt.Errorf("configmap '%s' does not contain required key '%s' (available keys: %v)",
			configMapKey, key, availableKeys)
	}

	if strings.TrimSpace(image) == "" {
		// Key present but empty - FAIL deployment
		return "", fmt.Errorf("configmap '%s' contains empty value for key '%s' - image must be explicitly configured",
			configMapKey, key)
	}

	log.Info("Successfully retrieved image from ConfigMap",
		"configmap", configMapKey,
		"key", key,
		"image", image)

	return image, nil
}

// validateImageConfiguration validates that the image string is properly formatted
func (r *EvalHubReconciler) validateImageConfiguration(ctx context.Context, image, imageType string) error {
	log := log.FromContext(ctx)

	// Basic validation - ensure image has a registry/repo and tag
	if !strings.Contains(image, ":") {
		log.V(1).Info("Image missing explicit tag, may use 'latest' implicitly", "image", image, "type", imageType)
	}

	// Check for potentially problematic configurations
	if strings.HasSuffix(image, ":latest") {
		log.Info("Warning: using 'latest' tag for container image", "image", image, "type", imageType)
	}

	return nil
}

// reconcileProxyConfigMap creates or updates the ConfigMap for kube-rbac-proxy configuration
func (r *EvalHubReconciler) reconcileProxyConfigMap(ctx context.Context, instance *evalhubv1alpha1.EvalHub) error {
	log := log.FromContext(ctx)
	log.Info("Reconciling Proxy ConfigMap", "name", instance.Name)

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name + "-proxy-config",
			Namespace: instance.Namespace,
		},
	}

	// Check if ConfigMap already exists
	getErr := r.Get(ctx, client.ObjectKeyFromObject(configMap), configMap)
	if getErr != nil && !errors.IsNotFound(getErr) {
		return getErr
	}

	// Generate proxy configuration data
	proxyConfigData := r.generateProxyConfigData(instance)

	if errors.IsNotFound(getErr) {
		// Create new ConfigMap
		configMap.Data = proxyConfigData
		if instance.UID != "" {
			if err := controllerutil.SetControllerReference(instance, configMap, r.Scheme); err != nil {
				return err
			}
		}
		log.Info("Creating Proxy ConfigMap", "name", configMap.Name)
		return r.Create(ctx, configMap)
	} else {
		// Update existing ConfigMap
		configMap.Data = proxyConfigData
		log.Info("Updating Proxy ConfigMap", "name", configMap.Name)
		return r.Update(ctx, configMap)
	}
}

// generateProxyConfigData generates the kube-rbac-proxy configuration data
func (r *EvalHubReconciler) generateProxyConfigData(instance *evalhubv1alpha1.EvalHub) map[string]string {
	// kube-rbac-proxy configuration for EvalHub using proper YAML marshaling
	proxyConfig := map[string]interface{}{
		"authorization": map[string]interface{}{
			"resourceAttributes": map[string]interface{}{
				"namespace":   instance.Namespace,
				"apiVersion":  "trustyai.opendatahub.io/v1alpha1",
				"resource":    "evalhubs",
				"name":        instance.Name,
				"subresource": "proxy",
			},
		},
		"upstreams": []map[string]interface{}{
			{
				"upstream":      "http://127.0.0.1:8000/",
				"path":          "/",
				"rewriteTarget": "/",
				"allowedPaths": []string{
					"/api/v1/health",
					"/api/v1/providers",
					"/api/v1/benchmarks",
					"/api/v1/evaluations",
					"/api/v1/evaluations/jobs",
					"/api/v1/evaluations/jobs/*",
					"/api/v1/evaluations/*/status",
					"/api/v1/evaluations/*/results",
					"/openapi.json",
					"/docs",
					"/redoc",
				},
			},
		},
	}

	yamlData, err := yaml.Marshal(proxyConfig)
	if err != nil {
		// This should never happen with our static config, but handle gracefully
		log.Log.Error(err, "Failed to marshal proxy configuration to YAML")
		return map[string]string{"config.yaml": "# Error: Failed to generate configuration"}
	}

	return map[string]string{
		"config.yaml": string(yamlData),
	}
}

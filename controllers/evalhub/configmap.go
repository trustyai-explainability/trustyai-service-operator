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

// DatabaseConfig represents the database configuration in config.yaml
type DatabaseConfig struct {
	Driver       string `yaml:"driver"`
	MaxOpenConns int    `yaml:"max_open_conns,omitempty"`
	MaxIdleConns int    `yaml:"max_idle_conns,omitempty"`
}

// SecretsMapping represents the secrets mapping configuration in config.yaml
type SecretsMapping struct {
	Dir      string            `yaml:"dir"`
	Mappings map[string]string `yaml:"mappings"`
}

// EvalHubConfig represents the eval-hub configuration structure
type EvalHubConfig struct {
	Providers   []ProviderResource `yaml:"providers"`
	Collections []string           `yaml:"collections,omitempty"`
	Database    *DatabaseConfig    `yaml:"database,omitempty"`
	Secrets     *SecretsMapping    `yaml:"secrets,omitempty"`
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

// buildEvalHubConfig builds the full EvalHubConfig for an instance.
func (r *EvalHubReconciler) buildEvalHubConfig(instance *evalhubv1alpha1.EvalHub) EvalHubConfig {
	config := EvalHubConfig{
		Providers: []ProviderResource{
			{
				ID:   "lm-eval-harness",
				Name: "lm-eval-harness",
				Type: "lm_evaluation_harness",
				Benchmarks: []BenchmarkResource{
					{ID: "arc_challenge", Name: "arc_challenge"},
					{ID: "hellaswag", Name: "hellaswag"},
					{ID: "mmlu", Name: "mmlu"},
					{ID: "truthfulqa", Name: "truthfulqa"},
				},
			},
			{
				ID:   "ragas-provider",
				Name: "ragas-provider",
				Type: "ragas",
				Benchmarks: []BenchmarkResource{
					{ID: "faithfulness", Name: "faithfulness"},
					{ID: "answer_relevancy", Name: "answer_relevancy"},
					{ID: "context_precision", Name: "context_precision"},
					{ID: "context_recall", Name: "context_recall"},
				},
			},
			{
				ID:   "garak-security",
				Name: "garak-security",
				Type: "garak",
				Benchmarks: []BenchmarkResource{
					{ID: "encoding", Name: "encoding"},
					{ID: "injection", Name: "injection"},
					{ID: "malware", Name: "malware"},
					{ID: "prompt_injection", Name: "prompt_injection"},
				},
			},
			{
				ID:   "trustyai-custom",
				Name: "trustyai-custom",
				Type: "trustyai_custom",
				Benchmarks: []BenchmarkResource{
					{ID: "bias_detection", Name: "bias_detection"},
					{ID: "fairness_metrics", Name: "fairness_metrics"},
				},
			},
		},
		Collections: []string{
			"healthcare_safety_v1",
			"automotive_safety_v1",
			"finance_compliance_v1",
			"general_llm_eval_v1",
		},
	}

	// Conditionally add database configuration
	if instance.Spec.IsDatabaseConfigured() {
		maxOpen, maxIdle := dbDefaultMaxOpen, dbDefaultMaxIdle
		if instance.Spec.Database.MaxOpenConns > 0 {
			maxOpen = instance.Spec.Database.MaxOpenConns
		}
		if instance.Spec.Database.MaxIdleConns > 0 {
			maxIdle = instance.Spec.Database.MaxIdleConns
		}
		config.Database = &DatabaseConfig{
			Driver:       dbDriver,
			MaxOpenConns: maxOpen,
			MaxIdleConns: maxIdle,
		}
		config.Secrets = &SecretsMapping{
			Dir:      dbSecretMountPath,
			Mappings: map[string]string{dbSecretKey: "database.url"},
		}
	}

	return config
}

// generateConfigData generates the configuration data for the main ConfigMap.
func (r *EvalHubReconciler) generateConfigData(instance *evalhubv1alpha1.EvalHub) (map[string]string, error) {
	config := r.buildEvalHubConfig(instance)

	configYAML, err := yaml.Marshal(config)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"config.yaml": string(configYAML),
	}, nil
}

// generateProvidersData generates per-provider YAML entries for the providers ConfigMap.
// Each provider becomes a separate key like "lm-eval-harness.yaml".
func (r *EvalHubReconciler) generateProvidersData(providers []ProviderResource) (map[string]string, error) {
	data := make(map[string]string, len(providers))
	for _, provider := range providers {
		providerYAML, err := yaml.Marshal(provider)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal provider %s: %w", provider.Name, err)
		}
		data[fmt.Sprintf("%s.yaml", provider.Name)] = string(providerYAML)
	}
	return data, nil
}

// reconcileProvidersConfigMap creates or updates the providers ConfigMap.
// Each provider gets its own key (e.g. "lm-eval-harness.yaml") so that when mounted
// at /etc/evalhub/providers/ each becomes a separate file.
func (r *EvalHubReconciler) reconcileProvidersConfigMap(ctx context.Context, instance *evalhubv1alpha1.EvalHub) error {
	log := log.FromContext(ctx)
	log.Info("Reconciling Providers ConfigMap", "name", instance.Name)

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name + "-providers",
			Namespace: instance.Namespace,
		},
	}

	getErr := r.Get(ctx, client.ObjectKeyFromObject(configMap), configMap)
	if getErr != nil && !errors.IsNotFound(getErr) {
		return getErr
	}

	config := r.buildEvalHubConfig(instance)
	providersData, err := r.generateProvidersData(config.Providers)
	if err != nil {
		return fmt.Errorf("failed to generate providers data: %w", err)
	}

	if errors.IsNotFound(getErr) {
		configMap.Data = providersData
		if instance.UID != "" {
			if err := controllerutil.SetControllerReference(instance, configMap, r.Scheme); err != nil {
				return err
			}
		}
		log.Info("Creating Providers ConfigMap", "name", configMap.Name)
		return r.Create(ctx, configMap)
	}

	configMap.Data = providersData
	log.Info("Updating Providers ConfigMap", "name", configMap.Name)
	return r.Update(ctx, configMap)
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
				"namespace":    instance.Namespace,
				"apiGroup":     "trustyai.opendatahub.io",
				"resource":     "evalhubs",
				"resourceName": instance.Name,
				"subresource":  "proxy",
			},
		},
		"upstreams": []map[string]interface{}{
			{
				"upstream":      "http://127.0.0.1:8080/",
				"path":          "/",
				"rewriteTarget": "/",
				"allowedPaths": []string{
					"/api/v1/health",
					"/api/v1/providers",
					"/api/v1/benchmarks",
					"/api/v1/evaluations",
					"/api/v1/evaluations/",
					"/api/v1/evaluations/jobs",
					"/api/v1/evaluations/jobs/",
					"/api/v1/evaluations/jobs/*",
					"/api/v1/evaluations/jobs/*/events",
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

// reconcileServiceCAConfigMap creates or updates the ConfigMap for service CA certificate injection
// This ConfigMap is used by jobs to mount the service CA certificate for TLS verification
func (r *EvalHubReconciler) reconcileServiceCAConfigMap(ctx context.Context, instance *evalhubv1alpha1.EvalHub) error {
	log := log.FromContext(ctx)
	log.Info("Reconciling Service CA ConfigMap", "name", instance.Name)

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name + "-service-ca",
			Namespace: instance.Namespace,
			Annotations: map[string]string{
				// This annotation triggers OpenShift service CA operator to inject the CA certificate
				"service.beta.openshift.io/inject-cabundle": "true",
			},
		},
	}

	// Check if ConfigMap already exists
	getErr := r.Get(ctx, client.ObjectKeyFromObject(configMap), configMap)
	if getErr != nil && !errors.IsNotFound(getErr) {
		return getErr
	}

	if errors.IsNotFound(getErr) {
		// Create new ConfigMap
		// Note: We don't set any data - OpenShift service CA operator will inject service-ca.crt
		configMap.Data = map[string]string{}
		if instance.UID != "" {
			if err := controllerutil.SetControllerReference(instance, configMap, r.Scheme); err != nil {
				return err
			}
		}
		log.Info("Creating Service CA ConfigMap", "name", configMap.Name)
		return r.Create(ctx, configMap)
	} else {
		// Update existing ConfigMap to ensure annotation is present
		if configMap.Annotations == nil {
			configMap.Annotations = make(map[string]string)
		}
		configMap.Annotations["service.beta.openshift.io/inject-cabundle"] = "true"
		log.Info("Updating Service CA ConfigMap", "name", configMap.Name)
		return r.Update(ctx, configMap)
	}
}

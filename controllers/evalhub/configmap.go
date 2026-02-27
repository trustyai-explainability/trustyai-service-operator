package evalhub

import (
	"context"
	"fmt"
	"strconv"
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

// ServiceConfig represents the service section in config.yaml
type ServiceConfig struct {
	Port            int    `json:"port"`
	ReadyFile       string `json:"ready_file"`
	TerminationFile string `json:"termination_file"`
}

// DatabaseConfig represents the database configuration in config.yaml
type DatabaseConfig struct {
	Driver       string `json:"driver"`
	URL          string `json:"url,omitempty"`
	MaxOpenConns int    `json:"max_open_conns,omitempty"`
	MaxIdleConns int    `json:"max_idle_conns,omitempty"`
}

// OTELConfig represents the OpenTelemetry configuration in config.yaml
type OTELConfig struct {
	Enabled          bool     `json:"enabled"`
	ExporterType     string   `json:"exporter_type,omitempty"`
	ExporterEndpoint string   `json:"exporter_endpoint,omitempty"`
	ExporterInsecure bool     `json:"exporter_insecure,omitempty"`
	SamplingRatio    *float64 `json:"sampling_ratio,omitempty"`
	EnableTracing    bool     `json:"enable_tracing,omitempty"`
	EnableMetrics    bool     `json:"enable_metrics,omitempty"`
	EnableLogs       bool     `json:"enable_logs,omitempty"`
}

// SecretsMapping represents the secrets mapping configuration in config.yaml
type SecretsMapping struct {
	Dir      string            `json:"dir"`
	Mappings map[string]string `json:"mappings"`
}

// EnvMappings maps environment variable names to config field paths
type EnvMappings map[string]string

// EvalHubConfig represents the eval-hub configuration structure
type EvalHubConfig struct {
	Service     ServiceConfig   `json:"service"`
	Secrets     *SecretsMapping `json:"secrets,omitempty"`
	EnvMappings EnvMappings     `json:"env_mappings"`
	Database    *DatabaseConfig `json:"database"`
	OTEL        *OTELConfig     `json:"otel,omitempty"`
	Prometheus  map[string]any  `json:"prometheus,omitempty"`
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
		Service: ServiceConfig{
			Port:            containerPort,
			ReadyFile:       "/tmp/repo-ready",
			TerminationFile: "/tmp/termination-log",
		},
		EnvMappings: EnvMappings{
			"PORT":                        "service.port",
			"DB_URL":                      "database.url",
			"MLFLOW_TRACKING_URI":         "mlflow.tracking_uri",
			"MLFLOW_CA_CERT_PATH":         "mlflow.ca_cert_path",
			"MLFLOW_INSECURE_SKIP_VERIFY": "mlflow.insecure_skip_verify",
			"MLFLOW_TOKEN_PATH":           "mlflow.token_path",
			"MLFLOW_WORKSPACE":            "mlflow.workspace",
		},
		Database: &DatabaseConfig{
			Driver: "sqlite",
			URL:    "file::eval_hub:?mode=memory&cache=shared",
		},
		Prometheus: map[string]any{
			"enabled": true,
		},
	}

	// Override database configuration when explicitly configured
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

	// Override OTEL configuration when explicitly configured
	if instance.Spec.IsOTELConfigured() {
		otelCfg := &OTELConfig{
			Enabled:          true,
			ExporterType:     instance.Spec.Otel.ExporterType,
			ExporterEndpoint: instance.Spec.Otel.ExporterEndpoint,
			ExporterInsecure: instance.Spec.Otel.ExporterInsecure,
			EnableTracing:    instance.Spec.Otel.EnableTracing,
			EnableMetrics:    instance.Spec.Otel.EnableMetrics,
			EnableLogs:       instance.Spec.Otel.EnableLogs,
		}
		if instance.Spec.Otel.SamplingRatio != "" {
			ratio, err := strconv.ParseFloat(instance.Spec.Otel.SamplingRatio, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid samplingRatio %q: %w", instance.Spec.Otel.SamplingRatio, err)
			}
			otelCfg.SamplingRatio = &ratio
		}
		config.OTEL = otelCfg
	}

	// Convert to YAML
	configYAML, err := yaml.Marshal(config)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"config.yaml": string(configYAML),
	}, nil
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
				"namespace": instance.Namespace,
				"apiGroup":  "trustyai.opendatahub.io",
				"resource":  "evalhubs",
				// kube-rbac-proxy expects the Kubernetes ResourceAttributes key "name".
				// Keep "resourceName" for compatibility with older config consumers.
				"name":         instance.Name,
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

// reconcileProviderConfigMaps copies provider ConfigMaps from the operator namespace to the
// EvalHub CR's namespace. Only providers listed in instance.Spec.Providers are copied.
// Each source ConfigMap is discovered by the labels:
//   - trustyai.opendatahub.io/evalhub-provider-type=system
//   - trustyai.opendatahub.io/evalhub-provider-name=<name>
//
// Returns the list of created ConfigMap names (for building projected volumes).
func (r *EvalHubReconciler) reconcileProviderConfigMaps(ctx context.Context, instance *evalhubv1alpha1.EvalHub) ([]string, error) {
	if len(instance.Spec.Providers) == 0 {
		return nil, nil
	}

	log := log.FromContext(ctx)
	log.Info("Reconciling Provider ConfigMaps", "instance", instance.Name, "providers", instance.Spec.Providers)

	var cmNames []string
	for _, providerName := range instance.Spec.Providers {
		// Look up the source ConfigMap by both labels
		var sourceList corev1.ConfigMapList
		if err := r.List(ctx, &sourceList,
			client.InNamespace(r.Namespace),
			client.MatchingLabels{
				providerLabel:     "system",
				providerNameLabel: providerName,
			}); err != nil {
			return nil, fmt.Errorf("failed to list provider ConfigMaps for %q in namespace %s: %w", providerName, r.Namespace, err)
		}
		if len(sourceList.Items) == 0 {
			return nil, fmt.Errorf("provider %q not found: no ConfigMap with label %s=%s in namespace %s",
				providerName, providerNameLabel, providerName, r.Namespace)
		}

		src := &sourceList.Items[0]
		targetName := instance.Name + "-provider-" + providerName

		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      targetName,
				Namespace: instance.Namespace,
			},
		}

		// Check if ConfigMap already exists
		getErr := r.Get(ctx, client.ObjectKeyFromObject(configMap), configMap)
		if getErr != nil && !errors.IsNotFound(getErr) {
			return nil, getErr
		}

		if errors.IsNotFound(getErr) {
			configMap.Data = src.Data
			if instance.UID != "" {
				if err := controllerutil.SetControllerReference(instance, configMap, r.Scheme); err != nil {
					return nil, err
				}
			}
			log.Info("Creating Provider ConfigMap", "name", targetName, "provider", providerName)
			if err := r.Create(ctx, configMap); err != nil {
				return nil, err
			}
		} else {
			configMap.Data = src.Data
			log.Info("Updating Provider ConfigMap", "name", targetName, "provider", providerName)
			if err := r.Update(ctx, configMap); err != nil {
				return nil, err
			}
		}

		cmNames = append(cmNames, targetName)
	}

	return cmNames, nil
}

// providerVolumeProjections builds VolumeProjection entries for mounting provider ConfigMaps
// into a single projected volume.
func providerVolumeProjections(cmNames []string) []corev1.VolumeProjection {
	var projections []corev1.VolumeProjection
	for _, name := range cmNames {
		projections = append(projections, corev1.VolumeProjection{
			ConfigMap: &corev1.ConfigMapProjection{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: name,
				},
			},
		})
	}
	return projections
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

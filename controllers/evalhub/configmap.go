package evalhub

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	evalhubv1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
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
	Port             int    `json:"port"`
	Host             string `json:"host,omitempty"`
	DisableAuth      bool   `json:"disable_auth,omitempty"`
	ReadyFile        string `json:"ready_file"`
	TerminationFile  string `json:"termination_file"`
	EvalInitImage    string `json:"eval_init_image,omitempty"`
	EvalSidecarImage string `json:"eval_sidecar_image,omitempty"`
	TLSCertFile      string `json:"tls_cert_file,omitempty"`
	TLSKeyFile       string `json:"tls_key_file,omitempty"`
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

// EvalHubClientConfig represents the eval-hub client configuration in config.yaml
type EvalHubClientConfig struct {
	HTTPTimeout        time.Duration `json:"http_timeout"`
	CACertPath         string        `json:"ca_cert_path,omitempty"`
	InsecureSkipVerify bool          `json:"insecure_skip_verify,omitempty"`
}

// SidecarContainerConfig represents the sidecar container configuration in config.yaml
type SidecarContainerConfig struct {
	Image     string                `json:"image,omitempty"`
	Resources *ResourceRequirements `json:"resources,omitempty"`
}

// ResourceRequirements represents the resource requirements for the sidecar container in config.yaml
type ResourceRequirements struct {
	Requests *ResourceRequirementDef `json:"requests,omitempty"`
	Limits   *ResourceRequirementDef `json:"limits,omitempty"`
}

// ResourceRequirementDef represents the resource requirement definition for the sidecar container in config.yaml
type ResourceRequirementDef struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
}

// SidecarConfig represents the sidecar configuration in config.yaml
type SidecarConfig struct {
	BaseURL          string                  `json:"base_url,omitempty"`
	EvalHub          *EvalHubClientConfig    `json:"eval_hub"`
	SidecarContainer *SidecarContainerConfig `json:"sidecar_container,omitempty"`
}

// EvalHubConfig represents the eval-hub configuration structure
type EvalHubConfig struct {
	Service     ServiceConfig   `json:"service"`
	Secrets     *SecretsMapping `json:"secrets,omitempty"`
	EnvMappings EnvMappings     `json:"env_mappings"`
	Database    *DatabaseConfig `json:"database"`
	OTEL        *OTELConfig     `json:"otel,omitempty"`
	Prometheus  map[string]any  `json:"prometheus,omitempty"`
	Sidecar     *SidecarConfig  `json:"sidecar,omitempty"`
}

// reconcileConfigMap creates or updates the ConfigMap for EvalHub configuration
func (r *EvalHubReconciler) reconcileConfigMap(ctx context.Context, instance *evalhubv1.EvalHub) error {
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
	configData, err := r.generateConfigData(ctx, instance)
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
func (r *EvalHubReconciler) generateConfigData(ctx context.Context, instance *evalhubv1.EvalHub) (map[string]string, error) {
	evalHubImage, err := r.getImageFromConfigMap(ctx, configMapEvalHubImageKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get EvalHub image: %w", err)
	}

	config := EvalHubConfig{
		Service: ServiceConfig{
			Port:             evalHubAppPort,
			DisableAuth:      true,
			ReadyFile:        "/tmp/repo-ready",
			TerminationFile:  "/tmp/termination-log",
			EvalInitImage:    evalHubImage,
			EvalSidecarImage: evalHubImage,
		},
		EnvMappings: EnvMappings{
			"PORT":                        "service.port",
			"API_HOST":                    "service.host",
			"DISABLE_AUTH":                "service.disable_auth",
			"TLS_CERT_FILE":               "service.tls_cert_file",
			"TLS_KEY_FILE":                "service.tls_key_file",
			"DB_URL":                      "database.url",
			"MLFLOW_TRACKING_URI":         "mlflow.tracking_uri",
			"MLFLOW_CA_CERT_PATH":         "mlflow.ca_cert_path",
			"MLFLOW_INSECURE_SKIP_VERIFY": "mlflow.insecure_skip_verify",
			"MLFLOW_TOKEN_PATH":           "mlflow.token_path",
			"MLFLOW_WORKSPACE":            "mlflow.workspace",
		},
		Prometheus: map[string]any{
			"enabled": true,
		},
		Sidecar: &SidecarConfig{
			BaseURL: sidecarBaseURL,
			EvalHub: &EvalHubClientConfig{
				HTTPTimeout:        30 * time.Second,
				InsecureSkipVerify: false,
			},
			SidecarContainer: &SidecarContainerConfig{
				Image: evalHubImage,
				Resources: &ResourceRequirements{
					Requests: &ResourceRequirementDef{
						CPU:    "200m",
						Memory: "512Mi",
					},
					Limits: &ResourceRequirementDef{
						CPU:    "500m",
						Memory: "2Gi",
					},
				},
			},
		},
	}

	// Database configuration — always present since the controller validates
	// that spec.database is set before reaching this point.
	if instance.Spec.IsSQLite() {
		config.Database = &DatabaseConfig{
			Driver: "sqlite",
			URL:    "file::eval_hub:?mode=memory&cache=shared",
		}
	} else if instance.Spec.IsPostgreSQL() {
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
		"auth.yaml":   generateAuthConfigData(),
	}, nil
}

// generateAuthConfigData returns the authorization configuration for the ConfigMap.
// This defines per-endpoint SAR (SubjectAccessReview) policies used for multi-tenancy,
// mapping the X-Tenant header to namespace-scoped RBAC checks.
func generateAuthConfigData() string {
	return `authorization:
  endpoints:
    - path: /api/v1/evaluations/jobs/*/events
      mappings:
        - methods: [post]
          resources:
            - rewrites:
                byHttpHeader:
                  name: X-Tenant
              resourceAttributes:
                namespace: "{{.FromHeader}}"
                apiGroup: trustyai.opendatahub.io
                resource: status-events
                verb: create
    - path: /api/v1/evaluations/jobs/*
      mappings:
        - methods: [get, delete, put, patch]
          resources:
            - rewrites:
                byHttpHeader:
                  name: X-Tenant
              resourceAttributes:
                namespace: "{{.FromHeader}}"
                apiGroup: trustyai.opendatahub.io
                resource: evaluations
                verb: "{{.FromMethod}}"
    - path: /api/v1/evaluations/jobs
      mappings:
        - methods: [get]
          resources:
            - rewrites:
                byHttpHeader:
                  name: X-Tenant
              resourceAttributes:
                namespace: "{{.FromHeader}}"
                apiGroup: trustyai.opendatahub.io
                resource: evaluations
                verb: list
        - methods: [post]
          resources:
            - rewrites:
                byHttpHeader:
                  name: X-Tenant
              resourceAttributes:
                namespace: "{{.FromHeader}}"
                apiGroup: trustyai.opendatahub.io
                resource: evaluations
                verb: create
            - rewrites:
                byHttpHeader:
                  name: X-Tenant
              resourceAttributes:
                namespace: "{{.FromHeader}}"
                apiGroup: mlflow.kubeflow.org
                resource: experiments
                verb: create
            - rewrites:
                byHttpHeader:
                  name: X-Tenant
              resourceAttributes:
                namespace: "{{.FromHeader}}"
                apiGroup: mlflow.kubeflow.org
                resource: experiments
                verb: get
    - path: /api/v1/evaluations/collections/*
      mappings:
        - methods: [get, delete, put, patch]
          resources:
            - rewrites:
                byHttpHeader:
                  name: X-Tenant
              resourceAttributes:
                namespace: "{{.FromHeader}}"
                apiGroup: trustyai.opendatahub.io
                resource: collections
                verb: "{{.FromMethod}}"
    - path: /api/v1/evaluations/collections
      mappings:
        - methods: [get]
          resources:
            - rewrites:
                byHttpHeader:
                  name: X-Tenant
              resourceAttributes:
                namespace: "{{.FromHeader}}"
                apiGroup: trustyai.opendatahub.io
                resource: collections
                verb: list
        - methods: [post]
          resources:
            - rewrites:
                byHttpHeader:
                  name: X-Tenant
              resourceAttributes:
                namespace: "{{.FromHeader}}"
                apiGroup: trustyai.opendatahub.io
                resource: collections
                verb: create
    - path: /api/v1/evaluations/providers/*
      mappings:
        - methods: [get, delete, put, patch]
          resources:
            - rewrites:
                byHttpHeader:
                  name: X-Tenant
              resourceAttributes:
                namespace: "{{.FromHeader}}"
                apiGroup: trustyai.opendatahub.io
                resource: providers
                verb: "{{.FromMethod}}"
    - path: /api/v1/evaluations/providers
      mappings:
        - methods: [get]
          resources:
            - rewrites:
                byHttpHeader:
                  name: X-Tenant
              resourceAttributes:
                namespace: "{{.FromHeader}}"
                apiGroup: trustyai.opendatahub.io
                resource: providers
                verb: list
        - methods: [post]
          resources:
            - rewrites:
                byHttpHeader:
                  name: X-Tenant
              resourceAttributes:
                namespace: "{{.FromHeader}}"
                apiGroup: trustyai.opendatahub.io
                resource: providers
                verb: create
`
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
		Name:      r.effectiveOperatorConfigMapName(),
	}

	// Create an empty ConfigMap object
	var cm corev1.ConfigMap

	// Try to get the ConfigMap
	err := r.Client.Get(ctx, configMapKey, &cm)
	if err != nil {
		if errors.IsNotFound(err) {
			// ConfigMap not found - FAIL deployment with clear error
			return "", fmt.Errorf("required configmap '%s' not found in namespace '%s' - operator configuration missing",
				configMapKey.Name, r.Namespace)
		}
		return "", fmt.Errorf("error reading configmap '%s' in namespace '%s': %w",
			configMapKey.Name, r.Namespace, err)
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

// reconcileProviderConfigMaps copies system provider ConfigMaps from the operator namespace to
// the EvalHub CR's namespace, and discovers tenant-scoped provider ConfigMaps (labeled
// evalhub-provider-type=tenant) already present in the instance namespace.
//
// System ConfigMaps are mounted at /etc/evalhub/config/providers/ (existing behaviour, unchanged).
// Tenant ConfigMaps are used directly from the instance namespace and returned separately so
// the deployment can mount them at /etc/evalhub/config/providers/tenant/.
//
// Returns (systemCMNames, tenantCMNames, error).
func (r *EvalHubReconciler) reconcileProviderConfigMaps(ctx context.Context, instance *evalhubv1.EvalHub) ([]string, []string, error) {
	log := log.FromContext(ctx)

	// --- System providers (existing behaviour) ---
	var systemCMNames []string
	for _, providerName := range instance.Spec.Providers {
		var sourceList corev1.ConfigMapList
		if err := r.List(ctx, &sourceList,
			client.InNamespace(r.Namespace),
			client.MatchingLabels{
				providerLabel:     providerTypeSystem,
				providerNameLabel: providerName,
			}); err != nil {
			return nil, nil, fmt.Errorf("failed to list provider ConfigMaps for %q in namespace %s: %w", providerName, r.Namespace, err)
		}
		if len(sourceList.Items) == 0 {
			return nil, nil, fmt.Errorf("provider %q not found: no ConfigMap with label %s=%s in namespace %s",
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

		getErr := r.Get(ctx, client.ObjectKeyFromObject(configMap), configMap)
		if getErr != nil && !errors.IsNotFound(getErr) {
			return nil, nil, getErr
		}

		if errors.IsNotFound(getErr) {
			configMap.Data = src.Data
			if instance.UID != "" {
				if err := controllerutil.SetControllerReference(instance, configMap, r.Scheme); err != nil {
					return nil, nil, err
				}
			}
			log.Info("Creating Provider ConfigMap", "name", targetName, "provider", providerName)
			if err := r.Create(ctx, configMap); err != nil {
				return nil, nil, err
			}
		} else {
			configMap.Data = src.Data
			log.Info("Updating Provider ConfigMap", "name", targetName, "provider", providerName)
			if err := r.Update(ctx, configMap); err != nil {
				return nil, nil, err
			}
		}

		systemCMNames = append(systemCMNames, targetName)
	}

	// --- Tenant providers (new) ---
	// Discover ConfigMaps labeled evalhub-provider-type=tenant in the instance namespace.
	// These are used directly — no copy required. They mount at a separate path so they
	// cannot shadow system providers.
	var tenantList corev1.ConfigMapList
	if err := r.List(ctx, &tenantList,
		client.InNamespace(instance.Namespace),
		client.MatchingLabels{providerLabel: providerTypeTenant}); err != nil {
		return nil, nil, fmt.Errorf("failed to list tenant provider ConfigMaps in namespace %s: %w", instance.Namespace, err)
	}

	var tenantCMNames []string
	for i := range tenantList.Items {
		tenantCMNames = append(tenantCMNames, tenantList.Items[i].Name)
		log.V(1).Info("Found tenant Provider ConfigMap", "name", tenantList.Items[i].Name)
	}

	return systemCMNames, tenantCMNames, nil
}

// reconcileCollectionConfigMaps copies system collection ConfigMaps from the operator namespace
// to the EvalHub CR's namespace, and discovers tenant-scoped collection ConfigMaps (labeled
// evalhub-collection-type=tenant) already present in the instance namespace.
//
// System ConfigMaps are mounted at /etc/evalhub/config/collections/ (existing behaviour).
// Tenant ConfigMaps are used directly and returned separately so the deployment can mount them
// at /etc/evalhub/config/collections/tenant/.
//
// Returns (systemCMNames, tenantCMNames, error).
func (r *EvalHubReconciler) reconcileCollectionConfigMaps(ctx context.Context, instance *evalhubv1.EvalHub) ([]string, []string, error) {
	log := log.FromContext(ctx)

	// --- System collections (existing behaviour) ---
	var systemCMNames []string
	for _, collectionName := range instance.Spec.Collections {
		var sourceList corev1.ConfigMapList
		if err := r.List(ctx, &sourceList,
			client.InNamespace(r.Namespace),
			client.MatchingLabels{
				collectionLabel:     collectionTypeSystem,
				collectionNameLabel: collectionName,
			}); err != nil {
			return nil, nil, fmt.Errorf("failed to list collection ConfigMaps for %q in namespace %s: %w", collectionName, r.Namespace, err)
		}
		if len(sourceList.Items) == 0 {
			return nil, nil, fmt.Errorf("collection %q not found: no ConfigMap with label %s=%s in namespace %s",
				collectionName, collectionNameLabel, collectionName, r.Namespace)
		}

		src := &sourceList.Items[0]
		targetName := instance.Name + "-collection-" + collectionName

		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      targetName,
				Namespace: instance.Namespace,
			},
		}

		getErr := r.Get(ctx, client.ObjectKeyFromObject(configMap), configMap)
		if getErr != nil && !errors.IsNotFound(getErr) {
			return nil, nil, getErr
		}

		if errors.IsNotFound(getErr) {
			configMap.Data = src.Data
			if instance.UID != "" {
				if err := controllerutil.SetControllerReference(instance, configMap, r.Scheme); err != nil {
					return nil, nil, err
				}
			}
			log.Info("Creating Collection ConfigMap", "name", targetName, "collection", collectionName)
			if err := r.Create(ctx, configMap); err != nil {
				return nil, nil, err
			}
		} else {
			configMap.Data = src.Data
			log.Info("Updating Collection ConfigMap", "name", targetName, "collection", collectionName)
			if err := r.Update(ctx, configMap); err != nil {
				return nil, nil, err
			}
		}

		systemCMNames = append(systemCMNames, targetName)
	}

	// --- Tenant collections (new) ---
	// Discover ConfigMaps labeled evalhub-collection-type=tenant in the instance namespace.
	var tenantList corev1.ConfigMapList
	if err := r.List(ctx, &tenantList,
		client.InNamespace(instance.Namespace),
		client.MatchingLabels{collectionLabel: collectionTypeTenant}); err != nil {
		return nil, nil, fmt.Errorf("failed to list tenant collection ConfigMaps in namespace %s: %w", instance.Namespace, err)
	}

	var tenantCMNames []string
	for i := range tenantList.Items {
		tenantCMNames = append(tenantCMNames, tenantList.Items[i].Name)
		log.V(1).Info("Found tenant Collection ConfigMap", "name", tenantList.Items[i].Name)
	}

	return systemCMNames, tenantCMNames, nil
}

// collectionVolumeProjections builds VolumeProjection entries for mounting collection ConfigMaps
// into a single projected volume.
func collectionVolumeProjections(cmNames []string) []corev1.VolumeProjection {
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
func (r *EvalHubReconciler) reconcileServiceCAConfigMap(ctx context.Context, instance *evalhubv1.EvalHub) error {
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

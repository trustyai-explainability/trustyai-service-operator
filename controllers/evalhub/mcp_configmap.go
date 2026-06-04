package evalhub

import (
	"context"
	"fmt"
	"maps"

	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

func mcpConfigMapName(instance *evalhubv1alpha1.EvalHub) string {
	return instance.Name + "-mcp-config"
}

func mcpConfigFilePath() string {
	return mcpConfigMountPath + "/" + mcpConfigFileName
}

// MCPConfig is the evalhub-mcp YAML config (see https://eval-hub.github.io/mcp/installation/).
// Tokens are supplied via EVALHUB_TOKEN env, not this file.
type MCPConfig struct {
	BaseURL            string `json:"base_url"`
	Transport          string `json:"transport,omitempty"`
	Host               string `json:"host,omitempty"`
	Port               int    `json:"port,omitempty"`
	CACertPath         string `json:"ca_cert_path,omitempty"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify,omitempty"`
}

// reconcileMCPConfigMap creates or updates the ConfigMap for the MCP server.
// If MCP is disabled, any existing MCP ConfigMap is deleted.
func (r *EvalHubReconciler) reconcileMCPConfigMap(ctx context.Context, instance *evalhubv1alpha1.EvalHub) error {
	log := log.FromContext(ctx)
	name := mcpConfigMapName(instance)

	if !instance.Spec.IsMCPEnabled() {
		return r.deleteMCPResource(ctx, instance, &corev1.ConfigMap{}, name)
	}

	log.Info("Reconciling MCP ConfigMap", "name", name)

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
		},
	}

	getErr := r.Get(ctx, client.ObjectKeyFromObject(configMap), configMap)
	if getErr != nil && !errors.IsNotFound(getErr) {
		return getErr
	}

	configData, err := r.generateMCPConfigData(instance)
	if err != nil {
		return fmt.Errorf("failed to generate MCP config data: %w", err)
	}

	desiredLabels := mcpLabels(instance)

	if errors.IsNotFound(getErr) {
		configMap.Data = configData
		configMap.Labels = desiredLabels
		if err := controllerutil.SetControllerReference(instance, configMap, r.Scheme); err != nil {
			return err
		}
		log.Info("Creating MCP ConfigMap", "name", name)
		return r.Create(ctx, configMap)
	}

	if mcpConfigMapMatchesDesired(configMap, configData, desiredLabels, instance) {
		return nil
	}

	configMap.Data = configData
	configMap.Labels = desiredLabels
	if err := controllerutil.SetControllerReference(instance, configMap, r.Scheme); err != nil {
		return err
	}
	log.Info("Updating MCP ConfigMap", "name", name)
	return r.Update(ctx, configMap)
}

// mcpConfigMapMatchesDesired reports whether the live ConfigMap already matches desired data, labels, and controller ownership.
func mcpConfigMapMatchesDesired(cm *corev1.ConfigMap, data map[string]string, labels map[string]string, instance *evalhubv1alpha1.EvalHub) bool {
	if !maps.Equal(cm.Data, data) {
		return false
	}
	if !maps.Equal(cm.Labels, labels) {
		return false
	}
	return metav1.IsControlledBy(cm, instance)
}

func (r *EvalHubReconciler) generateMCPConfigData(instance *evalhubv1alpha1.EvalHub) (map[string]string, error) {
	mcp := instance.Spec.MCP
	transport := mcpClientTransport(mcp)

	evalHubServiceURL := fmt.Sprintf("https://%s.%s.svc.cluster.local:%d", instance.Name, instance.Namespace, servicePort)

	config := MCPConfig{
		BaseURL:            evalHubServiceURL,
		Transport:          transport,
		Host:               "127.0.0.1",
		Port:               mcpAppPort,
		CACertPath:         mcpServiceCAMountPath + "/" + serviceCACertFile,
		InsecureSkipVerify: false,
	}

	configYAML, err := yaml.Marshal(config)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		mcpConfigFileName:     string(configYAML),
		evalHubAuthConfigMapKey: generateMCPAuthConfigData(instance),
	}, nil
}

// generateMCPAuthConfigData returns kube-rbac-proxy authorization for the MCP server.
// Clients must hold get/create on evalhubs/proxy for this EvalHub instance (see createAPIAccessRole).
func generateMCPAuthConfigData(instance *evalhubv1alpha1.EvalHub) string {
	return fmt.Sprintf(`authorization:
  endpoints:
    - path: /*
      mappings:
        - resources:
            - resourceAttributes:
                namespace: %q
                apiGroup: trustyai.opendatahub.io
                resource: evalhubs
                subresource: proxy
                name: %q
                verb: get
            - resourceAttributes:
                namespace: %q
                apiGroup: trustyai.opendatahub.io
                resource: evalhubs
                subresource: proxy
                name: %q
                verb: create
`, instance.Namespace, instance.Name, instance.Namespace, instance.Name)
}

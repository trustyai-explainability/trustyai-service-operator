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

func mcpConfigMapName(instance *evalhubv1alpha1.EvalHub) string {
	return instance.Name + "-mcp-config"
}

// MCPConfig represents the MCP server configuration file structure
type MCPConfig struct {
	EvalHub   MCPEvalHubConfig `json:"evalhub"`
	Transport string           `json:"transport"`
	Host      string           `json:"host"`
	Port      int              `json:"port"`
}

// MCPEvalHubConfig represents the EvalHub connection settings within MCP config
type MCPEvalHubConfig struct {
	BaseURL            string `json:"base_url"`
	CACertPath         string `json:"ca_cert_path,omitempty"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify"`
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

	if errors.IsNotFound(getErr) {
		configMap.Data = configData
		configMap.Labels = mcpLabels(instance)
		if err := controllerutil.SetControllerReference(instance, configMap, r.Scheme); err != nil {
			return err
		}
		log.Info("Creating MCP ConfigMap", "name", name)
		return r.Create(ctx, configMap)
	}

	configMap.Data = configData
	log.Info("Updating MCP ConfigMap", "name", name)
	return r.Update(ctx, configMap)
}

func (r *EvalHubReconciler) generateMCPConfigData(instance *evalhubv1alpha1.EvalHub) (map[string]string, error) {
	transport := instance.Spec.MCP.Transport
	if transport == "" {
		transport = "http-sse"
	}

	evalHubServiceURL := fmt.Sprintf("https://%s.%s.svc.cluster.local:%d", instance.Name, instance.Namespace, servicePort)

	config := MCPConfig{
		EvalHub: MCPEvalHubConfig{
			BaseURL:            evalHubServiceURL,
			CACertPath:         mcpServiceCAMountPath + "/" + serviceCACertFile,
			InsecureSkipVerify: false,
		},
		Transport: transport,
		Host:      "0.0.0.0",
		Port:      mcpContainerPort,
	}

	configYAML, err := yaml.Marshal(config)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		mcpConfigFileName: string(configYAML),
	}, nil
}

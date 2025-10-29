package gorch

import (
	"context"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"

	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/gorch/templates"
)

const orchestratorKubeRBACProxyConfigTemplatePath = "kube-rbac-proxy/orchestrator-config.tmpl.yaml"
const gatewayKubeRBACProxyConfigTemplatePath = "kube-rbac-proxy/gateway-config.tmpl.yaml"

// ensureOrchestratorKubeRBACProxyConfigMap ensures the orchestrator kube-rbac-proxy ConfigMap exists
func (r *GuardrailsOrchestratorReconciler) ensureOrchestratorKubeRBACProxyConfigMap(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) error {
	rbacConfigMapName := orchestrator.Name + "-kube-rbac-proxy-orchestrator-config"
	return utils.EnsureConfigMap(ctx, r.Client, orchestrator, rbacConfigMapName, "", orchestratorKubeRBACProxyConfigTemplatePath, templateParser.ParseResource)
}

// ensureGatewayKubeRBACProxyConfigMap ensures the gateway kube-rbac-proxy ConfigMap exists
func (r *GuardrailsOrchestratorReconciler) ensureGatewayKubeRBACProxyConfigMap(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) error {
	rbacConfigMapName := orchestrator.Name + "-kube-rbac-proxy-gateway-config"
	return utils.EnsureConfigMap(ctx, r.Client, orchestrator, rbacConfigMapName, "", gatewayKubeRBACProxyConfigTemplatePath, templateParser.ParseResource)
}

package gorch

import (
	"context"
	"fmt"
	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/gorch/templates"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// configureKubeRBACProxy creates the kube-rbac-proxy config structs to be used in the deployment template
func (r *GuardrailsOrchestratorReconciler) configureKubeRBACProxy(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator, deploymentConfig *DeploymentConfig) error {
	kubeRBACProxyImage, err := utils.GetImageFromConfigMap(ctx, r.Client, kubeRBACProxyImageKey, constants.ConfigMap, r.Namespace)
	if kubeRBACProxyImage == "" || err != nil {
		log.FromContext(ctx).Error(err, "Error getting Kube-RBAC-Proxy image from ConfigMap.")
		return err
	}
	log.FromContext(ctx).Info("Using kube-rbac-proxy image " + kubeRBACProxyImage + " " + "from configmap " + r.Namespace + ":" + constants.ConfigMap)

	deploymentConfig.OrchestratorKubeRBACProxy = &utils.KubeRBACProxyConfig{
		Suffix:             "orchestrator",
		Namespace:          orchestrator.Namespace,
		Name:               orchestrator.Name,
		KubeRBACProxyImage: kubeRBACProxyImage,
		DownstreamPort:     8432,
		HealthPort:         9443,
		UpstreamProtocol:   "https",
		UpstreamHost:       fmt.Sprintf("%s-service.%s.svc", orchestrator.Name, orchestrator.Namespace), // use full service name to avoid certificate validation issues
		UpstreamPort:       8032,
	}
	if orchestrator.Spec.EnableGuardrailsGateway {
		deploymentConfig.GatewayKubeRBACProxy = &utils.KubeRBACProxyConfig{
			Suffix:             "gateway",
			Namespace:          orchestrator.Namespace,
			Name:               orchestrator.Name,
			KubeRBACProxyImage: kubeRBACProxyImage,
			DownstreamPort:     8490,
			HealthPort:         9444,
			UpstreamProtocol:   "http",
			UpstreamHost:       "localhost",
			UpstreamPort:       8090,
		}
	}

	if orchestrator.Spec.EnableBuiltInDetectors {
		deploymentConfig.BuiltInKubeRBACProxy = &utils.KubeRBACProxyConfig{
			Suffix:             "built-in",
			Namespace:          orchestrator.Namespace,
			Name:               orchestrator.Name,
			KubeRBACProxyImage: kubeRBACProxyImage,
			DownstreamPort:     8480,
			HealthPort:         9445,
			UpstreamProtocol:   "http",
			UpstreamHost:       "localhost",
			UpstreamPort:       8080,
		}
	}

	return nil
}

const orchestratorKubeRBACProxyConfigTemplatePath = "kube-rbac-proxy/orchestrator-config.tmpl.yaml"
const gatewayKubeRBACProxyConfigTemplatePath = "kube-rbac-proxy/gateway-config.tmpl.yaml"
const builtinKubeRBACProxyConfigTemplatePath = "kube-rbac-proxy/built-in-config.tmpl.yaml"

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

// ensureGatewayKubeRBACProxyConfigMap ensures the gateway kube-rbac-proxy ConfigMap exists
func (r *GuardrailsOrchestratorReconciler) ensureBuiltInKubeRBACProxyConfigMap(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) error {
	rbacConfigMapName := orchestrator.Name + "-kube-rbac-proxy-built-in-config"
	return utils.EnsureConfigMap(ctx, r.Client, orchestrator, rbacConfigMapName, "", builtinKubeRBACProxyConfigTemplatePath, templateParser.ParseResource)
}

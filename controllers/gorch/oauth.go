package gorch

import (
	"context"
	"fmt"
	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strings"
)

type KubeRBACProxyConfig struct {
	Suffix             string
	Name               string
	Namespace          string
	KubeRBACProxyImage string
	UpstreamProtocol   string
	UpstreamHost       string
	UpstreamPort       int
	DownstreamPort     int
	HealthPort         int
}

// requiresOAuth checks if the oauth annotation key is set in the orchestrator CR
func requiresOAuth(orchestrator *gorchv1alpha1.GuardrailsOrchestrator) bool {
	val, ok := orchestrator.Annotations[oAuthAnnotationKey]
	return ok && strings.ToLower(val) == "true"
}

// configureKubeRBACProxy creates the kube-rbac-proxy config structs to be used in the deployment template
func (r *GuardrailsOrchestratorReconciler) configureKubeRBACProxy(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator, deploymentConfig *DeploymentConfig) error {
	kubeRBACProxyImage, err := r.getImageFromConfigMap(ctx, kubeRBACProxyImageKey, constants.ConfigMap, r.Namespace)
	if kubeRBACProxyImage == "" || err != nil {
		log.FromContext(ctx).Error(err, "Error getting Kube-RBAC-Proxy image from ConfigMap.")
		return err
	}
	log.FromContext(ctx).Info("Using kube-rbac-proxy image " + kubeRBACProxyImage + " " + "from configmap " + r.Namespace + ":" + constants.ConfigMap)

	deploymentConfig.OrchestratorKubeRBACProxy = &KubeRBACProxyConfig{
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
		deploymentConfig.GatewayKubeRBACProxy = &KubeRBACProxyConfig{
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
	return nil
}

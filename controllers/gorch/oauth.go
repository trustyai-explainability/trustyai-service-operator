package gorch

import (
	"context"
	"fmt"
	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strings"
)

type OAuthConfig struct {
	Suffix           string
	Name             string
	Namespace        string
	OAuthProxyImage  string
	UpstreamProtocol string
	UpstreamHost     string
	UpstreamPort     int
	DownstreamPort   int
}

// requiresOAuth checks if the oauth annotation key is set in the orchestrator CR
func requiresOAuth(orchestrator *gorchv1alpha1.GuardrailsOrchestrator) bool {
	val, ok := orchestrator.Annotations[oAuthAnnotationKey]
	return ok && strings.ToLower(val) == "true"
}

// configureOAuth creates the oauth config structs to be used in the deployment template
func (r *GuardrailsOrchestratorReconciler) configureOAuth(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator, deploymentConfig *DeploymentConfig) error {
	oAuthImage, err := r.getImageFromConfigMap(ctx, oAuthImageKey, constants.ConfigMap, r.Namespace)
	if oAuthImage == "" || err != nil {
		log.FromContext(ctx).Error(err, "Error getting OAuth proxy image from ConfigMap.")
		return err
	}
	log.FromContext(ctx).Info("Using oauth image " + oAuthImage + " " + "from configmap " + r.Namespace + ":" + constants.ConfigMap)

	deploymentConfig.OrchestratorOAuthProxy = &OAuthConfig{
		Suffix:           "orchestrator",
		Namespace:        orchestrator.Namespace,
		Name:             orchestrator.Name,
		OAuthProxyImage:  oAuthImage,
		DownstreamPort:   8432,
		UpstreamProtocol: "https",
		UpstreamHost:     fmt.Sprintf("%s-service.%s.svc", orchestrator.Name, orchestrator.Namespace), // use full service name to avoid certificate validation issues
		UpstreamPort:     8032,
	}
	if orchestrator.Spec.EnableGuardrailsGateway {
		deploymentConfig.GatewayOAuthProxy = &OAuthConfig{
			Suffix:           "gateway",
			Namespace:        orchestrator.Namespace,
			Name:             orchestrator.Name,
			OAuthProxyImage:  oAuthImage,
			DownstreamPort:   8490,
			UpstreamProtocol: "http",
			UpstreamHost:     "localhost",
			UpstreamPort:     8090,
		}
	}
	return nil
}

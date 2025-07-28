package gorch

import (
	"context"
	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/gorch/templates"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	appsv1 "k8s.io/api/apps/v1"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type ContainerImages struct {
	OrchestratorImage      string
	DetectorImage          string
	GuardrailsGatewayImage string
}

type DeploymentConfig struct {
	Orchestrator    *gorchv1alpha1.GuardrailsOrchestrator
	ContainerImages ContainerImages
}

const deploymentTemplate = "deployment.tmpl.yaml"

func (r *GuardrailsOrchestratorReconciler) createDeployment(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) *appsv1.Deployment {
	var containerImages ContainerImages

	orchestratorImage, err := utils.GetImageFromConfigMap(ctx, r.Client, orchestratorImageKey, constants.ConfigMap, r.Namespace)
	if orchestratorImage == "" || err != nil {
		log.FromContext(ctx).Error(err, "Error getting container image from ConfigMap.")
	}
	containerImages.OrchestratorImage = orchestratorImage
	log.FromContext(ctx).Info("using OrchestratorImage " + orchestratorImage + " " + "from config map " + r.Namespace + ":" + constants.ConfigMap)

	// Check if the regex detectors are enabled
	if orchestrator.Spec.EnableBuiltInDetectors {
		detectorImage, err := utils.GetImageFromConfigMap(ctx, r.Client, detectorImageKey, constants.ConfigMap, r.Namespace)
		if detectorImage == "" || err != nil {
			log.FromContext(ctx).Error(err, "Error getting detectors image from ConfigMap.")
		}
		log.FromContext(ctx).Info("Using detector image " + detectorImage + " " + "from configmap " + r.Namespace + ":" + constants.ConfigMap)
		containerImages.DetectorImage = detectorImage
	}
	// Check if the guardrails sidecar gateway is enabled
	if orchestrator.Spec.EnableGuardrailsGateway {
		guardrailsGatewayImage, err := utils.GetImageFromConfigMap(ctx, r.Client, gatewayImageKey, constants.ConfigMap, r.Namespace)
		if guardrailsGatewayImage == "" || err != nil {
			log.FromContext(ctx).Error(err, "Error getting guardrails sidecar gateway image from ConfigMap.")
		}
		log.FromContext(ctx).Info("Using sidecar gateway image " + guardrailsGatewayImage + " " + "from configmap " + r.Namespace + ":" + constants.ConfigMap)

		containerImages.GuardrailsGatewayImage = guardrailsGatewayImage
	}

	deploymentConfig := DeploymentConfig{
		Orchestrator:    orchestrator,
		ContainerImages: containerImages,
	}

	var deployment *appsv1.Deployment

	deployment, err = templateParser.ParseResource[appsv1.Deployment](deploymentTemplate, deploymentConfig, reflect.TypeOf(&appsv1.Deployment{}))
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to parse deployment template")
	}
	if err := controllerutil.SetControllerReference(orchestrator, deployment, r.Scheme); err != nil {
		log.FromContext(ctx).Error(err, "Failed to set controller reference for deployment")
	}
	return deployment
}

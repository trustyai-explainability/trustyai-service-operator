package gorch

import (
	"context"
	"reflect"

	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/gorch/templates"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const deploymentTemplatePath = "deployment.tmpl.yaml"

type DeploymentConfig struct {
	Orchestrator   *gorchv1alpha1.GuardrailsOrchestrator
	ContainerImage string
	Version        string
}

// TO-DO: Move configmap args to volumes

func (r *GuardrailsOrchestratorReconciler) createDeployment(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) *appsv1.Deployment {
	containerImage, err := r.getImageFromConfigMap(ctx, configMapKey, defaultContainerImage)
	deploymentConfig := DeploymentConfig{
		Orchestrator:   orchestrator,
		ContainerImage: containerImage,
		Version:        Version,
	}
	if err != nil {
		log.FromContext(ctx).Error(err, "Error getting container image from ConfigMap.")
	}
	var deployment *appsv1.Deployment

	deployment, err = templateParser.ParseResource[appsv1.Deployment](deploymentTemplatePath, deploymentConfig, reflect.TypeOf(&appsv1.Deployment{}))
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to parse deployment template")
	}
	if err := controllerutil.SetControllerReference(orchestrator, deployment, r.Scheme); err != nil {
		log.FromContext(ctx).Error(err, "Failed to set controller reference for deployment")
	}
	return deployment
}

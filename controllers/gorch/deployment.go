package gorch

import (
	"context"
	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/gorch/templates"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	appsv1 "k8s.io/api/apps/v1"
	"reflect"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const deploymentTemplatePath = "deployment.tmpl.yaml"

type ContainerImages struct {
	OrchestratorImage      string
	DetectorImage          string
	GuardrailsGatewayImage string
}

type DeploymentConfig struct {
	Orchestrator              *gorchv1alpha1.GuardrailsOrchestrator
	ContainerImages           ContainerImages
	OrchestratorKubeRBACProxy *utils.KubeRBACProxyConfig
	GatewayKubeRBACProxy      *utils.KubeRBACProxyConfig
	BuiltInKubeRBACProxy      *utils.KubeRBACProxyConfig
}

func (r *GuardrailsOrchestratorReconciler) createDeployment(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) (*appsv1.Deployment, error) {
	var containerImages ContainerImages

	orchestratorImage, err := utils.GetImageFromConfigMap(ctx, r.Client, orchestratorImageKey, constants.ConfigMap, r.Namespace)
	if orchestratorImage == "" || err != nil {
		log.FromContext(ctx).Error(err, "Error getting container image from ConfigMap.")
		return nil, err
	}
	containerImages.OrchestratorImage = orchestratorImage
	log.FromContext(ctx).Info("using OrchestratorImage " + orchestratorImage + " " + "from config map " + r.Namespace + ":" + constants.ConfigMap)

	// Check if the regex detectors are enabled
	if orchestrator.Spec.EnableBuiltInDetectors {
		detectorImage, err := utils.GetImageFromConfigMap(ctx, r.Client, detectorImageKey, constants.ConfigMap, r.Namespace)
		if detectorImage == "" || err != nil {
			log.FromContext(ctx).Error(err, "Error getting detectors image from ConfigMap.")
			return nil, err
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
		Orchestrator:              orchestrator,
		ContainerImages:           containerImages,
		OrchestratorKubeRBACProxy: nil,
		GatewayKubeRBACProxy:      nil,
	}

	if utils.RequiresAuth(orchestrator) {
		if err = r.configureKubeRBACProxy(ctx, orchestrator, &deploymentConfig); err != nil {
			log.FromContext(ctx).Error(err, "Error configuring Kube-RBAC-Proxy.")
			return nil, err
		}
	}

	var deployment *appsv1.Deployment
	deployment, err = templateParser.ParseResource[*appsv1.Deployment](deploymentTemplatePath, deploymentConfig, reflect.TypeOf(&appsv1.Deployment{}))
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to parse deployment template")
		return nil, err
	}

	// add env vars to the deployment
	if orchestrator.Spec.EnvVars != nil && len(*orchestrator.Spec.EnvVars) > 0 {
		for i := range deployment.Spec.Template.Spec.Containers {
			if !isKubeRBACProxyContainer(deployment.Spec.Template.Spec.Containers[i].Name) {
				deployment.Spec.Template.Spec.Containers[i].Env = append(
					deployment.Spec.Template.Spec.Containers[i].Env,
					*orchestrator.Spec.EnvVars...)
			}
		}
	}

	if err := controllerutil.SetControllerReference(orchestrator, deployment, r.Scheme); err != nil {
		log.FromContext(ctx).Error(err, "Failed to set controller reference for deployment")
		return nil, err
	}
	return deployment, nil
}

// addTLSMounts adds secret mounts for each TLS serving secret required by the orchestrator
func (r GuardrailsOrchestratorReconciler) addTLSMounts(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator, deployment *appsv1.Deployment, tlsMounts []gorchv1alpha1.DetectedService) error {
	if len(tlsMounts) > 0 {
		for i := range tlsMounts {
			MountSecret(deployment, tlsMounts[i].TLSSecret)

			// validate that the TLS serving secrets indeed exist before creating deployment
			_, err := utils.GetSecret(ctx, r.Client, tlsMounts[i].TLSSecret, orchestrator.Namespace)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// patchDeployment updates existingDeployment in-place to match newDeployment's Spec and Template Annotations.
// Returns true if any changes were made.
func patchDeployment(existingDeployment, newDeployment *appsv1.Deployment) bool {
	changed := false

	// Patch Spec (deep copy to avoid pointer issues)
	if !reflect.DeepEqual(existingDeployment.Spec, newDeployment.Spec) {
		existingDeployment.Spec = *newDeployment.Spec.DeepCopy()
		changed = true
	}

	// Patch Annotations (if needed)
	if !reflect.DeepEqual(existingDeployment.Annotations, newDeployment.Annotations) {
		existingDeployment.Annotations = map[string]string{}
		for k, v := range newDeployment.Annotations {
			existingDeployment.Annotations[k] = v
		}
		changed = true
	}

	return changed
}

// isKubeRBACProxyContainer checks if a pod name corresponding to a kube-rbac-proxy pod
func isKubeRBACProxyContainer(name string) bool {
	return strings.HasPrefix(name, "kube-rbac-proxy")
}

package nemo

import (
	"context"
	nemov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/nemo/templates"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	appsv1 "k8s.io/api/apps/v1"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type ContainerImages struct {
	NemoGuardrailsImage string
	OAuthProxyImage     string
}

type DeploymentConfig struct {
	NemoGuardrails  *nemov1alpha1.NemoGuardrails
	ContainerImages ContainerImages
}

const deploymentTemplateFilename = "deployment.tmpl.yaml"

func (r *NemoGuardrailsReconciler) createDeployment(ctx context.Context, nemoGuardrails *nemov1alpha1.NemoGuardrails) (*appsv1.Deployment, error) {
	var containerImages ContainerImages

	// get nemo guardrails image from trustyai configmap
	nemoGuardrailsImage, err := utils.GetImageFromConfigMap(ctx, r.Client, nemoImageKey, constants.ConfigMap, r.Namespace)
	if nemoGuardrailsImage == "" || err != nil {
		log.FromContext(ctx).Error(err, "Error getting nemo-guardrails container image from ConfigMap.")
		return nil, err
	}
	containerImages.NemoGuardrailsImage = nemoGuardrailsImage
	log.FromContext(ctx).Info("using NemoGuardrailsImage " + nemoGuardrailsImage + " " + "from config map " + r.Namespace + ":" + constants.ConfigMap)

	// get oauth image from trustyai configmap
	oauthImage, err := utils.GetImageFromConfigMap(ctx, r.Client, oauthProxyImageKey, constants.ConfigMap, r.Namespace)
	if err != nil {
		log.FromContext(ctx).Error(err, "Error getting oauth container image from ConfigMap.")
		return nil, err
	}
	containerImages.OAuthProxyImage = oauthImage
	log.FromContext(ctx).Info("using OauthProxyImage " + oauthImage + " " + "from config map " + r.Namespace + ":" + constants.ConfigMap)

	deploymentConfig := DeploymentConfig{
		NemoGuardrails:  nemoGuardrails,
		ContainerImages: containerImages,
	}
	var deployment *appsv1.Deployment

	deployment, err = templateParser.ParseResource[appsv1.Deployment](deploymentTemplateFilename, deploymentConfig, reflect.TypeOf(&appsv1.Deployment{}))
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to parse deployment template")
	}
	if err := controllerutil.SetControllerReference(nemoGuardrails, deployment, r.Scheme); err != nil {
		log.FromContext(ctx).Error(err, "Failed to set controller reference for deployment")
		return nil, err
	}
	return deployment, nil
}

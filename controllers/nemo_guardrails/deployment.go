package nemo_guardrails

import (
	"context"
	"fmt"
	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/nemo_guardrails/templates"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type ContainerImages struct {
	NemoGuardrailsImage string
	AuthProxyImage      string
}

type DeploymentConfig struct {
	NemoGuardrails  *nemoguardrailsv1alpha1.NemoGuardrails
	ContainerImages ContainerImages
	UseAuthProxy    bool
}

const deploymentTemplateFilename = "deployment.tmpl.yaml"

func (r *NemoGuardrailsReconciler) createDeployment(ctx context.Context, nemoGuardrails *nemoguardrailsv1alpha1.NemoGuardrails) (*appsv1.Deployment, *string, error) {
	var containerImages ContainerImages

	// ==== get nemo guardrails image from trustyai configmap ===========================================================
	nemoGuardrailsImage, err := utils.GetImageFromConfigMap(ctx, r.Client, nemoGuardrailsImageKey, constants.ConfigMap, r.Namespace)
	if nemoGuardrailsImage == "" || err != nil {
		utils.LogErrorRetrieving(ctx, err, "nemo-guardrails image from configmap", constants.ConfigMap, r.Namespace)
		return nil, nil, err
	}
	containerImages.NemoGuardrailsImage = nemoGuardrailsImage
	log.FromContext(ctx).Info("using NemoGuardrailsImage " + nemoGuardrailsImage + " " + "from config map " + r.Namespace + ":" + constants.ConfigMap)

	// ==== get kube-rbac-proxy image from trustyai configmap ===========================================================
	authImage, err := utils.GetImageFromConfigMap(ctx, r.Client, configMapKubeRBACProxyImageKey, constants.ConfigMap, r.Namespace)
	if err != nil {
		utils.LogErrorRetrieving(ctx, err, "oauth image from configmap", constants.ConfigMap, r.Namespace)
		return nil, nil, err
	}
	containerImages.AuthProxyImage = authImage
	log.FromContext(ctx).Info("using AuthProxyImage " + authImage + " " + "from config map " + r.Namespace + ":" + constants.ConfigMap)

	// ==== create deployment definition ================================================================================
	deploymentConfig := DeploymentConfig{
		NemoGuardrails:  nemoGuardrails,
		ContainerImages: containerImages,
		UseAuthProxy:    utils.RequiresAuth(nemoGuardrails),
	}
	var deployment *appsv1.Deployment

	deployment, err = templateParser.ParseResource[*appsv1.Deployment](deploymentTemplateFilename, deploymentConfig, reflect.TypeOf(&appsv1.Deployment{}))
	if err != nil {
		utils.LogErrorParsing(ctx, err, "deployment template", nemoGuardrails.Name, nemoGuardrails.Namespace)
	}
	if err := controllerutil.SetControllerReference(nemoGuardrails, deployment, r.Scheme); err != nil {
		utils.LogErrorControllerReference(ctx, err, "deployment", deployment.Name, deployment.Namespace)
		return nil, nil, err
	}

	// Mount configuration configmaps
	var defaultConfig string
	defaultAlreadyChosen := false

	for idx, nemoConfig := range nemoGuardrails.Spec.NemoConfigs {
		// take the first config as default- this will be overwritten if any config has the default
		if idx == 0 {
			defaultConfig = nemoConfig.Name
		}
		if nemoConfig.ConfigMaps == nil || len(nemoConfig.ConfigMaps) == 0 {
			return nil, nil, fmt.Errorf("no configmaps provided inside NemoConfig=%s", nemoConfig.Name)
		}

		for _, configCM := range nemoConfig.ConfigMaps {

			configmap := &corev1.ConfigMap{}
			if err := r.Client.Get(ctx, types.NamespacedName{Name: configCM, Namespace: nemoGuardrails.Namespace}, configmap); err != nil {
				utils.LogErrorRetrieving(ctx, err, "configmap", configCM, deployment.Namespace)
				return nil, nil, err
			}
			volumeName := fmt.Sprintf("%s-%s-volume", nemoConfig.Name, configCM)
			utils.MountConfigMapToDeployment(configmap, volumeName, deployment)
			volumeMount := corev1.VolumeMount{
				Name:      volumeName,
				MountPath: "/app/config/" + nemoConfig.Name,
			}
			// Add the volumeMount to the first container's VolumeMounts
			deployment.Spec.Template.Spec.Containers[0].VolumeMounts = append(
				deployment.Spec.Template.Spec.Containers[0].VolumeMounts,
				volumeMount,
			)
		}

		// If a config is specified as the default config, mark it as such. If multiple configs are specified as default, throw a warning
		if nemoConfig.Default {
			if defaultAlreadyChosen {
				log.FromContext(ctx).Info(fmt.Sprintf(
					"warning: Two or more NemoConfigs have set default=true. Only '%s' will be used as default, as it was the first in the NemoConfig list to specify default=true.", defaultConfig))
			} else {
				defaultConfig = nemoConfig.Name
				defaultAlreadyChosen = true
			}
		}
	}

	if !defaultAlreadyChosen {
		log.FromContext(ctx).Info(fmt.Sprintf("no NemoConfigs were marked as default, using '%s' as default", defaultConfig))
	}

	// Set default config
	deployment.Spec.Template.Spec.Containers[0].Env = append(
		deployment.Spec.Template.Spec.Containers[0].Env,
		corev1.EnvVar{
			Name:  "CONFIG_ID",
			Value: defaultConfig,
		},
	)

	return deployment, &nemoGuardrailsImage, nil
}

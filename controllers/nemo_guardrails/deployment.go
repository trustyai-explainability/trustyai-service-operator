package nemo_guardrails

import (
	"context"
	"crypto/sha256"
	"fmt"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"reflect"
	"sort"
	"strings"

	"github.com/google/uuid"
	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/images"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/nemo_guardrails/templates"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type ContainerImages struct {
	NemoGuardrailsImage string
	AuthProxyImage      string
}

type DeploymentConfig struct {
	NemoGuardrails      *nemoguardrailsv1alpha1.NemoGuardrails
	ContainerImages     ContainerImages
	UseAuthProxy        bool
	KubeRbacProxyConfig *utils.KubeRBACProxyConfig
}

const deploymentTemplateFilename = "deployment.tmpl.yaml"

func GetRBACConfigName(nemoGuardrails nemoguardrailsv1alpha1.NemoGuardrails) string {
	return nemoGuardrails.Name + "-rbac-proxy-config"
}

// setAuthConfig will create a KubeRBACProxyConfig inside the DeploymentConfig for use in template parsing
func (r *NemoGuardrailsReconciler) setAuthConfig(ctx context.Context, nemoGuardrails *nemoguardrailsv1alpha1.NemoGuardrails, deploymentConfig *DeploymentConfig) error {
	// ==== get kube-rbac-proxy image from env var or configmap ===========================================================
	authImage, err := images.GetImageFromConfigMap(ctx, r.Client, configMapKubeRBACProxyImageKey, constants.ConfigMap, r.Namespace)
	if err != nil {
		utils.LogErrorRetrieving(ctx, err, "oauth image from env var or configmap", constants.ConfigMap, r.Namespace)
		return err
	}
	log.FromContext(ctx).Info("using AuthProxyImage " + authImage)

	deploymentConfig.KubeRbacProxyConfig = &utils.KubeRBACProxyConfig{
		Suffix:             "",
		Namespace:          nemoGuardrails.Namespace,
		Name:               GetRBACConfigName(*nemoGuardrails),
		KubeRBACProxyImage: authImage,
		DownstreamPort:     8443,
		HealthPort:         9444,
		UpstreamProtocol:   "http",
		UpstreamHost:       "localhost",
		UpstreamPort:       8000,
	}
	return nil
}

// mountNemoConfigs will take all configmaps specified inside the nemoGuardrails.NemoConfig section of the CR and mount them to the deployment in the specified directories
// this is where user guardrail config files (actions.py, flows.co, etc) are placed into the container
func (r *NemoGuardrailsReconciler) mountNemoConfigs(ctx context.Context, nemoGuardrails *nemoguardrailsv1alpha1.NemoGuardrails, deployment *appsv1.Deployment) error {
	// Mount configuration configmaps
	var defaultConfig string
	defaultAlreadyChosen := false

	// Accumulate a hash of all ConfigMap names and data to detect content changes
	hasher := sha256.New()

	for idx, nemoConfig := range nemoGuardrails.Spec.NemoConfigs {
		// Take the first config as default for now. If any config manually specifies default-ness, we'll override this
		if idx == 0 {
			defaultConfig = nemoConfig.Name
		}
		if nemoConfig.ConfigMaps == nil || len(nemoConfig.ConfigMaps) == 0 {
			return fmt.Errorf("no configmaps provided inside NemoConfig=%s", nemoConfig.Name)
		}

		for _, configCM := range nemoConfig.ConfigMaps {
			configmap := &corev1.ConfigMap{}
			if strings.HasPrefix(configCM, nemoGuardrailsDefaultConfigPrefix) {
				// if the specified config has a matching prefix in the name, try to load from the default configs
				// in the operator namespace
				sourceCM := &corev1.ConfigMap{}
				if err := r.Client.Get(ctx, types.NamespacedName{Name: configCM, Namespace: r.Namespace}, sourceCM); err != nil {
					if !k8serrors.IsNotFound(err) {
						return err
					}
					// not found in operator namespace, fall back to the deployment namespace
					utils.LogErrorRetrieving(ctx, err, "default NeMo Guardrails configmap", configCM, r.Namespace)
				} else {
					// copy from operator namespace into CR namespace
					crNamespaceConfigMap := &corev1.ConfigMap{
						ObjectMeta: metav1.ObjectMeta{
							Name:        fmt.Sprintf("%s-%s", nemoGuardrails.Name, sourceCM.Name),
							Namespace:   nemoGuardrails.Namespace,
							Labels:      sourceCM.Labels,
							Annotations: sourceCM.Annotations,
						},
						Data: sourceCM.Data,
					}
					copiedCM, justCreated, copyErr := utils.ReconcileManuallyDefinedConfigMap(ctx, r.Client, nemoGuardrails, crNamespaceConfigMap)
					if copyErr != nil {
						utils.LogErrorReconciling(ctx, copyErr, "default NeMo Guardrails configmap", configCM, nemoGuardrails.Namespace)
						return copyErr
					} else {
						if !justCreated {
							// if we didn't just create a new config, check to make sure the values are correct
							if err := utils.CompareAndUpdateConfigmap(ctx, r.Client, copiedCM, crNamespaceConfigMap, true); err != nil {
								utils.LogErrorUpdating(ctx, err, "default NeMo Guardrails configmap", configCM, nemoGuardrails.Namespace)
								return err
							}
						}
						configmap = copiedCM
					}
				}
			}

			if configmap.Name == "" {
				// if we didn't end up creating a new configmap in the above logic, try to retrieve from CR namespace
				if err := r.Client.Get(ctx, types.NamespacedName{Name: configCM, Namespace: nemoGuardrails.Namespace}, configmap); err != nil {
					utils.LogErrorRetrieving(ctx, err, "configmap", configCM, deployment.Namespace)
					return err
				}
			}

			labelValue, labelExists := configmap.Labels["nemo-guardrails-config"]
			if !labelExists || labelValue != "true" {
				patch := client.MergeFrom(configmap.DeepCopy())
				if configmap.Labels == nil {
					configmap.Labels = map[string]string{}
				}
				configmap.Labels["nemo-guardrails-config"] = "true"
				if err := r.Client.Patch(ctx, configmap, patch); err != nil {
					return err
				}
			}

			// Include ConfigMap name and data in hash for change detection
			hasher.Write([]byte(configCM))
			keys := make([]string, 0, len(configmap.Data))
			for k := range configmap.Data {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				hasher.Write([]byte(k))
				hasher.Write([]byte(configmap.Data[k]))
			}

			volumeName := fmt.Sprintf("%s-%s-vol", nemoConfig.Name, configmap.Name)
			if len(volumeName) > 63 {
				// prevent a configmap mounting failure if the volume name is too long with a deterministic UUID
				volumeName = uuid.NewSHA1(uuid.NameSpaceURL, []byte(volumeName)).String()
			}
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

	// Set ConfigMap content hash as a pod template annotation to trigger rollout on changes
	configHash := fmt.Sprintf("%x", hasher.Sum(nil))
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = make(map[string]string)
	}
	deployment.Spec.Template.Annotations["trustyai.opendatahub.io/nemo-config-hash"] = configHash

	// Set default config
	deployment.Spec.Template.Spec.Containers[0].Env = append(
		deployment.Spec.Template.Spec.Containers[0].Env,
		corev1.EnvVar{
			Name:  "CONFIG_ID",
			Value: defaultConfig,
		},
	)

	return nil
}

func (r *NemoGuardrailsReconciler) createDeployment(ctx context.Context, nemoGuardrails *nemoguardrailsv1alpha1.NemoGuardrails, caBundleInitContainerConfig utils.CABundleInitContainerConfig, configMapsToMount []corev1.ConfigMap) (*appsv1.Deployment, error) {
	var containerImages ContainerImages

	// ==== get nemo guardrails image from env var or configmap ===========================================================
	nemoGuardrailsImage, err := images.GetImageFromConfigMap(ctx, r.Client, nemoGuardrailsImageKey, constants.ConfigMap, r.Namespace)
	if err != nil {
		utils.LogErrorRetrieving(ctx, err, "nemo-guardrails image from env var or configmap", constants.ConfigMap, r.Namespace)
		return nil, err
	}
	if nemoGuardrailsImage == "" {
		err = fmt.Errorf("configmap %s in namespace %s has empty value for key %s", constants.ConfigMap, r.Namespace, nemoGuardrailsImageKey)
		utils.LogErrorRetrieving(ctx, err, "nemo-guardrails image from configmap", constants.ConfigMap, r.Namespace)
		return nil, err
	}
	containerImages.NemoGuardrailsImage = nemoGuardrailsImage
	log.FromContext(ctx).Info("using NemoGuardrailsImage " + nemoGuardrailsImage + " " + "from config map " + r.Namespace + ":" + constants.ConfigMap)

	// ==== create deployment definition ================================================================================
	deploymentConfig := DeploymentConfig{
		NemoGuardrails:  nemoGuardrails,
		ContainerImages: containerImages,
		UseAuthProxy:    utils.RequiresAuth(nemoGuardrails),
	}
	// === configure kube-rbac-proxy if needed ========
	if deploymentConfig.UseAuthProxy {
		if err := r.setAuthConfig(ctx, nemoGuardrails, &deploymentConfig); err != nil {
			return nil, err
		}
	}

	var deployment *appsv1.Deployment
	deployment, err = templateParser.ParseResource[*appsv1.Deployment](deploymentTemplateFilename, deploymentConfig, reflect.TypeOf(&appsv1.Deployment{}))
	if err != nil {
		utils.LogErrorParsing(ctx, err, "deployment template", nemoGuardrails.Name, nemoGuardrails.Namespace)
		return nil, err
	}
	if err := controllerutil.SetControllerReference(nemoGuardrails, deployment, r.Scheme); err != nil {
		utils.LogErrorControllerReference(ctx, err, "deployment", deployment.Name, deployment.Namespace)
		return nil, err
	}

	// Set replicas from CR spec
	if nemoGuardrails.Spec.Replicas != nil {
		deployment.Spec.Replicas = nemoGuardrails.Spec.Replicas
	}

	// Add user guardrail configs to deployment
	err = r.mountNemoConfigs(ctx, nemoGuardrails, deployment)
	if err != nil {
		return nil, err
	}

	// Add CA to deployment
	err = r.AddCAToDeployment(log.FromContext(ctx), deployment, caBundleInitContainerConfig, nemoGuardrailsImage, configMapsToMount)
	if err != nil {
		return nil, err
	}

	// add user environment variables
	if nemoGuardrails.Spec.Env != nil && len(nemoGuardrails.Spec.Env) > 0 {
		log.FromContext(ctx).Info("Updating NemoGuardrails env with user-provided environment variables")
		deployment.Spec.Template.Spec.Containers[0].Env = append(deployment.Spec.Template.Spec.Containers[0].Env, nemoGuardrails.Spec.Env...)
	}

	// apply pod scheduling constraints
	if nemoGuardrails.Spec.Template != nil && nemoGuardrails.Spec.Template.Pod != nil {
		pod := nemoGuardrails.Spec.Template.Pod
		if pod.Affinity != nil {
			deployment.Spec.Template.Spec.Affinity = pod.Affinity
		}
		if len(pod.Tolerations) > 0 {
			deployment.Spec.Template.Spec.Tolerations = pod.Tolerations
		}
		if len(pod.NodeSelector) > 0 {
			deployment.Spec.Template.Spec.NodeSelector = pod.NodeSelector
		}
	}

	return deployment, nil
}

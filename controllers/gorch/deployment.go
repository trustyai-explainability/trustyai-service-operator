package gorch

import (
	"context"
	"fmt"
	"reflect"
	"time"

	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/gorch/templates"
	appsv1 "k8s.io/api/apps/v1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const deploymentTemplatePath = "deployment.tmpl.yaml"

type ContainerImages struct {
	OrchestratorImage  string
	RegexDetectorImage string
	GatewayImage       string
}

type DeploymentConfig struct {
	Orchestrator    *gorchv1alpha1.GuardrailsOrchestrator
	ContainerImages ContainerImages
}

func (r *GuardrailsOrchestratorReconciler) createDeployment(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) *appsv1.Deployment {
	var containerImages ContainerImages
	// Get orchestrator image
	orchestratorConfigMapName := orchestrator.Name + "-config"
	orchestratorImage, err := r.getImageFromConfigMap(ctx, orchestratorImageKey, constants.ConfigMap, r.Namespace)
	if orchestratorImage == "" || err != nil {
		log.FromContext(ctx).Error(err, "Error getting container image from ConfigMap.")
	}
	containerImages.OrchestratorImage = orchestratorImage
	log.FromContext(ctx).Info("using orchestratorImage " + orchestratorImage + "from config map " + r.Namespace + ":" + constants.ConfigMap)
	// Check if the vLLM gateway is enabled
	if orchestrator.Spec.VLLMGatewayConfig != nil {
		//  Get the gateway and regex detector container images
		vllmGatewayImage, err := r.getImageFromConfigMap(ctx, vllmGatewayImageKey, orchestratorConfigMapName, orchestrator.Namespace)
		if vllmGatewayImage == "" || err != nil {
			log.FromContext(ctx).Error(err, "Error getting vLLM gateway image from ConfigMap.")
		}
		log.FromContext(ctx).Info("using vLLM gateway image " + vllmGatewayImage + "from config map " + orchestrator.Namespace + ":" + orchestratorConfigMapName)
		regexDetectorImage, err := r.getImageFromConfigMap(ctx, regexDetectorImageKey, orchestratorConfigMapName, orchestrator.Namespace)
		if regexDetectorImage == "" || err != nil {
			log.FromContext(ctx).Error(err, "Error getting regex detectors image from ConfigMap.")
		}
		log.FromContext(ctx).Info("using regex detectors image " + regexDetectorImage + "from config map " + orchestrator.Namespace + ":" + orchestratorConfigMapName)
		containerImages.GatewayImage = vllmGatewayImage
		containerImages.RegexDetectorImage = regexDetectorImage
	}

	deploymentConfig := DeploymentConfig{
		Orchestrator:    orchestrator,
		ContainerImages: containerImages,
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

func (r *GuardrailsOrchestratorReconciler) checkDeploymentReady(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) (bool, error) {
	var err error
	var deployment *appsv1.Deployment
	err = retry.OnError(
		wait.Backoff{
			Duration: 5 * time.Second,
		},
		func(err error) bool {
			return errors.IsNotFound(err) || err != nil
		},
		func() error {
			err = r.Get(ctx, types.NamespacedName{Name: orchestrator.Name, Namespace: orchestrator.Namespace}, deployment)
			if err != nil {
				return err
			}
			for _, condition := range deployment.Status.Conditions {
				if condition.Type == appsv1.DeploymentAvailable && condition.Status == "True" {
					err = r.checkPodsReady(ctx, *deployment)
				}
			}
			return fmt.Errorf("deployment %s is not ready", deployment.Name)
		},
	)
	return true, nil
}

func (r GuardrailsOrchestratorReconciler) checkPodsReady(ctx context.Context, deployment appsv1.Deployment) error {
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList, client.InNamespace(deployment.Namespace)); err != nil {
		return err
	}
	// check if all pods are all ready
	for _, pod := range podList.Items {
		for _, cs := range pod.Status.ContainerStatuses {
			if !cs.Ready {
				return fmt.Errorf("pod %s is not ready", pod.Name)
			}
		}
	}
	return nil
}

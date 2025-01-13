package gorch

import (
	"context"
	"fmt"
	"reflect"

	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/gorch/templates"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const configMapTemplatePath = "configmap.tmpl.yaml"

type configMapConfig struct {
	Orchestrator *gorchv1alpha1.GuardrailsOrchestrator
	// ContainerImage string
	// Version        string
}

func (r *GuardrailsOrchestratorReconciler) createConfigMap(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) *corev1.ConfigMap {
	configMapConfig := configMapConfig{
		Orchestrator: orchestrator,
	}

	var configMap *corev1.ConfigMap
	configMap, err := templateParser.ParseResource[corev1.ConfigMap](configMapTemplatePath, configMapConfig, reflect.TypeOf(&corev1.ConfigMap{}))
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to parse configmap template")
	}
	if err := controllerutil.SetControllerReference(orchestrator, configMap, r.Scheme); err != nil {
		log.FromContext(ctx).Error(err, "Failed to set controller reference for configmap")
	}
	return configMap
}

func (r *GuardrailsOrchestratorReconciler) getImageFromConfigMap(ctx context.Context, configMapKey string, defaultContainerImage string) (string, error) {
	if r.Namespace != "" {
		configMap := &corev1.ConfigMap{}
		err := r.Get(ctx, types.NamespacedName{Name: constants.ConfigMap, Namespace: r.Namespace}, configMap)
		if err != nil {
			if errors.IsNotFound(err) {
				return defaultContainerImage, nil
			}
			return defaultContainerImage, fmt.Errorf("error reading configmap %s", constants.ConfigMap)
		}

		containerImage, ok := configMap.Data[configMapKey]

		if !ok {
			return defaultContainerImage, fmt.Errorf("configmap %s does not contain necessary keys", configMapKey)
		}
		return containerImage, nil
	} else {
		return defaultContainerImage, nil
	}
}

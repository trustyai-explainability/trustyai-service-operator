package gorch

import (
	"context"
	"fmt"
	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/gorch/templates"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

type ConfigMapConfig struct {
	Orchestrator *gorchv1alpha1.GuardrailsOrchestrator
}

func (r *GuardrailsOrchestratorReconciler) getImageFromConfigMap(ctx context.Context, configMapKey string, configMapName string, namespace string) (string, error) {
	configMap := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: namespace}, configMap)
	if err != nil {
		if errors.IsNotFound(err) {
			return "", fmt.Errorf("could not find configmap %s on namespace %s", configMapName, namespace)
		}
		return "", fmt.Errorf("error reading configmap %s on namespace %s", configMapName, namespace)
	}

	containerImage, ok := configMap.Data[configMapKey]

	if !ok {
		return "", fmt.Errorf("configmap %s on namespace %s does not contain necessary keys", configMapName, namespace)
	}
	return containerImage, nil
}

func (r *GuardrailsOrchestratorReconciler) createConfigMap(ctx context.Context, configMapTemplatePath string, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) *corev1.ConfigMap {
	configMapConfig := ConfigMapConfig{
		Orchestrator: orchestrator,
	}
	var configMap *corev1.ConfigMap
	configMap, err := templateParser.ParseResource[corev1.ConfigMap](configMapTemplatePath, configMapConfig, reflect.TypeOf(&corev1.ConfigMap{}))

	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to parse configmap template")
	}
	controllerutil.SetControllerReference(orchestrator, configMap, r.Scheme)
	return configMap
}

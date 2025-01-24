package gorch

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// const configMapTemplatePath = "configmap.tmpl.yaml"

// type configMapConfig struct {
// 	Orchestrator *gorchv1alpha1.GuardrailsOrchestrator
// 	// ContainerImage string
// 	// Version        string
// }

// func (r *GuardrailsOrchestratorReconciler) createConfigMap(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) *corev1.ConfigMap {
// 	configMapConfig := configMapConfig{
// 		Orchestrator: orchestrator,
// 	}

// 	var configMap *corev1.ConfigMap
// 	configMap, err := templateParser.ParseResource[corev1.ConfigMap](configMapTemplatePath, configMapConfig, reflect.TypeOf(&corev1.ConfigMap{}))
// 	if err != nil {
// 		log.FromContext(ctx).Error(err, "Failed to parse configmap template")
// 	}
// 	if err := controllerutil.SetControllerReference(orchestrator, configMap, r.Scheme); err != nil {
// 		log.FromContext(ctx).Error(err, "Failed to set controller reference for configmap")
// 	}
// 	return configMap
// }

func (r *GuardrailsOrchestratorReconciler) getImageFromConfigMap(ctx context.Context, configMapKey string, configMapName string, namespace string) (string, error) {
	configMap := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: namespace}, configMap)
	if err != nil {
		if errors.IsNotFound(err) {
			return "", nil
		}
		return "", fmt.Errorf("error reading configmap %s", configMapName)
	}

	containerImage, ok := configMap.Data[configMapKey]

	if !ok {
		return "", fmt.Errorf("configmap %s does not contain necessary keys", configMapName)
	}
	return containerImage, nil
}

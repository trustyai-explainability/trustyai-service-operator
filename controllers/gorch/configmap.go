package gorch

import (
	"context"
	"fmt"

	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

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

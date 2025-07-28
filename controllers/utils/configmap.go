package utils

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetImageFromConfigMap retrieves a value from a ConfigMap by key.
// Can be used by any reconciler by passing in the controller-runtime client.
func GetImageFromConfigMap(ctx context.Context, c client.Client, configMapKey, configMapName, namespace string) (string, error) {
	configMap := &corev1.ConfigMap{}
	err := c.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: namespace}, configMap)
	if err != nil {
		if errors.IsNotFound(err) {
			return "", err
		}
		return "", fmt.Errorf("error reading configmap %s in namespace %s: %w", configMapName, namespace, err)
	}

	value, ok := configMap.Data[configMapKey]
	if !ok {
		return "", fmt.Errorf("configmap %s in namespace %s does not contain key %s", configMapName, namespace, configMapKey)
	}
	return value, nil
}

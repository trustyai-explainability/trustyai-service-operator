package controllers

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// getImageFromConfigMapDefault gets "stock" image values from a ConfigMap in the operator's namespace
func (r *TrustyAIServiceReconciler) getImageFromConfigMapStock(ctx context.Context, key string, defaultImage string) (string, error) {
	if r.Namespace != "" {
		// Define the key for the custom ConfigMap
		configMapKey := types.NamespacedName{
			Namespace: r.Namespace,
			Name:      imageConfigMap,
		}

		// Create an empty ConfigMap object
		var cm corev1.ConfigMap

		// Try to get the ConfigMap
		if err := r.Get(ctx, configMapKey, &cm); err != nil {
			if errors.IsNotFound(err) {
				// ConfigMap not found, fallback to default values
				return defaultImage, nil
			}
			// Other error occurred when trying to fetch the ConfigMap
			return defaultImage, fmt.Errorf("error reading configmap %s", configMapKey)
		}

		// ConfigMap is found, extract the image and tag
		image, ok := cm.Data[key]

		if !ok {
			// One or both of the keys are not present in the ConfigMap, return error
			return defaultImage, fmt.Errorf("configmap %s does not contain necessary keys", configMapKey)
		}

		// Return the image and tag
		return image, nil
	} else {
		return defaultImage, nil
	}
}

// getImageFromConfigMap gets a custom image value from a ConfigMap in the operator's namespace
func (r *TrustyAIServiceReconciler) getImageFromConfigMap(ctx context.Context, key string, defaultImage string) (string, error) {
	if r.Namespace != "" {
		// Define the key for the custom ConfigMap
		configMapKeyCustom := types.NamespacedName{
			Namespace: r.Namespace,
			Name:      imageConfigMapCustom,
		}

		// define the key for the stock ConfigMap
		configMapKey := types.NamespacedName{
			Namespace: r.Namespace,
			Name:      imageConfigMap,
		}

		// Create an empty ConfigMap object
		var cm corev1.ConfigMap

		// Try to get the ConfigMap
		if err := r.Get(ctx, configMapKeyCustom, &cm); err != nil {
			if errors.IsNotFound(err) {
				// ConfigMap not found, fallback to operator installed values
				return r.getImageFromConfigMapStock(ctx, key, defaultImage)
			}
			// Other error occurred when trying to fetch the ConfigMap
			return defaultImage, fmt.Errorf("error reading configmap %s", configMapKeyCustom)
		}

		// ConfigMap is found, extract the image and tag
		image, ok := cm.Data[key]

		if !ok {
			// One or both of the keys are not present in the ConfigMap, return error
			image, defaultErr := r.getImageFromConfigMapStock(ctx, key, defaultImage)
			errInvalidKeys := fmt.Errorf("configmap %s does not contain necessary keys, failing back to %s", configMapKeyCustom, configMapKey)

			if defaultErr != nil {
				errInvalidKeys = fmt.Errorf("%w. While retrieving %s, another error occured: %w", errInvalidKeys, configMapKey, defaultErr)
			}
			return image, errInvalidKeys
		}

		// Return the image and tag
		return image, nil
	} else {
		return defaultImage, nil
	}
}

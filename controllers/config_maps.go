package controllers

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// getImageFromConfigMap gets a custom image value from a ConfigMap in the operator's namespace
func (r *TrustyAIServiceReconciler) getImageFromConfigMap(ctx context.Context, key string, defaultImage string) (string, error) {
	if r.Namespace != "" {
		// Define the key for the ConfigMap
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

// getKServeServerlessConfig checks the kServeServerless value in a ConfigMap in the operator's namespace
func (r *TrustyAIServiceReconciler) getKServeServerlessConfig(ctx context.Context) (bool, error) {

	if r.Namespace != "" {
		// Define the key for the ConfigMap
		configMapKey := types.NamespacedName{
			Namespace: r.Namespace,
			Name:      imageConfigMap,
		}

		// Create an empty ConfigMap object
		var cm corev1.ConfigMap

		// Try to get the ConfigMap
		if err := r.Get(ctx, configMapKey, &cm); err != nil {
			if errors.IsNotFound(err) {
				// ConfigMap not found, return false as the default behavior
				return false, nil
			}
			// Other error occurred when trying to fetch the ConfigMap
			return false, fmt.Errorf("error reading configmap %s", configMapKey)
		}

		// ConfigMap is found, extract the kServeServerless value
		kServeServerless, ok := cm.Data[configMapkServeServerlessKey]

		if !ok || kServeServerless != "enabled" {
			// Key is missing or its value is not "enabled", return false
			return false, nil
		}

		// kServeServerless is "enabled"
		return true, nil
	} else {
		return false, nil
	}
}

// getTLSConfig checks the tls value in a ConfigMap in the operator's namespace
func (r *TrustyAIServiceReconciler) getTLSConfig(ctx context.Context) (bool, error) {

	if r.Namespace != "" {
		// Define the key for the ConfigMap
		configMapKey := types.NamespacedName{
			Namespace: r.Namespace,
			Name:      imageConfigMap,
		}

		// Create an empty ConfigMap object
		var cm corev1.ConfigMap

		// Try to get the ConfigMap
		if err := r.Get(ctx, configMapKey, &cm); err != nil {
			if errors.IsNotFound(err) {
				// ConfigMap not found, return false as the default behavior
				return false, nil
			}
			// Other error occurred when trying to fetch the ConfigMap
			return false, fmt.Errorf("error reading configmap %s", configMapKey)
		}

		// ConfigMap is found, extract the tls value
		tls, ok := cm.Data[configMapTLSKey]

		if !ok || tls != "enabled" {
			// Key is missing or its value is not "enabled", return false
			return false, nil
		}

		// tls is "enabled"
		return true, nil
	} else {
		return false, nil
	}
}

// getConfigMapNamesWithLabel retrieves the names of ConfigMaps that have the specified label
func (r *TrustyAIServiceReconciler) getConfigMapNamesWithLabel(ctx context.Context, namespace string, labelSelector client.MatchingLabels) ([]string, error) {
	configMapList := &corev1.ConfigMapList{}

	// List ConfigMaps with the specified label selector
	err := r.Client.List(ctx, configMapList, client.InNamespace(namespace), labelSelector)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, cm := range configMapList.Items {
		names = append(names, cm.Name)
	}

	return names, nil
}

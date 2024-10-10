package controllers

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// KServe InferenceLogger config
type LoggerConfig struct {
	Image         string  `json:"image"`
	MemoryRequest string  `json:"memoryRequest"`
	MemoryLimit   string  `json:"memoryLimit"`
	CpuRequest    string  `json:"cpuRequest"`
	CpuLimit      string  `json:"cpuLimit"`
	DefaultUrl    string  `json:"defaultUrl"`
	CaBundle      *string `json:"caBundle,omitempty"`
	CaCertFile    *string `json:"caCertFile,omitempty"`
}

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

func (r *TrustyAIServiceReconciler) getKServeLoggerConfig(ctx context.Context) (*LoggerConfig, error) {
	if r.Namespace == "" {
		return nil, nil
	}

	configMapKey := types.NamespacedName{
		Namespace: r.Namespace,
		Name:      "inferenceservice-config",
	}

	var cm corev1.ConfigMap

	if err := r.Get(ctx, configMapKey, &cm); err != nil {
		if errors.IsNotFound(err) {
			// ConfigMap not found
			return nil, nil
		}
		return nil, fmt.Errorf("error reading configmap %s: %v", configMapKey, err)
	}

	loggerData, ok := cm.Data["logger"]
	if !ok {
		log.FromContext(ctx).Info("No KServe logger key found in inferenceservice-config")
		return nil, nil
	}

	var loggerConfig LoggerConfig
	err := json.Unmarshal([]byte(loggerData), &loggerConfig)
	if err != nil {
		return nil, fmt.Errorf("error parsing logger config: %v", err)
	}

	return &loggerConfig, nil
}

// createOrUpdateLoggerCaConfigMap creates or updates the KServe InferenceLogger CA bundle based on the KServe global configuration
func (r *TrustyAIServiceReconciler) createOrUpdateLoggerCaConfigMap(ctx context.Context, namespace string, loggerConfig *LoggerConfig) error {
	if loggerConfig.CaBundle == nil {
		return fmt.Errorf("loggerConfig.CaBundle is nil")
	}
	configMapName := *loggerConfig.CaBundle

	data := map[string]string{}
	if loggerConfig.CaCertFile != nil {
		data[*loggerConfig.CaCertFile] = ""
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: namespace,
			Annotations: map[string]string{
				"service.beta.openshift.io/inject-cabundle":      "true",
				"service.beta.openshift.io/inject-cabundle-name": *loggerConfig.CaCertFile,
			},
		},
		Data: data,
	}

	existingCM := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: namespace}, existingCM)
	if err != nil && errors.IsNotFound(err) {
		if err := r.Create(ctx, cm); err != nil {
			return fmt.Errorf("failed to create ConfigMap %s: %v", configMapName, err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to get ConfigMap %s: %v", configMapName, err)
	} else {
		existingCM.Data = cm.Data
		existingCM.Annotations = cm.Annotations
		if err := r.Update(ctx, existingCM); err != nil {
			return fmt.Errorf("failed to update ConfigMap %s: %v", configMapName, err)
		}
	}

	return nil
}

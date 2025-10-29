package utils

import (
	"context"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ConfigMapConfig struct {
	Owner   metav1.Object
	Name    string
	Version string
}

// GetConfigMapByName retrieves a configmap by name in the given namespace
func GetConfigMapByName(ctx context.Context, c client.Client, configMapName string, namespace string) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{}
	err := c.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: namespace}, configMap)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("configmap %s not found in namespace %s", configMapName, namespace)
		}
		return nil, fmt.Errorf("error reading configmap %s in namespace %s: %w", configMapName, namespace, err)
	}
	return configMap, nil
}

// GetImageFromConfigMapWithFallback retrieves a value from a ConfigMap by key, with a fallback value if no configmap is found or the key does not exist
func GetImageFromConfigMapWithFallback(ctx context.Context, c client.Client, configMapKey, configMapName, namespace string, fallbackValue string) (string, error) {
	if namespace == "" {
		return fallbackValue, nil
	}

	image, err := GetImageFromConfigMap(ctx, c, configMapKey, configMapName, namespace)
	if err != nil {
		return fallbackValue, err
	} else {
		return image, nil
	}
}

// GetImageFromConfigMap retrieves a value from a ConfigMap by key.
// Can be used by any reconciler by passing in the controller-runtime client.
func GetImageFromConfigMap(ctx context.Context, c client.Client, configMapKey string, configMapName string, namespace string) (string, error) {
	configMap, err := GetConfigMapByName(ctx, c, configMapName, namespace)
	if err != nil {
		return "", err
	}
	value, ok := configMap.Data[configMapKey]
	if !ok {
		return "", fmt.Errorf("configmap %s in namespace %s does not contain key %s", configMapName, namespace, configMapKey)
	}
	return value, nil
}

// CreateConfigMap will create a ConfigMap object in the owner's namespace, but does not deploy anything to the cluster
func CreateConfigMap(ctx context.Context, c client.Client, owner metav1.Object, configMapName string, version string, configMapTemplatePath string, parser ResourceParserFunc[corev1.ConfigMap]) (*corev1.ConfigMap, error) {
	configMapConfig := ConfigMapConfig{
		Owner:   owner,
		Name:    configMapName,
		Version: version,
	}
	var configMap *corev1.ConfigMap
	configMap, err := parser(configMapTemplatePath, configMapConfig, reflect.TypeOf(&corev1.ConfigMap{}))

	if err != nil {
		log.FromContext(ctx).Error(err, "failed to parse configmap template")
		return nil, err
	}
	err = controllerutil.SetControllerReference(owner, configMap, c.Scheme())
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to set controller reference")
		return nil, err
	}
	return configMap, nil
}

// ReconcileConfigMap holds reconciliation logic for a generic ConfigMap in the owner's namespace.
// Returns the created/found configmap, a boolean flag indicating whether the return configmap was created during this function, and any errors
func ReconcileConfigMap(ctx context.Context, c client.Client, owner metav1.Object, configMapName string, version string, templatePath string, parserFunc ResourceParserFunc[corev1.ConfigMap]) (*corev1.ConfigMap, bool, error) {
	existingConfigMap := &corev1.ConfigMap{}
	err := c.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: owner.GetNamespace()}, existingConfigMap)
	if err != nil && errors.IsNotFound(err) {
		// Define a new configmap
		log.FromContext(ctx).Info("creating configmap", "name", configMapName, "namespace", owner.GetNamespace())
		cm, err := CreateConfigMap(ctx, c, owner, configMapName, version, templatePath, parserFunc)
		if err != nil {
			log.FromContext(ctx).Error(err, "Failed to define configmap", "configmap", owner.GetName(), "namespace", owner.GetNamespace())
			return nil, false, err
		}
		log.FromContext(ctx).Info("Creating a new ConfigMap", "ConfigMap.Namespace", cm.Namespace, "ConfigMap.Name", cm.Name)
		err = c.Create(ctx, cm)
		if err != nil {
			log.FromContext(ctx).Error(err, "Failed to create new ConfigMap", "ConfigMap.Namespace", cm.Namespace, "ConfigMap.Name", cm.Name)
			return nil, false, err
		}
		return cm, true, nil
	} else if err != nil {
		log.FromContext(ctx).Error(err, "Failed to get ConfigMap")
		return nil, false, err
	} else {
		return existingConfigMap, false, nil
	}
}

// MountConfigMapToDeployment adds a volume to a deployment spec
func MountConfigMapToDeployment(configMap *corev1.ConfigMap, volumeName string, deployment *appsv1.Deployment) {
	volume := corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: configMap.Name,
				},
			},
		},
	}
	deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, volume)
}

// Compare a provided configmap against an expectedConfigMap, and update the provided one if its data or labels do not match the expected CM
func compareAndUpdateConfigmap(ctx context.Context, c client.Client, configMap *corev1.ConfigMap, expectedConfigMap *corev1.ConfigMap) error {
	// Compare the data content to see if update is needed
	if !reflect.DeepEqual(configMap.Data, expectedConfigMap.Data) ||
		!reflect.DeepEqual(configMap.Labels, expectedConfigMap.Labels) {
		log.FromContext(ctx).Info("Updating configMap", "name", configMap.Name, "namespace", configMap.Namespace)

		// Update the existing ConfigMap with new data and labels
		configMap.Data = expectedConfigMap.Data
		configMap.Labels = expectedConfigMap.Labels

		if err := c.Update(ctx, configMap); err != nil {
			log.FromContext(ctx).Error(err, "Error updating ConfigMap", "name", configMap.Name, "namespace", configMap.Namespace)
			return err
		}
	} else {
		log.FromContext(ctx).V(1).Info("ConfigMap is up to date", "name", configMap.Name, "namespace", configMap.Namespace)
	}
	return nil
}

// EnsureConfigMap checks that a configmap in the cluster matches its expected state, where the expected state is checked by initializing a new instance of the configmap via the template parser
func EnsureConfigMap(ctx context.Context, c client.Client, owner metav1.Object, configMapName string, version string, templatePath string, parserFunc ResourceParserFunc[corev1.ConfigMap]) error {
	configMap, justCreated, err := ReconcileConfigMap(ctx, c, owner, configMapName, version, templatePath, parserFunc)
	if err != nil {
		log.FromContext(ctx).Error(err, "Error retrieving ConfigMap", "name", configMapName)
		return err
	}
	if justCreated {
		return nil // if we just created the configmap in utils.ReconcileConfigMap, we know it is correctly-valued;
	}

	// ConfigMap exists, check if update is needed by comparing data
	expectedConfigMap, createErr := CreateConfigMap(ctx, c, owner, configMapName, version, templatePath, parserFunc)
	if createErr != nil {
		log.FromContext(ctx).Error(createErr, "Error creating expected ConfigMap for comparison", "name", configMapName)
		return createErr
	}

	// check if update is needed by comparing data
	if err = compareAndUpdateConfigmap(ctx, c, configMap, expectedConfigMap); err != nil {
		return err
	}
	return nil
}

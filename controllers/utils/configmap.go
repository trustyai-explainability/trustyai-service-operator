package utils

import (
	"context"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
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

const configMapResourceKind = "configmap"

// === GENERIC FUNCTIONS ===============================================================================================

// DefineConfigMap will create a ConfigMap object in the owner's namespace, but does not deploy anything to the cluster
func DefineConfigMap(ctx context.Context, c client.Client, owner metav1.Object, configMapName string, version string, configMapTemplatePath string, parser ResourceParserFunc[*corev1.ConfigMap]) (*corev1.ConfigMap, error) {
	configMapConfig := ConfigMapConfig{
		Owner:   owner,
		Name:    configMapName,
		Version: version,
	}
	genericConfig := GetGenericConfig(StringPointer(configMapName), StringPointer(owner.GetNamespace()), configMapConfig)
	return DefineGeneric[*corev1.ConfigMap](ctx, c, owner, configMapResourceKind, genericConfig, configMapTemplatePath, parser)
}

// ReconcileConfigMap holds reconciliation logic for a generic ConfigMap in the owner's namespace.
// Returns the created/found configmap, a boolean flag indicating whether the return configmap was created during this function, and any errors
func ReconcileConfigMap(ctx context.Context, c client.Client, owner metav1.Object, configMapName string, version string, templatePath string, parserFunc ResourceParserFunc[*corev1.ConfigMap]) (*corev1.ConfigMap, bool, error) {
	configMapConfig := ConfigMapConfig{
		Owner:   owner,
		Name:    configMapName,
		Version: version,
	}
	genericConfig := GetGenericConfig(StringPointer(configMapName), StringPointer(owner.GetNamespace()), configMapConfig)
	return ReconcileGeneric[*corev1.ConfigMap](ctx, c, owner, configMapResourceKind, genericConfig, templatePath, parserFunc)
}

// === SPECIFIC CONFIGMAP FUNCTIONS ====================================================================================
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
		LogInfoUpdating(ctx, configMapResourceKind, configMap.Name, configMap.Namespace)

		// Update the existing ConfigMap with new data and labels
		configMap.Data = expectedConfigMap.Data
		configMap.Labels = expectedConfigMap.Labels

		if err := c.Update(ctx, configMap); err != nil {
			LogErrorUpdating(ctx, err, configMapResourceKind, configMap.Name, configMap.Namespace)
			return err
		}
	} else {
		log.FromContext(ctx).V(1).Info(fmt.Sprintf("configmap %s in namespace %s is up to date", configMap.Name, configMap.Namespace))
	}
	return nil
}

// EnsureConfigMap checks that a configmap in the cluster matches its expected state, where the expected state is checked by initializing a new instance of the configmap via the template parser
func EnsureConfigMap(ctx context.Context, c client.Client, owner metav1.Object, configMapName string, version string, templatePath string, parserFunc ResourceParserFunc[*corev1.ConfigMap]) error {
	configMap, justCreated, err := ReconcileConfigMap(ctx, c, owner, configMapName, version, templatePath, parserFunc)
	if err != nil {
		LogErrorRetrieving(ctx, err, configMapResourceKind, configMapName, owner.GetNamespace())
		return err
	}
	if justCreated {
		return nil // if we just created the configmap in utils.ReconcileConfigMap, we know it is correctly-valued;
	}

	// ConfigMap exists, check if update is needed by comparing data
	expectedConfigMap, err := DefineConfigMap(ctx, c, owner, configMapName, version, templatePath, parserFunc)
	if err != nil {
		LogErrorCreating(ctx, err, "expected comparison "+configMapResourceKind, configMapName, owner.GetNamespace())
		return err
	}

	// check if update is needed by comparing data
	if err = compareAndUpdateConfigmap(ctx, c, configMap, expectedConfigMap); err != nil {
		return err
	}
	return nil
}

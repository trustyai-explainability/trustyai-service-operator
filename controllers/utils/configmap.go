package utils

import (
	"context"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ConfigMapConfig struct {
	Owner         metav1.Object
	ConfigMapName string
}

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

func createConfigMap(ctx context.Context, c client.Client, owner metav1.Object, configMapName string, configMapTemplatePath string, parser ResourceParserFunc[corev1.ConfigMap]) (*corev1.ConfigMap, error) {
	configMapConfig := ConfigMapConfig{
		Owner:         owner,
		ConfigMapName: configMapName,
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

func GetConfigMapByName(ctx context.Context, c client.Client, configMapName, namespace string) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{}
	err := c.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: namespace}, configMap)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("could not find configmap %s in namespace %s: %w", configMapName, namespace, err)
		}
		return nil, fmt.Errorf("error reading configmap %s in namespace %s: %w", configMapName, namespace, err)
	}
	return configMap, nil
}

func ReconcileConfigMap(ctx context.Context, c client.Client, owner metav1.Object, configMapName string, templatePath string, parserFunc ResourceParserFunc[corev1.ConfigMap]) (ctrl.Result, error) {
	existingConfigMap := &corev1.ConfigMap{}
	err := c.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: owner.GetNamespace()}, existingConfigMap)
	if err != nil && errors.IsNotFound(err) {
		// Define a new configmap
		cm, err := createConfigMap(ctx, c, owner, configMapName, templatePath, parserFunc)
		if err != nil {
			log.FromContext(ctx).Error(err, "Failed to define configmap", "configmap", owner.GetName(), "namespace", owner.GetNamespace())
			return ctrl.Result{}, err
		}
		log.FromContext(ctx).Info("Creating a new ConfigMap", "ConfigMap.Namespace", cm.Namespace, "ConfigMap.Name", cm.Name)
		err = c.Create(ctx, cm)
		if err != nil {
			log.FromContext(ctx).Error(err, "Failed to create new ConfigMap", "ConfigMap.Namespace", cm.Namespace, "ConfigMap.Name", cm.Name)
			return ctrl.Result{}, err
		}
	} else if err != nil {
		log.FromContext(ctx).Error(err, "Failed to get ConfigMap")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func MountConfigMapToDeployment(configMap *corev1.ConfigMap, volumeName string, deployment *appsv1.Deployment) {
	// Add the volume to the deployment spec
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

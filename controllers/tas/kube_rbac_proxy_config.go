package tas

import (
	"context"
	"reflect"

	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/tas/templates"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const kubeRBACProxyConfigTemplatePath = "kube-rbac-proxy/config.tmpl.yaml"

// KubeRBACProxyConfigMapConfig holds the data needed for the kube-rbac-proxy ConfigMap template
type KubeRBACProxyConfigMapConfig struct {
	Instance *trustyaiopendatahubiov1alpha1.TrustyAIService
	Version  string
}

// createKubeRBACProxyConfigMapObject creates the ConfigMap object from template
func (r *TrustyAIServiceReconciler) createKubeRBACProxyConfigMapObject(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) (*corev1.ConfigMap, error) {
	config := KubeRBACProxyConfigMapConfig{
		Instance: instance,
		Version:  constants.Version,
	}

	var configMap *corev1.ConfigMap
	configMap, err := templateParser.ParseResource[corev1.ConfigMap](
		kubeRBACProxyConfigTemplatePath,
		config,
		reflect.TypeOf(&corev1.ConfigMap{}),
	)
	if err != nil {
		log.FromContext(ctx).Error(err, "Error parsing kube-rbac-proxy ConfigMap template")
		return nil, err
	}

	return configMap, nil
}

// ensureKubeRBACProxyConfigMap ensures the kube-rbac-proxy ConfigMap exists
func (r *TrustyAIServiceReconciler) ensureKubeRBACProxyConfigMap(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) error {
	configMapName := instance.Name + "-kube-rbac-proxy-config"

	// Check if ConfigMap already exists
	existingConfigMap := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: instance.Namespace}, existingConfigMap)
	if err != nil {
		if errors.IsNotFound(err) {
			// ConfigMap doesn't exist, create it
			configMap, createErr := r.createKubeRBACProxyConfigMapObject(ctx, instance)
			if createErr != nil {
				return createErr
			}

			if err := ctrl.SetControllerReference(instance, configMap, r.Scheme); err != nil {
				log.FromContext(ctx).Error(err, "Error setting TrustyAIService as owner of kube-rbac-proxy ConfigMap")
				return err
			}

			log.FromContext(ctx).Info("Creating kube-rbac-proxy ConfigMap", "name", configMapName, "namespace", instance.Namespace)
			if err := r.Create(ctx, configMap); err != nil {
				log.FromContext(ctx).Error(err, "Error creating kube-rbac-proxy ConfigMap")
				return err
			}
			return nil
		}
		// Other error occurred
		return err
	}

	// ConfigMap exists, check if update is needed by comparing data
	expectedConfigMap, createErr := r.createKubeRBACProxyConfigMapObject(ctx, instance)
	if createErr != nil {
		log.FromContext(ctx).Error(createErr, "Error creating expected kube-rbac-proxy ConfigMap for comparison")
		return createErr
	}

	// Compare the data content to see if update is needed
	if !reflect.DeepEqual(existingConfigMap.Data, expectedConfigMap.Data) ||
		!reflect.DeepEqual(existingConfigMap.Labels, expectedConfigMap.Labels) {
		log.FromContext(ctx).Info("Updating kube-rbac-proxy ConfigMap", "name", configMapName, "namespace", instance.Namespace)

		// Update the existing ConfigMap with new data and labels
		existingConfigMap.Data = expectedConfigMap.Data
		existingConfigMap.Labels = expectedConfigMap.Labels

		if err := r.Update(ctx, existingConfigMap); err != nil {
			log.FromContext(ctx).Error(err, "Error updating kube-rbac-proxy ConfigMap")
			return err
		}
	} else {
		log.FromContext(ctx).V(1).Info("kube-rbac-proxy ConfigMap is up to date", "name", configMapName, "namespace", instance.Namespace)
	}

	return nil
}

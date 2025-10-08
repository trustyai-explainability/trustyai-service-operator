package gorch

import (
	"context"
	"reflect"

	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/gorch/templates"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const orchestratorKubeRBACProxyConfigTemplatePath = "kube-rbac-proxy/orchestrator-config.tmpl.yaml"
const gatewayKubeRBACProxyConfigTemplatePath = "kube-rbac-proxy/gateway-config.tmpl.yaml"

// KubeRBACProxyConfigMapConfig holds the data needed for the kube-rbac-proxy ConfigMap template
type KubeRBACProxyConfigMapConfig struct {
	Orchestrator *gorchv1alpha1.GuardrailsOrchestrator
}

// createOrchestratorKubeRBACProxyConfigMapObject creates the orchestrator ConfigMap object from template
func (r *GuardrailsOrchestratorReconciler) createOrchestratorKubeRBACProxyConfigMapObject(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) (*corev1.ConfigMap, error) {
	config := KubeRBACProxyConfigMapConfig{
		Orchestrator: orchestrator,
	}

	var configMap *corev1.ConfigMap
	configMap, err := templateParser.ParseResource[corev1.ConfigMap](
		orchestratorKubeRBACProxyConfigTemplatePath,
		config,
		reflect.TypeOf(&corev1.ConfigMap{}),
	)
	if err != nil {
		log.FromContext(ctx).Error(err, "Error parsing orchestrator kube-rbac-proxy ConfigMap template")
		return nil, err
	}

	return configMap, nil
}

// createGatewayKubeRBACProxyConfigMapObject creates the gateway ConfigMap object from template
func (r *GuardrailsOrchestratorReconciler) createGatewayKubeRBACProxyConfigMapObject(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) (*corev1.ConfigMap, error) {
	config := KubeRBACProxyConfigMapConfig{
		Orchestrator: orchestrator,
	}

	var configMap *corev1.ConfigMap
	configMap, err := templateParser.ParseResource[corev1.ConfigMap](
		gatewayKubeRBACProxyConfigTemplatePath,
		config,
		reflect.TypeOf(&corev1.ConfigMap{}),
	)
	if err != nil {
		log.FromContext(ctx).Error(err, "Error parsing gateway kube-rbac-proxy ConfigMap template")
		return nil, err
	}

	return configMap, nil
}

// ensureOrchestratorKubeRBACProxyConfigMap ensures the orchestrator kube-rbac-proxy ConfigMap exists
func (r *GuardrailsOrchestratorReconciler) ensureOrchestratorKubeRBACProxyConfigMap(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) error {
	configMapName := orchestrator.Name + "-kube-rbac-proxy-orchestrator-config"

	// Check if ConfigMap already exists
	existingConfigMap := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: orchestrator.Namespace}, existingConfigMap)
	if err != nil {
		if errors.IsNotFound(err) {
			// ConfigMap doesn't exist, create it
			configMap, createErr := r.createOrchestratorKubeRBACProxyConfigMapObject(ctx, orchestrator)
			if createErr != nil {
				return createErr
			}

			if err := ctrl.SetControllerReference(orchestrator, configMap, r.Scheme); err != nil {
				log.FromContext(ctx).Error(err, "Error setting GuardrailsOrchestrator as owner of orchestrator kube-rbac-proxy ConfigMap")
				return err
			}

			log.FromContext(ctx).Info("Creating orchestrator kube-rbac-proxy ConfigMap", "name", configMapName, "namespace", orchestrator.Namespace)
			if err := r.Create(ctx, configMap); err != nil {
				log.FromContext(ctx).Error(err, "Error creating orchestrator kube-rbac-proxy ConfigMap")
				return err
			}
			return nil
		}
		// Other error occurred
		return err
	}

	// ConfigMap exists, check if update is needed by comparing data
	expectedConfigMap, createErr := r.createOrchestratorKubeRBACProxyConfigMapObject(ctx, orchestrator)
	if createErr != nil {
		log.FromContext(ctx).Error(createErr, "Error creating expected orchestrator kube-rbac-proxy ConfigMap for comparison")
		return createErr
	}

	// Compare the data content to see if update is needed
	if !reflect.DeepEqual(existingConfigMap.Data, expectedConfigMap.Data) ||
		!reflect.DeepEqual(existingConfigMap.Labels, expectedConfigMap.Labels) {
		log.FromContext(ctx).Info("Updating orchestrator kube-rbac-proxy ConfigMap", "name", configMapName, "namespace", orchestrator.Namespace)

		// Update the existing ConfigMap with new data and labels
		existingConfigMap.Data = expectedConfigMap.Data
		existingConfigMap.Labels = expectedConfigMap.Labels

		if err := r.Update(ctx, existingConfigMap); err != nil {
			log.FromContext(ctx).Error(err, "Error updating orchestrator kube-rbac-proxy ConfigMap")
			return err
		}
	} else {
		log.FromContext(ctx).V(1).Info("Orchestrator kube-rbac-proxy ConfigMap is up to date", "name", configMapName, "namespace", orchestrator.Namespace)
	}

	return nil
}

// ensureGatewayKubeRBACProxyConfigMap ensures the gateway kube-rbac-proxy ConfigMap exists
func (r *GuardrailsOrchestratorReconciler) ensureGatewayKubeRBACProxyConfigMap(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) error {
	configMapName := orchestrator.Name + "-kube-rbac-proxy-gateway-config"

	// Check if ConfigMap already exists
	existingConfigMap := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: orchestrator.Namespace}, existingConfigMap)
	if err != nil {
		if errors.IsNotFound(err) {
			// ConfigMap doesn't exist, create it
			configMap, createErr := r.createGatewayKubeRBACProxyConfigMapObject(ctx, orchestrator)
			if createErr != nil {
				return createErr
			}

			if err := ctrl.SetControllerReference(orchestrator, configMap, r.Scheme); err != nil {
				log.FromContext(ctx).Error(err, "Error setting GuardrailsOrchestrator as owner of gateway kube-rbac-proxy ConfigMap")
				return err
			}

			log.FromContext(ctx).Info("Creating gateway kube-rbac-proxy ConfigMap", "name", configMapName, "namespace", orchestrator.Namespace)
			if err := r.Create(ctx, configMap); err != nil {
				log.FromContext(ctx).Error(err, "Error creating gateway kube-rbac-proxy ConfigMap")
				return err
			}
			return nil
		}
		// Other error occurred
		return err
	}

	// ConfigMap exists, check if update is needed by comparing data
	expectedConfigMap, createErr := r.createGatewayKubeRBACProxyConfigMapObject(ctx, orchestrator)
	if createErr != nil {
		log.FromContext(ctx).Error(createErr, "Error creating expected gateway kube-rbac-proxy ConfigMap for comparison")
		return createErr
	}

	// Compare the data content to see if update is needed
	if !reflect.DeepEqual(existingConfigMap.Data, expectedConfigMap.Data) ||
		!reflect.DeepEqual(existingConfigMap.Labels, expectedConfigMap.Labels) {
		log.FromContext(ctx).Info("Updating gateway kube-rbac-proxy ConfigMap", "name", configMapName, "namespace", orchestrator.Namespace)

		// Update the existing ConfigMap with new data and labels
		existingConfigMap.Data = expectedConfigMap.Data
		existingConfigMap.Labels = expectedConfigMap.Labels

		if err := r.Update(ctx, existingConfigMap); err != nil {
			log.FromContext(ctx).Error(err, "Error updating gateway kube-rbac-proxy ConfigMap")
			return err
		}
	} else {
		log.FromContext(ctx).V(1).Info("Gateway kube-rbac-proxy ConfigMap is up to date", "name", configMapName, "namespace", orchestrator.Namespace)
	}

	return nil
}

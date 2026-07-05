package module

import (
	"context"
	"fmt"
	"strconv"

	modulev1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/module/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// reconcileConfigMap creates or updates the DSC ConfigMap with eval settings from the module spec
func (r *TrustyAIReconciler) reconcileConfigMap(ctx context.Context, module *modulev1alpha1.TrustyAI) error {
	logger := log.FromContext(ctx)

	// Build desired ConfigMap
	desired := r.buildDSCConfigMap(module)

	// Check if ConfigMap already exists
	existing := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      DSCConfigMapName,
		Namespace: r.Namespace,
	}, existing)

	if err != nil {
		if errors.IsNotFound(err) {
			// Create new ConfigMap
			logger.Info("Creating DSC ConfigMap", "name", DSCConfigMapName, "namespace", r.Namespace)
			if err := r.Create(ctx, desired); err != nil {
				return fmt.Errorf("failed to create DSC ConfigMap: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to get DSC ConfigMap: %w", err)
	}

	// Update existing ConfigMap if data has changed
	if !configMapDataEqual(existing.Data, desired.Data) {
		logger.Info("Updating DSC ConfigMap", "name", DSCConfigMapName, "namespace", r.Namespace)
		existing.Data = desired.Data
		if err := r.Update(ctx, existing); err != nil {
			return fmt.Errorf("failed to update DSC ConfigMap: %w", err)
		}
	}

	return nil
}

// buildDSCConfigMap constructs the DSC ConfigMap from the module spec
func (r *TrustyAIReconciler) buildDSCConfigMap(module *modulev1alpha1.TrustyAI) *corev1.ConfigMap {
	data := make(map[string]string)

	// Convert bool values to string
	data[LMEvalPermitCodeExecutionKey] = strconv.FormatBool(module.Spec.Eval.LMEval.PermitCodeExecution)
	data[LMEvalPermitOnlineKey] = strconv.FormatBool(module.Spec.Eval.LMEval.PermitOnline)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DSCConfigMapName,
			Namespace: r.Namespace,
		},
		Data: data,
	}

	// Set owner reference for garbage collection
	// Note: Module CR is cluster-scoped, ConfigMap is namespaced
	// We cannot set cross-namespace owner references, so we'll handle cleanup manually
	// via finalizer instead

	return cm
}

// deleteDSCConfigMap deletes the DSC ConfigMap during finalizer cleanup
func (r *TrustyAIReconciler) deleteDSCConfigMap(ctx context.Context) error {
	logger := log.FromContext(ctx)

	cm := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      DSCConfigMapName,
		Namespace: r.Namespace,
	}, cm)

	if err != nil {
		if errors.IsNotFound(err) {
			// ConfigMap already deleted, nothing to do
			return nil
		}
		return fmt.Errorf("failed to get DSC ConfigMap for deletion: %w", err)
	}

	logger.Info("Deleting DSC ConfigMap", "name", DSCConfigMapName, "namespace", r.Namespace)
	if err := r.Delete(ctx, cm); err != nil {
		return fmt.Errorf("failed to delete DSC ConfigMap: %w", err)
	}

	return nil
}

// configMapDataEqual compares two ConfigMap data maps
func configMapDataEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}

	for k, v := range a {
		if b[k] != v {
			return false
		}
	}

	return true
}

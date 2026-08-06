package trustyaimodule

import (
	"context"
	"fmt"
	"strconv"

	platformv1alpha1 "github.com/trustyai-explainability/trustyai-operator-module/pkg/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *TrustyAIModuleReconciler) reconcileConfigMap(ctx context.Context, module *platformv1alpha1.TrustyAI) error {
	logger := log.FromContext(ctx)

	desired := r.buildDSCConfigMap(module)

	existing := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      DSCConfigMapName,
		Namespace: r.Namespace,
	}, existing)

	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Creating DSC ConfigMap", "name", DSCConfigMapName, "namespace", r.Namespace)
			if err := r.Create(ctx, desired); err != nil {
				r.EventRecorder.Event(module, "Warning", "ConfigMapCreateFailed", "Failed to create DSC ConfigMap")
				return fmt.Errorf("failed to create DSC ConfigMap: %w", err)
			}
			r.EventRecorder.Event(module, "Normal", "ConfigMapCreated", "DSC ConfigMap created successfully")
			return nil
		}
		return fmt.Errorf("failed to get DSC ConfigMap: %w", err)
	}

	if !configMapDataEqual(existing.Data, desired.Data) {
		logger.Info("Updating DSC ConfigMap", "name", DSCConfigMapName, "namespace", r.Namespace)
		existing.Data = desired.Data
		if err := r.Update(ctx, existing); err != nil {
			r.EventRecorder.Event(module, "Warning", "ConfigMapUpdateFailed", "Failed to update DSC ConfigMap")
			return fmt.Errorf("failed to update DSC ConfigMap: %w", err)
		}
		r.EventRecorder.Event(module, "Normal", "ConfigMapUpdated", "DSC ConfigMap updated successfully")
	}

	return nil
}

func (r *TrustyAIModuleReconciler) buildDSCConfigMap(module *platformv1alpha1.TrustyAI) *corev1.ConfigMap {
	data := map[string]string{
		LMEvalPermitCodeExecutionKey: strconv.FormatBool(module.Spec.Eval.LMEval.PermitCodeExecution),
		LMEvalPermitOnlineKey:        strconv.FormatBool(module.Spec.Eval.LMEval.PermitOnline),
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DSCConfigMapName,
			Namespace: r.Namespace,
		},
		Data: data,
	}
}

func (r *TrustyAIModuleReconciler) deleteDSCConfigMap(ctx context.Context) error {
	logger := log.FromContext(ctx)

	cm := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      DSCConfigMapName,
		Namespace: r.Namespace,
	}, cm)

	if err != nil {
		if errors.IsNotFound(err) {
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

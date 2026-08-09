package trustyaimodule

import (
	"context"
	"fmt"
	"maps"
	"strconv"

	platformv1alpha1 "github.com/trustyai-explainability/trustyai-operator-module/pkg/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// reconcileWorkloadConfigMap creates or updates the trustyai-service-operator-config
// ConfigMap in the applications namespace with the user-configurable settings from the
// TrustyAI spec. Image overrides are kept in the static manifests-template ConfigMap;
// only the operator-behaviour keys are managed here.
func (r *TrustyAIModuleReconciler) reconcileWorkloadConfigMap(ctx context.Context, module *platformv1alpha1.TrustyAI) error {
	logger := log.FromContext(ctx)

	desired := r.buildWorkloadConfigMap(module)

	existing := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      WorkloadConfigMapName,
		Namespace: r.ApplicationsNamespace,
	}, existing)

	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Creating workload ConfigMap", "name", WorkloadConfigMapName, "namespace", r.ApplicationsNamespace)
			if createErr := r.Create(ctx, desired); createErr != nil {
				r.EventRecorder.Event(module, "Warning", "ConfigMapCreateFailed", "Failed to create workload ConfigMap")
				return fmt.Errorf("failed to create workload ConfigMap: %w", createErr)
			}
			r.EventRecorder.Event(module, "Normal", "ConfigMapCreated", "Workload ConfigMap created successfully")
			return nil
		}
		return fmt.Errorf("failed to get workload ConfigMap: %w", err)
	}

	merged := mergeConfigMapData(existing.Data, desired.Data)
	if !configMapDataEqual(existing.Data, merged) {
		logger.Info("Updating workload ConfigMap", "name", WorkloadConfigMapName, "namespace", r.ApplicationsNamespace)
		existing.Data = merged
		if updateErr := r.Update(ctx, existing); updateErr != nil {
			r.EventRecorder.Event(module, "Warning", "ConfigMapUpdateFailed", "Failed to update workload ConfigMap")
			return fmt.Errorf("failed to update workload ConfigMap: %w", updateErr)
		}
		r.EventRecorder.Event(module, "Normal", "ConfigMapUpdated", "Workload ConfigMap updated successfully")
	}

	return nil
}

// buildWorkloadConfigMap constructs the user-configurable portion of the workload
// operator ConfigMap from the TrustyAI CR spec. Image keys are managed by the
// static manifests-template overlay and must not be overwritten here.
func (r *TrustyAIModuleReconciler) buildWorkloadConfigMap(module *platformv1alpha1.TrustyAI) *corev1.ConfigMap {
	lme := module.Spec.Eval.LMEval

	kServeVal := "disabled"
	if module.Spec.KServeServerless {
		kServeVal = "enabled"
	}

	maxBatch := lme.MaxBatchSize
	if maxBatch == 0 {
		maxBatch = 24
	}
	defaultBatch := lme.DefaultBatchSize
	if defaultBatch == 0 {
		defaultBatch = 8
	}
	pullPolicy := lme.ImagePullPolicy
	if pullPolicy == "" {
		pullPolicy = "Always"
	}

	data := map[string]string{
		WorkloadKeyKServeServerless:       kServeVal,
		WorkloadKeyLMEvalPermitCodeExec:   strconv.FormatBool(lme.PermitCodeExecution),
		WorkloadKeyLMEvalPermitOnline:     strconv.FormatBool(lme.PermitOnline),
		WorkloadKeyLMEvalMaxBatchSize:     strconv.Itoa(maxBatch),
		WorkloadKeyLMEvalDefaultBatchSize: strconv.Itoa(defaultBatch),
		WorkloadKeyLMEvalDetectDevice:     strconv.FormatBool(lme.DetectDevice),
		WorkloadKeyLMEvalImagePullPolicy:  pullPolicy,
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      WorkloadConfigMapName,
			Namespace: r.ApplicationsNamespace,
		},
		Data: data,
	}
}

// mergeConfigMapData merges spec-derived keys (from desired) into the existing map,
// leaving image-override keys (managed by the static manifests) untouched.
func mergeConfigMapData(existing, desired map[string]string) map[string]string {
	merged := make(map[string]string, len(existing)+len(desired))
	maps.Copy(merged, existing)
	maps.Copy(merged, desired)
	return merged
}

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

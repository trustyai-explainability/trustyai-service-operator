package trustyaimodule

import (
	"context"
	stderrors "errors"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// DependencyCheckResult represents the result of a dependency check
type DependencyCheckResult struct {
	Name      string
	Satisfied bool
	Message   string
}

func (r *TrustyAIModuleReconciler) checkDependencies(ctx context.Context) ([]DependencyCheckResult, error) {
	logger := log.FromContext(ctx)
	logger.Info("Checking platform dependencies")

	var results []DependencyCheckResult

	inferenceServiceResult, err := r.checkInferenceService(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check InferenceService: %w", err)
	}
	results = append(results, inferenceServiceResult)

	monitoringResult, err := r.checkMonitoring(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check Monitoring: %w", err)
	}
	results = append(results, monitoringResult)

	return results, nil
}

func (r *TrustyAIModuleReconciler) checkInferenceService(ctx context.Context) (DependencyCheckResult, error) {
	logger := log.FromContext(ctx)

	result := DependencyCheckResult{Name: "InferenceService"}

	gvk := schema.GroupVersionKind{
		Group:   "serving.kserve.io",
		Version: "v1beta1",
		Kind:    "InferenceService",
	}

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk)

	if err := r.List(ctx, list); err != nil {
		if errors.IsNotFound(err) || isNoMatchError(err) {
			result.Satisfied = false
			result.Message = "InferenceService CRD not found (KServe is not installed)"
			logger.Info("InferenceService dependency not satisfied", "reason", result.Message)
			return result, nil
		}
		return result, fmt.Errorf("failed to list InferenceService: %w", err)
	}

	result.Satisfied = true
	result.Message = "InferenceService CRD is available"
	logger.Info("InferenceService dependency satisfied")
	return result, nil
}

func (r *TrustyAIModuleReconciler) checkMonitoring(ctx context.Context) (DependencyCheckResult, error) {
	logger := log.FromContext(ctx)

	result := DependencyCheckResult{Name: "Monitoring"}

	gvk := schema.GroupVersionKind{
		Group:   "monitoring.coreos.com",
		Version: "v1",
		Kind:    "Prometheus",
	}

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk)

	if err := r.List(ctx, list); err != nil {
		if errors.IsNotFound(err) || isNoMatchError(err) {
			result.Satisfied = false
			result.Message = "Monitoring is not installed (Prometheus CRD not found)"
			logger.Info("Monitoring dependency not satisfied", "reason", result.Message)
			return result, nil
		}
		return result, fmt.Errorf("failed to list Prometheus: %w", err)
	}

	if len(list.Items) == 0 {
		result.Satisfied = false
		result.Message = "Prometheus CRD found but no Prometheus instance exists"
		logger.Info("Monitoring dependency not satisfied", "reason", result.Message)
		return result, nil
	}

	result.Satisfied = true
	result.Message = fmt.Sprintf("Monitoring is available (%d Prometheus instance(s) found)", len(list.Items))
	logger.Info("Monitoring dependency satisfied", "instances", len(list.Items))
	return result, nil
}

func isNoMatchError(err error) bool {
	if err == nil {
		return false
	}
	var noKindMatchErr *meta.NoKindMatchError
	if stderrors.As(err, &noKindMatchErr) {
		return true
	}
	return errors.IsNotFound(err)
}

func allDependenciesSatisfied(results []DependencyCheckResult) bool {
	for _, result := range results {
		if !result.Satisfied {
			return false
		}
	}
	return true
}

func formatDependencyMessages(results []DependencyCheckResult) string {
	var unsatisfied []string
	for _, result := range results {
		if !result.Satisfied {
			unsatisfied = append(unsatisfied, fmt.Sprintf("%s: %s", result.Name, result.Message))
		}
	}

	if len(unsatisfied) == 0 {
		return "All dependencies satisfied"
	}

	msg := "Missing dependencies: "
	for i, dep := range unsatisfied {
		if i > 0 {
			msg += "; "
		}
		msg += dep
	}
	return msg
}

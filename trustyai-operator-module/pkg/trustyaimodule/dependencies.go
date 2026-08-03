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

	serviceMeshResult, err := r.checkServiceMesh(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check Service Mesh: %w", err)
	}
	results = append(results, serviceMeshResult)

	monitoringResult, err := r.checkMonitoring(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check Monitoring: %w", err)
	}
	results = append(results, monitoringResult)

	return results, nil
}

func (r *TrustyAIModuleReconciler) checkServiceMesh(ctx context.Context) (DependencyCheckResult, error) {
	logger := log.FromContext(ctx)

	result := DependencyCheckResult{Name: "ServiceMesh"}

	gvk := schema.GroupVersionKind{
		Group:   "maistra.io",
		Version: "v2",
		Kind:    "ServiceMeshControlPlane",
	}

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk)

	if err := r.List(ctx, list); err != nil {
		if errors.IsNotFound(err) || isNoMatchError(err) {
			result.Satisfied = false
			result.Message = "Service Mesh is not installed (ServiceMeshControlPlane CRD not found)"
			logger.Info("Service Mesh dependency not satisfied", "reason", result.Message)
			return result, nil
		}
		return result, fmt.Errorf("failed to list ServiceMeshControlPlane: %w", err)
	}

	if len(list.Items) == 0 {
		result.Satisfied = false
		result.Message = "Service Mesh is installed but no ServiceMeshControlPlane instance found"
		logger.Info("Service Mesh dependency not satisfied", "reason", result.Message)
		return result, nil
	}

	for _, item := range list.Items {
		conditions, found, err := unstructured.NestedSlice(item.Object, "status", "conditions")
		if err != nil || !found {
			continue
		}
		for _, cond := range conditions {
			condMap, ok := cond.(map[string]interface{})
			if !ok {
				continue
			}
			condType, _, _ := unstructured.NestedString(condMap, "type")
			condStatus, _, _ := unstructured.NestedString(condMap, "status")
			if condType == "Ready" && condStatus == "True" {
				result.Satisfied = true
				result.Message = fmt.Sprintf("Service Mesh is ready (instance: %s)", item.GetName())
				logger.Info("Service Mesh dependency satisfied", "instance", item.GetName())
				return result, nil
			}
		}
	}

	result.Satisfied = false
	result.Message = "Service Mesh instances found but none are ready"
	logger.Info("Service Mesh dependency not satisfied", "reason", result.Message)
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

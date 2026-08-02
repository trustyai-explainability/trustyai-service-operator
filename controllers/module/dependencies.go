package module

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

// checkDependencies validates all required platform dependencies
// Returns a list of dependency check results
func (r *TrustyAIReconciler) checkDependencies(ctx context.Context) ([]DependencyCheckResult, error) {
	logger := log.FromContext(ctx)
	logger.Info("Checking platform dependencies")

	results := []DependencyCheckResult{}

	// Check Service Mesh
	serviceMeshResult, err := r.checkServiceMesh(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check Service Mesh: %w", err)
	}
	results = append(results, serviceMeshResult)

	// Check Monitoring
	monitoringResult, err := r.checkMonitoring(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check Monitoring: %w", err)
	}
	results = append(results, monitoringResult)

	return results, nil
}

// checkServiceMesh verifies that Service Mesh (Istio/OSSM) is installed
func (r *TrustyAIReconciler) checkServiceMesh(ctx context.Context) (DependencyCheckResult, error) {
	logger := log.FromContext(ctx)

	result := DependencyCheckResult{
		Name: "ServiceMesh",
	}

	// Check for ServiceMeshControlPlane CRD
	// GVK for ServiceMeshControlPlane
	gvk := schema.GroupVersionKind{
		Group:   "maistra.io",
		Version: "v2",
		Kind:    "ServiceMeshControlPlane",
	}

	// List ServiceMeshControlPlane resources
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

	// Check if at least one ServiceMeshControlPlane exists
	if len(list.Items) == 0 {
		result.Satisfied = false
		result.Message = "Service Mesh is installed but no ServiceMeshControlPlane instance found"
		logger.Info("Service Mesh dependency not satisfied", "reason", result.Message)
		return result, nil
	}

	// Check if any instance is ready
	for _, item := range list.Items {
		// Get status conditions
		conditions, found, err := unstructured.NestedSlice(item.Object, "status", "conditions")
		if err != nil || !found {
			continue
		}

		// Check for Ready condition
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

// checkMonitoring verifies that monitoring stack (Prometheus) is available
func (r *TrustyAIReconciler) checkMonitoring(ctx context.Context) (DependencyCheckResult, error) {
	logger := log.FromContext(ctx)

	result := DependencyCheckResult{
		Name: "Monitoring",
	}

	// Check for Prometheus CRD (from prometheus-operator)
	// GVK for Prometheus
	gvk := schema.GroupVersionKind{
		Group:   "monitoring.coreos.com",
		Version: "v1",
		Kind:    "Prometheus",
	}

	// List Prometheus resources
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

	// Check if at least one Prometheus instance exists
	if len(list.Items) == 0 {
		result.Satisfied = false
		result.Message = "Prometheus CRD found but no Prometheus instance exists"
		logger.Info("Monitoring dependency not satisfied", "reason", result.Message)
		return result, nil
	}

	// Prometheus instances exist - consider monitoring satisfied
	// We don't check ready status as Prometheus doesn't have standard Ready condition
	result.Satisfied = true
	result.Message = fmt.Sprintf("Monitoring is available (%d Prometheus instance(s) found)", len(list.Items))
	logger.Info("Monitoring dependency satisfied", "instances", len(list.Items))
	return result, nil
}

// isNoMatchError checks if the error is a "no matches for kind" error
// This happens when the CRD doesn't exist
func isNoMatchError(err error) bool {
	if err == nil {
		return false
	}
	// Check for NoKindMatchError which occurs when CRD doesn't exist
	var noKindMatchErr *meta.NoKindMatchError
	if stderrors.As(err, &noKindMatchErr) {
		return true
	}
	// Also check for NotFound errors
	return errors.IsNotFound(err)
}

// allDependenciesSatisfied checks if all dependencies are met
func allDependenciesSatisfied(results []DependencyCheckResult) bool {
	for _, result := range results {
		if !result.Satisfied {
			return false
		}
	}
	return true
}

// formatDependencyMessages creates a human-readable message from dependency results
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

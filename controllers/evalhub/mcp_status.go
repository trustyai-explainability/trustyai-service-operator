package evalhub

import (
	"context"

	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	mcpConditionReconciled = "Reconciled"
	mcpConditionReady      = "Ready"
	mcpConditionRouteReady = "RouteReady"
)

// reconcileMCPServer reconciles optional MCP resources. Failures are recorded on status.mcp only;
// they do not affect top-level EvalHub Ready/Phase. Returns false when a critical MCP reconcile step failed.
func (r *EvalHubReconciler) reconcileMCPServer(ctx context.Context, instance *evalhubv1alpha1.EvalHub) bool {
	log := log.FromContext(ctx)

	if err := r.reconcileMCPConfigMap(ctx, instance); err != nil {
		log.Error(err, "Failed to reconcile MCP ConfigMap")
		setMCPReconcileError(instance, "MCPConfigMapFailed", err.Error())
		return false
	}
	if err := r.reconcileMCPDeployment(ctx, instance); err != nil {
		log.Error(err, "Failed to reconcile MCP Deployment")
		setMCPReconcileError(instance, "MCPDeploymentFailed", err.Error())
		return false
	}
	if err := r.reconcileMCPService(ctx, instance); err != nil {
		log.Error(err, "Failed to reconcile MCP Service")
		setMCPReconcileError(instance, "MCPServiceFailed", err.Error())
		return false
	}
	if err := r.reconcileMCPRoute(ctx, instance); err != nil {
		log.Error(err, "Failed to reconcile MCP Route")
		setMCPRouteWarning(instance, err.Error())
	} else {
		setMCPRouteSuccess(instance)
	}
	return true
}

func setMCPReconcileError(instance *evalhubv1alpha1.EvalHub, reason, message string) {
	mcp := instance.Status.MCP
	if mcp == nil {
		mcp = &evalhubv1alpha1.EvalHubMCPStatus{}
	}
	mcp.Phase = "Error"
	mcp.Ready = false
	mcp.URL = ""
	conditions := mcp.Conditions
	setMCPCondition(&conditions, mcpConditionReconciled, metav1.ConditionFalse, reason, message, instance.Generation)
	mcp.Conditions = conditions
	instance.Status.MCP = mcp
}

func setMCPRouteWarning(instance *evalhubv1alpha1.EvalHub, message string) {
	mcp := instance.Status.MCP
	if mcp == nil {
		mcp = &evalhubv1alpha1.EvalHubMCPStatus{}
	}
	conditions := mcp.Conditions
	setMCPCondition(&conditions, mcpConditionRouteReady, metav1.ConditionFalse, "MCPRouteFailed", message, instance.Generation)
	mcp.Conditions = conditions
	instance.Status.MCP = mcp
}

func setMCPRouteSuccess(instance *evalhubv1alpha1.EvalHub) {
	mcp := instance.Status.MCP
	if mcp == nil {
		mcp = &evalhubv1alpha1.EvalHubMCPStatus{}
	}
	conditions := mcp.Conditions
	setMCPCondition(&conditions, mcpConditionRouteReady, metav1.ConditionTrue, "MCPRouteReady", "Route reconciliation succeeded", instance.Generation)
	mcp.Conditions = conditions
	instance.Status.MCP = mcp
}

func setMCPCondition(conditions *[]metav1.Condition, condType string, status metav1.ConditionStatus, reason, message string, observedGeneration int64) {
	meta.SetStatusCondition(conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		ObservedGeneration: observedGeneration,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	})
}

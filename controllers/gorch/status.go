package gorch

import (
	"context"
	"errors"
	"fmt"
	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	generatorReady  bool
	deploymentReady bool
	routeReady      bool
)

const (
	AutoConfigFailed = "AutoConfigFailed"
)

func (r *GuardrailsOrchestratorReconciler) updateStatus(ctx context.Context, original *gorchv1alpha1.GuardrailsOrchestrator, update func(saved *gorchv1alpha1.GuardrailsOrchestrator)) (*gorchv1alpha1.GuardrailsOrchestrator, error) {
	saved := original.DeepCopy()

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := r.Client.Get(ctx, client.ObjectKeyFromObject(original), saved)
		if err != nil {
			return err
		}
		update(saved)
		err = r.Client.Status().Update(ctx, saved)
		return err
	})
	return saved, err
}

type StatusUpdate struct {
	component       string
	reason          string
	message         string
	conditionStatus corev1.ConditionStatus
}

func createUpdate(component string, isReady bool, err error) StatusUpdate {
	if err != nil {
		return StatusUpdate{
			component:       component,
			reason:          component + "ReadinessCheckFailed",
			message:         fmt.Sprintf("%s readiness check failed: %v", component, err),
			conditionStatus: corev1.ConditionFalse,
		}
	}
	if isReady {
		return StatusUpdate{
			component:       component,
			reason:          component + "Ready",
			message:         fmt.Sprintf("%s is ready", component),
			conditionStatus: corev1.ConditionTrue,
		}
	} else {
		return StatusUpdate{
			component:       component,
			reason:          component + "NotReady",
			message:         fmt.Sprintf("%s is not ready", component),
			conditionStatus: corev1.ConditionFalse,
		}
	}
}

func (r *GuardrailsOrchestratorReconciler) reconcileStatuses(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) (ctrl.Result, error) {
	var statusUpdates []StatusUpdate
	var requiredRoutesReadiness []bool
	var routeCheckErrors []error

	// Check overall deployment status
	deploymentReady, deploymentError := utils.CheckDeploymentReady(ctx, r.Client, orchestrator.Name, orchestrator.Namespace)
	statusUpdates = append(statusUpdates, createUpdate("Deployment", deploymentReady, deploymentError))

	// Orchestrator needed?
	if !orchestrator.Spec.DisableOrchestrator {
		generatorReady, generatorError := r.checkGeneratorPresent(ctx, orchestrator.Namespace)
		statusUpdates = append(statusUpdates, createUpdate("InferenceService", generatorReady, generatorError))
		httpRouteReady, httpRouteError := utils.CheckRouteReady(ctx, r.Client, orchestrator.Name, orchestrator.Namespace)
		healthRouteReady, healthRouteError := utils.CheckRouteReady(ctx, r.Client, orchestrator.Name+"-health", orchestrator.Namespace)
		requiredRoutesReadiness = append(requiredRoutesReadiness, httpRouteReady, healthRouteReady)
		routeCheckErrors = append(routeCheckErrors, httpRouteError, healthRouteError)
	}

	// Gateway needed?
	if orchestrator.Spec.EnableGuardrailsGateway {
		gatewayRouteReady, gatewayRouteError := utils.CheckRouteReady(ctx, r.Client, orchestrator.Name+"-gateway", orchestrator.Namespace)
		requiredRoutesReadiness = append(requiredRoutesReadiness, gatewayRouteReady)
		routeCheckErrors = append(routeCheckErrors, gatewayRouteError)
	}

	// Detectors needed?
	if orchestrator.Spec.EnableBuiltInDetectors {
		detectorRouteReady, detectorRouteError := utils.CheckRouteReady(ctx, r.Client, orchestrator.Name+"-built-in", orchestrator.Namespace)
		requiredRoutesReadiness = append(requiredRoutesReadiness, detectorRouteReady)
		routeCheckErrors = append(routeCheckErrors, detectorRouteError)
	}

	// Check route readiness
	statusUpdates = append(statusUpdates, createUpdate("Route", utils.AllTrue(requiredRoutesReadiness), errors.Join(routeCheckErrors...)))

	// Apply the new statues
	_, updateErr := r.updateStatus(ctx, orchestrator, func(saved *gorchv1alpha1.GuardrailsOrchestrator) {
		allStatusesTrue := true
		for _, status := range statusUpdates {
			utils.SetResourceCondition(&saved.Status.Conditions, status.component, status.reason, status.message, status.conditionStatus)
			allStatusesTrue = allStatusesTrue && status.conditionStatus == corev1.ConditionTrue
		}

		// If all statuses are true, report the reconciliation as finished
		if allStatusesTrue {
			utils.UnsetProgressingCondition(&saved.Status.Conditions, utils.ReconcileCompleted, "")
			utils.SetCompleteCondition(&saved.Status.Conditions, corev1.ConditionTrue, utils.ReconcileCompleted, "GuardrailsOrchestrator resource successfully initialized")
			saved.Status.Phase = utils.PhaseReady
		}
	})
	if updateErr != nil {
		utils.LogErrorUpdating(ctx, updateErr, "status", orchestrator.Name, orchestrator.Namespace)
		return ctrl.Result{}, updateErr
	}

	return ctrl.Result{}, nil
}

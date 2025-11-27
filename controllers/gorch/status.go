package gorch

import (
	"context"
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

func (r *GuardrailsOrchestratorReconciler) reconcileStatuses(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) (ctrl.Result, error) {
	generatorReady, _ = r.checkGeneratorPresent(ctx, orchestrator.Namespace)
	deploymentReady, _ = utils.CheckDeploymentReady(ctx, r.Client, orchestrator.Name, orchestrator.Namespace)
	httpRouteReady, _ := utils.CheckRouteReady(ctx, r.Client, orchestrator.Name+"-http", orchestrator.Namespace)
	healthRouteReady, _ := utils.CheckRouteReady(ctx, r.Client, orchestrator.Name+"-health", orchestrator.Namespace)
	routeReady = httpRouteReady && healthRouteReady
	if generatorReady && deploymentReady && routeReady {
		_, updateErr := r.updateStatus(ctx, orchestrator, func(saved *gorchv1alpha1.GuardrailsOrchestrator) {
			utils.SetResourceCondition(&saved.Status.Conditions, "InferenceService", "InferenceServiceReady", "Inference service is ready", corev1.ConditionTrue)
			utils.SetResourceCondition(&saved.Status.Conditions, "Deployment", "DeploymentReady", "Deployment is ready", corev1.ConditionTrue)
			utils.SetResourceCondition(&saved.Status.Conditions, "Route", "RouteReady", "Route is ready", corev1.ConditionTrue)
			utils.SetCompleteCondition(&saved.Status.Conditions, corev1.ConditionTrue, utils.ReconcileCompleted, utils.ReconcileCompletedMessage)
			saved.Status.Phase = utils.PhaseReady
		})
		if updateErr != nil {
			utils.LogErrorUpdating(ctx, updateErr, "status", orchestrator.Name, orchestrator.Namespace)
			return ctrl.Result{}, updateErr
		}
	} else {
		_, updateErr := r.updateStatus(ctx, orchestrator, func(saved *gorchv1alpha1.GuardrailsOrchestrator) {
			if generatorReady {
				utils.SetResourceCondition(&saved.Status.Conditions, "InferenceService", "InferenceServiceReady", "Inference service is ready", corev1.ConditionTrue)
			} else {
				utils.SetResourceCondition(&saved.Status.Conditions, "InferenceService", "InferenceServiceNotReady", "Inference service is not ready", corev1.ConditionFalse)
			}
			if deploymentReady {
				utils.SetResourceCondition(&saved.Status.Conditions, "Deployment", "DeploymentReady", "Deployment is ready", corev1.ConditionTrue)
			} else {
				utils.SetResourceCondition(&saved.Status.Conditions, "Deployment", "DeploymentNotReady", "Deployment is not ready", corev1.ConditionFalse)
			}
			if routeReady {
				utils.SetResourceCondition(&saved.Status.Conditions, "Route", "RouteReady", "Route is ready", corev1.ConditionTrue)
			} else {
				utils.SetResourceCondition(&saved.Status.Conditions, "Route", "RouteNotReady", "Route is not ready", corev1.ConditionFalse)
			}

			utils.SetCompleteCondition(&saved.Status.Conditions, corev1.ConditionFalse, utils.ReconcileFailed, utils.ReconcileFailedMessage)
		})
		if updateErr != nil {
			utils.LogErrorUpdating(ctx, updateErr, "status", orchestrator.Name, orchestrator.Namespace)
			return ctrl.Result{}, updateErr
		}
	}
	return ctrl.Result{}, nil
}

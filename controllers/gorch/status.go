package gorch

import (
	"context"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"

	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	generatorReady  bool
	deploymentReady bool
	routeReady      bool
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
	httpRouteReady, _ := utils.CheckRouteReady(ctx, r.Client, orchestrator.Name, orchestrator.Namespace, "-http")
	healthRouteReady, _ := utils.CheckRouteReady(ctx, r.Client, orchestrator.Name, orchestrator.Namespace, "-health")
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
			log.FromContext(ctx).Error(updateErr, "Failed to update status")
			return ctrl.Result{}, updateErr
		}
	} else {
		_, updateErr := r.updateStatus(ctx, orchestrator, func(saved *gorchv1alpha1.GuardrailsOrchestrator) {
			utils.SetStatus(&saved.Status.Conditions, "InferenceService", generatorReady)
			utils.SetStatus(&saved.Status.Conditions, "Deployment", deploymentReady)
			utils.SetStatus(&saved.Status.Conditions, "Route", routeReady)
			utils.SetCompleteCondition(&saved.Status.Conditions, corev1.ConditionFalse, utils.ReconcileFailed, utils.ReconcileFailedMessage)
		})
		if updateErr != nil {
			log.FromContext(ctx).Error(updateErr, "Failed to update status")
			return ctrl.Result{}, updateErr
		}
	}
	return ctrl.Result{}, nil
}

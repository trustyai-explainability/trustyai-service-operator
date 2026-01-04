package nemo_guardrails

import (
	"context"
	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *NemoGuardrailsReconciler) updateStatus(ctx context.Context, original *nemoguardrailsv1alpha1.NemoGuardrails, update func(saved *nemoguardrailsv1alpha1.NemoGuardrails)) (*nemoguardrailsv1alpha1.NemoGuardrails, error) {
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

func (r *NemoGuardrailsReconciler) reconcileStatuses(ctx context.Context, nemoGuardrails *nemoguardrailsv1alpha1.NemoGuardrails) (ctrl.Result, error) {
	deploymentReady, _ := utils.CheckDeploymentReady(ctx, r.Client, nemoGuardrails.Name, nemoGuardrails.Namespace)
	routeReady, _ := utils.CheckRouteReady(ctx, r.Client, nemoGuardrails.Name, nemoGuardrails.Namespace)

	if deploymentReady && routeReady {
		_, updateErr := r.updateStatus(ctx, nemoGuardrails, func(saved *nemoguardrailsv1alpha1.NemoGuardrails) {
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
		_, updateErr := r.updateStatus(ctx, nemoGuardrails, func(saved *nemoguardrailsv1alpha1.NemoGuardrails) {

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

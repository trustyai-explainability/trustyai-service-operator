package nemo_guardrails

import (
	"context"
	"time"

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

func (r *NemoGuardrailsReconciler) markWorkloadNamespaceRejected(ctx context.Context, nemoGuardrails *nemoguardrailsv1alpha1.NemoGuardrails, cause error) (ctrl.Result, error) {
	_, updateErr := r.updateStatus(ctx, nemoGuardrails, func(saved *nemoguardrailsv1alpha1.NemoGuardrails) {
		utils.SetResourceCondition(&saved.Status.Conditions, "Deployment", "WorkloadNamespaceNotAllowed", cause.Error(), corev1.ConditionFalse)
		utils.SetCompleteCondition(&saved.Status.Conditions, corev1.ConditionFalse, utils.ReconcileFailed, cause.Error())
		saved.Status.Phase = utils.PhaseError
	})
	if updateErr != nil {
		log.FromContext(ctx).Error(updateErr, "Failed to update status")
		return ctrl.Result{}, updateErr
	}
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *NemoGuardrailsReconciler) reconcileStatuses(ctx context.Context, nemoGuardrails *nemoguardrailsv1alpha1.NemoGuardrails) (ctrl.Result, error) {
	targetNamespace := workloadNamespace(nemoGuardrails)
	deploymentReady, _ := utils.CheckDeploymentReady(ctx, r.Client, nemoGuardrails.Name, targetNamespace)

	exposeRoute := nemoGuardrails.Spec.ExposeRoute != nil && *nemoGuardrails.Spec.ExposeRoute
	routeReady := !exposeRoute
	if exposeRoute {
		routeReady, _ = utils.CheckRouteReady(ctx, r.Client, nemoGuardrails.Name, targetNamespace)
	}

	if deploymentReady && routeReady {
		_, updateErr := r.updateStatus(ctx, nemoGuardrails, func(saved *nemoguardrailsv1alpha1.NemoGuardrails) {
			utils.SetResourceCondition(&saved.Status.Conditions, "Deployment", "DeploymentReady", "Deployment is ready", corev1.ConditionTrue)
			if exposeRoute {
				utils.SetResourceCondition(&saved.Status.Conditions, "Route", "RouteReady", "Route is ready", corev1.ConditionTrue)
			} else {
				utils.SetResourceCondition(&saved.Status.Conditions, "Route", "RouteDisabled", "Route is not required", corev1.ConditionFalse)
			}
			utils.SetCompleteCondition(&saved.Status.Conditions, corev1.ConditionTrue, utils.ReconcileCompleted, utils.ReconcileCompletedMessage)
			saved.Status.Phase = utils.PhaseReady
			saved.Status.WorkloadNamespace = targetNamespace
		})
		if updateErr != nil {
			log.FromContext(ctx).Error(updateErr, "Failed to update status")
			return ctrl.Result{}, updateErr
		}
	} else {
		_, updateErr := r.updateStatus(ctx, nemoGuardrails, func(saved *nemoguardrailsv1alpha1.NemoGuardrails) {

			utils.SetStatus(&saved.Status.Conditions, "Deployment", deploymentReady)
			if exposeRoute {
				utils.SetStatus(&saved.Status.Conditions, "Route", routeReady)
			} else {
				utils.SetResourceCondition(&saved.Status.Conditions, "Route", "RouteDisabled", "Route is not required", corev1.ConditionFalse)
			}
			utils.SetCompleteCondition(&saved.Status.Conditions, corev1.ConditionFalse, utils.ReconcileFailed, utils.ReconcileFailedMessage)
			saved.Status.WorkloadNamespace = targetNamespace
		})
		if updateErr != nil {
			log.FromContext(ctx).Error(updateErr, "Failed to update status")
			return ctrl.Result{}, updateErr
		}
	}
	return ctrl.Result{}, nil
}

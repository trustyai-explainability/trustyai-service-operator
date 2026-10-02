package nemo_guardrails

import (
	"context"
	"errors"
	"fmt"

	routev1 "github.com/openshift/api/route/v1"
	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
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
	deploymentReady, deploymentErr := utils.CheckDeploymentReady(ctx, r.Client, nemoGuardrails.Name, nemoGuardrails.Namespace)

	exposeRoute := nemoGuardrails.Spec.ExposeRoute != nil && *nemoGuardrails.Spec.ExposeRoute
	// showEndpointStatus is true only when the Route is both requested and protected by auth.
	showEndpointStatus := exposeRoute && utils.RequiresAuth(nemoGuardrails)
	routeReady := !exposeRoute
	endpoint := ""
	if exposeRoute {
		routeReady, _ = utils.CheckRouteReady(ctx, r.Client, nemoGuardrails.Name, nemoGuardrails.Namespace)
		if routeReady && showEndpointStatus {
			var endpointErr error
			endpoint, endpointErr = r.getRouteEndpoint(ctx, nemoGuardrails.Name, nemoGuardrails.Namespace)
			if endpointErr != nil {
				return ctrl.Result{}, endpointErr
			}
		}
	}

	if deploymentReady && routeReady {
		_, updateErr := r.updateStatus(ctx, nemoGuardrails, func(saved *nemoguardrailsv1alpha1.NemoGuardrails) {
			utils.SetResourceCondition(&saved.Status.Conditions, "Deployment", "DeploymentReady", "Deployment is ready", corev1.ConditionTrue)
			if exposeRoute {
				utils.SetResourceCondition(&saved.Status.Conditions, "Route", "RouteReady", "Route is ready", corev1.ConditionTrue)
			} else {
				utils.SetResourceCondition(&saved.Status.Conditions, "Route", "RouteDisabled", "Route is not required", corev1.ConditionFalse)
			}
			if showEndpointStatus {
				saved.Status.Endpoint = endpoint
			} else {
				saved.Status.Endpoint = ""
			}
			utils.SetCompleteCondition(&saved.Status.Conditions, corev1.ConditionTrue, utils.ReconcileCompleted, utils.ReconcileCompletedMessage)
			saved.Status.Phase = utils.PhaseReady
		})
		if updateErr != nil {
			log.FromContext(ctx).Error(updateErr, "Failed to update status")
			return ctrl.Result{}, updateErr
		}
	} else {
		_, updateErr := r.updateStatus(ctx, nemoGuardrails, func(saved *nemoguardrailsv1alpha1.NemoGuardrails) {

			if deploymentErr != nil {
				utils.SetResourceCondition(&saved.Status.Conditions, "Deployment", "DeploymentReadinessCheckFailed", "Deployment readiness check failed: "+deploymentErr.Error(), corev1.ConditionFalse)
			} else {
				utils.SetStatus(&saved.Status.Conditions, "Deployment", deploymentReady)
			}
			if exposeRoute {
				utils.SetStatus(&saved.Status.Conditions, "Route", routeReady)
			} else {
				utils.SetResourceCondition(&saved.Status.Conditions, "Route", "RouteDisabled", "Route is not required", corev1.ConditionFalse)
			}
			if showEndpointStatus {
				saved.Status.Endpoint = endpoint
			} else {
				saved.Status.Endpoint = ""
			}
			if deploymentErr != nil {
				message := "Deployment readiness check failed: " + deploymentErr.Error()
				if errors.Is(deploymentErr, utils.ErrDeploymentProgressDeadlineExceeded) {
					message = "Deployment rollout is stuck: " + deploymentErr.Error()
				}
				utils.SetCompleteCondition(&saved.Status.Conditions, corev1.ConditionFalse, utils.ReconcileFailed, message)
				saved.Status.Phase = utils.PhaseError
			} else {
				utils.SetCompleteCondition(&saved.Status.Conditions, corev1.ConditionFalse, "WaitingForReady", "Waiting for required resources to become ready")
				saved.Status.Phase = utils.PhaseProgressing
			}
		})
		if updateErr != nil {
			log.FromContext(ctx).Error(updateErr, "Failed to update status")
			return ctrl.Result{}, updateErr
		}
	}
	return ctrl.Result{}, nil
}

// getRouteEndpoint builds the public URL for a Route from its host.
func (r *NemoGuardrailsReconciler) getRouteEndpoint(ctx context.Context, name, namespace string) (string, error) {
	route := &routev1.Route{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, route); err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	if route.Spec.Host != "" {
		return fmt.Sprintf("https://%s", route.Spec.Host), nil
	}
	for _, ingress := range route.Status.Ingress {
		if ingress.Host == "" {
			continue
		}
		for _, condition := range ingress.Conditions {
			if condition.Type == routev1.RouteAdmitted && condition.Status == corev1.ConditionTrue {
				return fmt.Sprintf("https://%s", ingress.Host), nil
			}
		}
	}
	return "", nil
}

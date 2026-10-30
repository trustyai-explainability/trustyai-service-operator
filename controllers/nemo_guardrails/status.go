package nemo_guardrails

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

func (r *NemoGuardrailsReconciler) reconcileStatuses(ctx context.Context, nemoGuardrails *nemoguardrailsv1alpha1.NemoGuardrails, targetNamespace string) (ctrl.Result, error) {
	identity := newWorkloadIdentity(nemoGuardrails, targetNamespace)
	deploymentReady, deploymentErr := utils.CheckDeploymentReady(ctx, r.Client, identity.Name, targetNamespace)

	exposeRoute := nemoGuardrails.Spec.ExposeRoute != nil && *nemoGuardrails.Spec.ExposeRoute
	routeReady := !exposeRoute
	if exposeRoute {
		routeReady, _ = utils.CheckRouteReady(ctx, r.Client, identity.Name, targetNamespace)
	}

	endpoint := internalServiceEndpoint(nemoGuardrails, identity.Name, targetNamespace)
	// Publish the new address once the Deployment is Ready and, when a Route is required, that Route is admitted.
	// Until then a namespace move keeps the previous endpoint and workload namespace.
	publishAddress := previousWorkloadNamespace(nemoGuardrails) == targetNamespace || (deploymentReady && routeReady)
	setEndpoint := func(saved *nemoguardrailsv1alpha1.NemoGuardrails) {
		if !publishAddress {
			return
		}
		// A scheme change waits until the new pods are Ready. Until then the previous URL still matches the pods that are serving.
		saved.Status.Endpoint = endpointWhileRolling(saved.Status.Endpoint, endpoint, deploymentReady)
		saved.Status.WorkloadNamespace = targetNamespace
	}

	if deploymentReady && routeReady {
		_, updateErr := r.updateStatus(ctx, nemoGuardrails, func(saved *nemoguardrailsv1alpha1.NemoGuardrails) {
			utils.SetResourceCondition(&saved.Status.Conditions, "Deployment", "DeploymentReady", "Deployment is ready", corev1.ConditionTrue)
			if exposeRoute {
				utils.SetResourceCondition(&saved.Status.Conditions, "Route", "RouteReady", "Route is ready", corev1.ConditionTrue)
			} else {
				utils.SetResourceCondition(&saved.Status.Conditions, "Route", "RouteDisabled", "Route is not required", corev1.ConditionFalse)
			}
			setEndpoint(saved)
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
			setEndpoint(saved)
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

// endpointWhileRolling keeps a published URL while a rollout is still serving the previous protocol.
func endpointWhileRolling(previous, desired string, deploymentReady bool) string {
	if deploymentReady || previous == "" || urlScheme(previous) == urlScheme(desired) {
		return desired
	}
	return previous
}

func urlScheme(endpoint string) string {
	switch {
	case strings.HasPrefix(endpoint, "https://"):
		return "https"
	case strings.HasPrefix(endpoint, "http://"):
		return "http"
	default:
		return ""
	}
}

func internalServiceEndpoint(nemoGuardrails *nemoguardrailsv1alpha1.NemoGuardrails, serviceName, namespace string) string {
	scheme := "http"
	if utils.RequiresAuth(nemoGuardrails) {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s.%s.svc.cluster.local", scheme, serviceName, namespace)
}

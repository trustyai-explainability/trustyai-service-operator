package gorch

import (
	"context"
	"fmt"
	"strings"
	"time"

	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

const (
	ConditionReconcileComplete gorchv1alpha1.ConditionType = "ReconcileComplete"
	ConditionProgessing        gorchv1alpha1.ConditionType = "Progressing"
)

const (
	PhaseProgressing = "Progressing"
	PhaseReady       = "Ready"
)

const (
	ReconcileFailed           = "ReconcileFailed"
	ReconcileInit             = "ReconcileInit"
	ReconcileCompleted        = "ReconcileCompleted"
	ReconcileCompletedMessage = "Reconcile completed successfully"
	ReconcileFailedMessage    = "Reconcile failed"
)

func SetStatusCondition(conditions *[]gorchv1alpha1.Condition, newCondition gorchv1alpha1.Condition) bool {
	if conditions == nil {
		conditions = &[]gorchv1alpha1.Condition{}
	}
	existingCondition := GetStatusCondition(*conditions, newCondition.Type)
	if existingCondition == nil {
		newCondition.LastTransitionTime = metav1.NewTime(time.Now())
		*conditions = append(*conditions, newCondition)
		return true
	}

	changed := updateCondition(existingCondition, newCondition)

	return changed
}

func GetStatusCondition(conditions []gorchv1alpha1.Condition, conditionType gorchv1alpha1.ConditionType) *gorchv1alpha1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

func updateCondition(existingCondition *gorchv1alpha1.Condition, newCondition gorchv1alpha1.Condition) bool {
	changed := false
	if existingCondition.Status != newCondition.Status {
		changed = true
		existingCondition.Status = newCondition.Status
		existingCondition.LastTransitionTime = metav1.NewTime(time.Now())
	}
	if existingCondition.Reason != newCondition.Reason {
		changed = true
		existingCondition.Reason = newCondition.Reason

	}
	if existingCondition.Message != newCondition.Message {
		changed = true
		existingCondition.Message = newCondition.Message
	}
	return changed
}

func SetProgressingCondition(conditions *[]gorchv1alpha1.Condition, reason string, message string) {
	SetStatusCondition(conditions, gorchv1alpha1.Condition{
		Type:    ConditionProgessing,
		Status:  corev1.ConditionTrue,
		Reason:  reason,
		Message: message,
	})

}

func SetResourceCondition(conditions *[]gorchv1alpha1.Condition, component string, reason string, message string, status corev1.ConditionStatus) {
	condtype := component + "Ready"
	SetStatusCondition(conditions, gorchv1alpha1.Condition{
		Type:    gorchv1alpha1.ConditionType(condtype),
		Status:  status,
		Reason:  reason,
		Message: message,
	})
}

func SetCompleteCondition(conditions *[]gorchv1alpha1.Condition, status corev1.ConditionStatus, reason, message string) {
	SetStatusCondition(conditions, gorchv1alpha1.Condition{
		Type:    ConditionReconcileComplete,
		Status:  status,
		Reason:  reason,
		Message: message,
	})
}

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
	logger := log.FromContext(ctx)
	generatorReady, generatorErr := r.checkGeneratorPresent(ctx, orchestrator.Namespace)
	deploymentReady, deploymentErr := r.checkDeploymentReady(ctx, orchestrator)
	httpRouteReady, httpRouteErr := r.checkRouteReady(ctx, orchestrator, "-http")
	healthRouteReady, healthRouteErr := r.checkRouteReady(ctx, orchestrator, "-health")
	routeReady = httpRouteReady && healthRouteReady

	if generatorErr != nil {
		logger.Error(generatorErr, "Error checking inference service readiness")
	}
	if deploymentErr != nil {
		logger.Error(deploymentErr, "Error checking deployment readiness")
	}
	if httpRouteErr != nil {
		logger.Error(httpRouteErr, "Error checking HTTP route readiness")
	}
	if healthRouteErr != nil {
		logger.Error(healthRouteErr, "Error checking Health route readiness")
	}

	if generatorReady && deploymentReady && routeReady {
		_, updateErr := r.updateStatus(ctx, orchestrator, func(saved *gorchv1alpha1.GuardrailsOrchestrator) {
			SetResourceCondition(&saved.Status.Conditions, "InferenceService", "InferenceServiceReady",
				"Inference service is ready and operational", corev1.ConditionTrue)

			SetResourceCondition(&saved.Status.Conditions, "Deployment", "DeploymentReady",
				fmt.Sprintf("Deployment '%s' is ready with all %d replicas available",
					orchestrator.Name, orchestrator.Spec.Replicas), corev1.ConditionTrue)

			SetResourceCondition(&saved.Status.Conditions, "Route", "RouteReady",
				fmt.Sprintf("Routes '%s-http' and '%s-health' are ready and accessible",
					orchestrator.Name, orchestrator.Name), corev1.ConditionTrue)

			SetCompleteCondition(&saved.Status.Conditions, corev1.ConditionTrue, ReconcileCompleted,
				"All components have been successfully reconciled and are operational")

			saved.Status.Phase = PhaseReady
		})
		if updateErr != nil {
			logger.Error(updateErr, "Failed to update status")
			return ctrl.Result{}, updateErr
		}
	} else {
		_, updateErr := r.updateStatus(ctx, orchestrator, func(saved *gorchv1alpha1.GuardrailsOrchestrator) {
			if generatorReady {
				SetResourceCondition(&saved.Status.Conditions, "InferenceService", "InferenceServiceReady",
					"Inference service is ready and operational", corev1.ConditionTrue)
			} else {
				errorDetail := ""
				if generatorErr != nil {
					errorDetail = fmt.Sprintf(": %s", generatorErr.Error())
				}
				SetResourceCondition(&saved.Status.Conditions, "InferenceService", "InferenceServiceNotReady",
					fmt.Sprintf("Inference service is not ready in namespace '%s'%s. Verify that an InferenceService exists.",
						orchestrator.Namespace, errorDetail), corev1.ConditionFalse)
			}

			if deploymentReady {
				SetResourceCondition(&saved.Status.Conditions, "Deployment", "DeploymentReady",
					fmt.Sprintf("Deployment '%s' is ready with all %d replicas available",
						orchestrator.Name, orchestrator.Spec.Replicas), corev1.ConditionTrue)
			} else {
				configMapName := ""
				if orchestrator.Spec.OrchestratorConfig != nil {
					configMapName = *orchestrator.Spec.OrchestratorConfig
				}

				errorDetail := ""
				if deploymentErr != nil {
					errorDetail = fmt.Sprintf(": %s", deploymentErr.Error())
				}

				SetResourceCondition(&saved.Status.Conditions, "Deployment", "DeploymentNotReady",
					fmt.Sprintf("Deployment '%s' is not ready%s. Verify ConfigMap '%s' exists and contains required image references.",
						orchestrator.Name, errorDetail, configMapName), corev1.ConditionFalse)
			}

			if routeReady {
				SetResourceCondition(&saved.Status.Conditions, "Route", "RouteReady",
					fmt.Sprintf("Routes '%s-http' and '%s-health' are ready and accessible",
						orchestrator.Name, orchestrator.Name), corev1.ConditionTrue)
			} else {
				routeErrorDetail := ""
				if !httpRouteReady && !healthRouteReady {
					if httpRouteErr != nil || healthRouteErr != nil {
						routeErrorDetail = fmt.Sprintf(": %s", getErrorMessage(httpRouteErr, healthRouteErr))
					}
					SetResourceCondition(&saved.Status.Conditions, "Route", "RouteNotReady",
						fmt.Sprintf("Both routes '%s-http' and '%s-health' are not ready%s. Verify service '%s-service' exists and is properly configured.",
							orchestrator.Name, orchestrator.Name, routeErrorDetail, orchestrator.Name), corev1.ConditionFalse)
				} else if !httpRouteReady {
					routeErrorMsg := ""
					if httpRouteErr != nil {
						routeErrorMsg = fmt.Sprintf(": %s", httpRouteErr.Error())
					}
					SetResourceCondition(&saved.Status.Conditions, "Route", "RouteNotReady",
						fmt.Sprintf("HTTP route '%s-http' is not ready%s. Health route is operational.",
							orchestrator.Name, routeErrorMsg), corev1.ConditionFalse)
				} else {
					routeErrorMsg := ""
					if healthRouteErr != nil {
						routeErrorMsg = fmt.Sprintf(": %s", healthRouteErr.Error())
					}
					SetResourceCondition(&saved.Status.Conditions, "Route", "RouteNotReady",
						fmt.Sprintf("Health route '%s-health' is not ready%s. HTTP route is operational.",
							orchestrator.Name, routeErrorMsg), corev1.ConditionFalse)
				}
			}

			SetCompleteCondition(&saved.Status.Conditions, corev1.ConditionFalse, ReconcileFailed,
				"Reconciliation failed. Check individual component statuses for specific issues that need to be addressed.")
		})
		if updateErr != nil {
			logger.Error(updateErr, "Failed to update status")
			return ctrl.Result{}, updateErr
		}
	}
	return ctrl.Result{}, nil
}

func getErrorMessage(errors ...error) string {
	var messages []string
	for _, err := range errors {
		if err != nil {
			messages = append(messages, err.Error())
		}
	}
	if len(messages) == 1 {
		return messages[0]
	} else if len(messages) > 1 {
		return "Multiple issues detected: " + strings.Join(messages, "; ")
	}
	return "Unknown error"
}

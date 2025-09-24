package gorch

import (
	"context"
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
	PhaseError       = "Error"
	PhaseProgressing = "Progressing"
	PhaseReady       = "Ready"
)

const (
	ReconcileFailed           = "ReconcileFailed"
	ReconcileInit             = "ReconcileInit"
	ReconcileCompleted        = "ReconcileCompleted"
	AutoConfigFailed          = "AutoConfigFailed"
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

func UnsetProgressingCondition(conditions *[]gorchv1alpha1.Condition, reason string, message string) {
	SetStatusCondition(conditions, gorchv1alpha1.Condition{
		Type:    ConditionProgessing,
		Status:  corev1.ConditionFalse,
		Reason:  reason,
		Message: message,
	})
}

func SetFailedCondition(conditions *[]gorchv1alpha1.Condition, reason string, message string) {
	SetStatusCondition(conditions, gorchv1alpha1.Condition{
		Type:    ReconcileFailed,
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
	generatorReady, _ = r.checkGeneratorPresent(ctx, orchestrator.Namespace)
	deploymentReady, _ = r.checkDeploymentReady(ctx, orchestrator)
	httpRouteReady, _ := r.checkRouteReady(ctx, orchestrator, "-http")
	healthRouteReady, _ := r.checkRouteReady(ctx, orchestrator, "-health")
	routeReady = httpRouteReady && healthRouteReady
	if generatorReady && deploymentReady && routeReady {
		_, updateErr := r.updateStatus(ctx, orchestrator, func(saved *gorchv1alpha1.GuardrailsOrchestrator) {
			SetResourceCondition(&saved.Status.Conditions, "InferenceService", "InferenceServiceReady", "Inference service is ready", corev1.ConditionTrue)
			SetResourceCondition(&saved.Status.Conditions, "Deployment", "DeploymentReady", "Deployment is ready", corev1.ConditionTrue)
			SetResourceCondition(&saved.Status.Conditions, "Route", "RouteReady", "Route is ready", corev1.ConditionTrue)
			SetCompleteCondition(&saved.Status.Conditions, corev1.ConditionTrue, ReconcileCompleted, ReconcileCompletedMessage)
			saved.Status.Phase = PhaseReady
		})
		if updateErr != nil {
			log.FromContext(ctx).Error(updateErr, "Failed to update status")
			return ctrl.Result{}, updateErr
		}
	} else {
		_, updateErr := r.updateStatus(ctx, orchestrator, func(saved *gorchv1alpha1.GuardrailsOrchestrator) {
			if generatorReady {
				SetResourceCondition(&saved.Status.Conditions, "InferenceService", "InferenceServiceReady", "Inference service is ready", corev1.ConditionTrue)
			} else {
				SetResourceCondition(&saved.Status.Conditions, "InferenceService", "InferenceServiceNotReady", "Inference service is not ready", corev1.ConditionFalse)
			}
			if deploymentReady {
				SetResourceCondition(&saved.Status.Conditions, "Deployment", "DeploymentReady", "Deployment is ready", corev1.ConditionTrue)
			} else {
				SetResourceCondition(&saved.Status.Conditions, "Deployment", "DeploymentNotReady", "Deployment is not ready", corev1.ConditionFalse)
			}
			if routeReady {
				SetResourceCondition(&saved.Status.Conditions, "Route", "RouteReady", "Route is ready", corev1.ConditionTrue)
			} else {
				SetResourceCondition(&saved.Status.Conditions, "Route", "RouteNotReady", "Route is not ready", corev1.ConditionFalse)
			}

			SetCompleteCondition(&saved.Status.Conditions, corev1.ConditionFalse, ReconcileFailed, ReconcileFailedMessage)
		})
		if updateErr != nil {
			log.FromContext(ctx).Error(updateErr, "Failed to update status")
			return ctrl.Result{}, updateErr
		}
	}
	return ctrl.Result{}, nil
}

package gorch

import (
	"context"
	"fmt"
	"time"

	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	ReconcileInit             = "ReconcileSuccess"
	ReconcileCompleted        = "ReconcileCompleted"
	ReconcileCompletedMessage = "Reconcile completed successfully"
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

func SetCompleteCondition(conditions *[]gorchv1alpha1.Condition, reason, message string) {
	SetStatusCondition(conditions, gorchv1alpha1.Condition{
		Type:    ConditionReconcileComplete,
		Status:  corev1.ConditionTrue,
		Reason:  reason,
		Message: message,
	})
}

func (r *GuardrailsOrchestratorReconciler) updateStatus(original *gorchv1alpha1.GuardrailsOrchestrator, update func(saved *gorchv1alpha1.GuardrailsOrchestrator)) (*gorchv1alpha1.GuardrailsOrchestrator, error) {
	saved := &gorchv1alpha1.GuardrailsOrchestrator{}

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		key := client.ObjectKeyFromObject(original)
		fmt.Printf("Fetching GuardrailsOrchestrator with key: %v\n", key)
		err := r.Client.Get(context.TODO(), key, saved)
		if err != nil {
			return err
		}
		update(saved)
		err = r.Client.Status().Update(context.TODO(), saved)
		return err
	})
	return saved, err
}
package utils

import (
	"github.com/trustyai-explainability/trustyai-service-operator/api/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"time"
)

const (
	ConditionReconcileComplete = "ReconcileComplete"
	ConditionProgessing        = "Progressing"
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
	ReconcileCompletedMessage = "Reconcile completed successfully"
	ReconcileFailedMessage    = "Reconcile failed"
)

func SetStatusCondition(conditions *[]common.Condition, newCondition common.Condition) bool {
	if conditions == nil {
		conditions = &[]common.Condition{}
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

func GetStatusCondition(conditions []common.Condition, conditionType string) *common.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

func updateCondition(existingCondition *common.Condition, newCondition common.Condition) bool {
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

func SetProgressingCondition(conditions *[]common.Condition, reason string, message string) {
	SetStatusCondition(conditions, common.Condition{
		Type:    ConditionProgessing,
		Status:  corev1.ConditionTrue,
		Reason:  reason,
		Message: message,
	})
}

func UnsetProgressingCondition(conditions *[]common.Condition, reason string, message string) {
	SetStatusCondition(conditions, common.Condition{
		Type:    ConditionProgessing,
		Status:  corev1.ConditionFalse,
		Reason:  reason,
		Message: message,
	})
}

func SetFailedCondition(conditions *[]common.Condition, reason string, message string) {
	SetStatusCondition(conditions, common.Condition{
		Type:    ReconcileFailed,
		Status:  corev1.ConditionTrue,
		Reason:  reason,
		Message: message,
	})
}

func SetResourceCondition(conditions *[]common.Condition, component string, reason string, message string, status corev1.ConditionStatus) {
	condtype := component + "Ready"
	SetStatusCondition(conditions, common.Condition{
		Type:    condtype,
		Status:  status,
		Reason:  reason,
		Message: message,
	})
}

func SetCompleteCondition(conditions *[]common.Condition, status corev1.ConditionStatus, reason, message string) {
	SetStatusCondition(conditions, common.Condition{
		Type:    ConditionReconcileComplete,
		Status:  status,
		Reason:  reason,
		Message: message,
	})
}

func SetStatus(conditions *[]common.Condition, resourceKind string, isReady bool) {
	if isReady {
		SetResourceCondition(conditions, resourceKind, resourceKind+"Ready", resourceKind+" is ready", corev1.ConditionTrue)
	} else {
		SetResourceCondition(conditions, resourceKind, resourceKind+"NotReady", resourceKind+" is not ready", corev1.ConditionFalse)
	}
}

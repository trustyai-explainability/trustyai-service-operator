package gorch

import (
	"context"
	"time"

	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	typeDegradedOrchestrator  = "Degraded"
	typeAvailableOrchestrator = "Available"
)

type updateStatusFunc func(orchestrator *gorchv1alpha1.GuardrailsOrchestrator, message string) error

func setStatusConditions(conditions []gorchv1alpha1.GuardrailsOrchestratorCondition, newConditions ...gorchv1alpha1.GuardrailsOrchestratorCondition) ([]gorchv1alpha1.GuardrailsOrchestratorCondition, bool) {
	var atLeastOneUpdated bool
	var updated bool
	for _, cond := range newConditions {
		conditions, updated = updateStatusCondition(conditions, cond)
		atLeastOneUpdated = atLeastOneUpdated || updated
	}

	return conditions, atLeastOneUpdated
}

var testTime *time.Time

// updateStatusConditions updates the status of the orchestrator with the new conditions
func updateStatusConditions(orchestrator *gorchv1alpha1.GuardrailsOrchestrator, client client.Client, newConditions ...gorchv1alpha1.GuardrailsOrchestratorCondition) error {
	var updated bool
	log := log.FromContext(context.Background())
	orchestrator.Status.Conditions, updated = setStatusConditions(orchestrator.Status.Conditions, newConditions...)
	if !updated {
		log.Info("GuardrailsOrchestrator status conditions not changed")
		return nil
	}
	return client.Status().Update(context.TODO(), orchestrator)
}

// updateStatusCondition updates the status of the orchestrator with the new condition.
func updateStatusCondition(conditions []gorchv1alpha1.GuardrailsOrchestratorCondition, newCondition gorchv1alpha1.GuardrailsOrchestratorCondition) ([]gorchv1alpha1.GuardrailsOrchestratorCondition, bool) {
	var now time.Time
	if testTime == nil {
		now = time.Now()
	} else {
		now = *testTime
	}

	newCondition.LastTransitionTime = metav1.NewTime(now.Truncate(time.Second))

	//  If the condition doesn't exist, add it
	if conditions == nil {
		return []gorchv1alpha1.GuardrailsOrchestratorCondition{newCondition}, true
	}
	for i, cond := range conditions {
		if cond.Type == newCondition.Type {
			// Case 1: The condition has not changed
			if cond.Status == newCondition.Status &&
				cond.Reason == newCondition.Reason &&
				cond.Message == newCondition.Message {
				return conditions, false
			}

			// Case 2: The condition status has changed
			if newCondition.Status == cond.Status {
				newCondition.LastTransitionTime = cond.LastTransitionTime
			}
			// Case 3: The condition has changed, create a new slice with the updated condition
			res := make([]gorchv1alpha1.GuardrailsOrchestratorCondition, len(conditions))
			copy(res, conditions)
			res[i] = newCondition
			return res, true
		}
	}
	return append(conditions, newCondition), true
}

// statusUpdateError updates the status of the orchestrator with the error message if the error is not nil.
func (r *GuardrailsOrchestratorReconciler) statusUpdateError(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator, updateStatus updateStatusFunc, err error) error {
	log := log.FromContext(ctx)
	if err == nil {
		return nil
	}
	if err := updateStatus(orchestrator, err.Error()); err != nil {
		log.Error(err, "Failed to update GuardrailsOrchestrator status")
	}
	return err
}

// setStatusFailed handles the case where one of the orchestrator's components is not available.
func (r *GuardrailsOrchestratorReconciler) setStatusFailed(reason string) updateStatusFunc {
	return func(orchestrator *gorchv1alpha1.GuardrailsOrchestrator, message string) error {
		return updateStatusConditions(
			orchestrator,
			r.Client,
			statusNotReady(reason, message),
		)
	}
}

func statusReady() gorchv1alpha1.GuardrailsOrchestratorCondition {
	return gorchv1alpha1.GuardrailsOrchestratorCondition{
		Type:   typeAvailableOrchestrator,
		Status: metav1.ConditionTrue,
		Reason: "OrchestratorAvailable",
	}
}

func statusNotReady(reason, message string) gorchv1alpha1.GuardrailsOrchestratorCondition {
	return gorchv1alpha1.GuardrailsOrchestratorCondition{
		Type:    typeAvailableOrchestrator,
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: message,
	}
}

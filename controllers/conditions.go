package controllers

import (
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (r *TrustyAIServiceReconciler) setCondition(instance *trustyaiopendatahubiov1alpha1.TrustyAIService, condition trustyaiopendatahubiov1alpha1.Condition) error {
	condition.LastTransitionTime = metav1.Now()

	for i, c := range instance.Status.Conditions {
		if c.Type == condition.Type {
			if c.Status != condition.Status || c.Reason != condition.Reason || c.Message != condition.Message {
				instance.Status.Conditions[i] = condition
			}
			return nil
		}
	}

	instance.Status.Conditions = append(instance.Status.Conditions, condition)
	return nil
}

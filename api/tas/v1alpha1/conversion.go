package v1alpha1

import (
	v1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

// ConvertTo converts the v1alpha1 TrustyAIService to v1 TrustyAIService
func (src *TrustyAIService) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1.TrustyAIService)

	// Convert ObjectMeta
	dst.ObjectMeta = src.ObjectMeta

	// Convert Spec
	dst.Spec.Replicas = src.Spec.Replicas
	dst.Spec.Storage.Format = src.Spec.Storage.Format
	dst.Spec.Storage.Folder = src.Spec.Storage.Folder
	dst.Spec.Storage.Size = src.Spec.Storage.Size
	dst.Spec.Storage.DatabaseConfigurations = src.Spec.Storage.DatabaseConfigurations
	dst.Spec.Data.Filename = src.Spec.Data.Filename
	dst.Spec.Data.Format = src.Spec.Data.Format
	dst.Spec.Metrics.Schedule = src.Spec.Metrics.Schedule
	dst.Spec.Metrics.BatchSize = src.Spec.Metrics.BatchSize

	// Convert Status
	dst.Status.Phase = src.Status.Phase
	dst.Status.Replicas = src.Status.Replicas
	dst.Status.Ready = src.Status.Ready

	// Convert Conditions from common.Condition (v1alpha1 src) to metav1.Condition (v1 dst)
	dst.Status.Conditions = make([]metav1.Condition, len(src.Status.Conditions))
	for i, srcCondition := range src.Status.Conditions {
		// Convert corev1.ConditionStatus to metav1.ConditionStatus
		var status metav1.ConditionStatus
		switch srcCondition.Status {
		case corev1.ConditionTrue:
			status = metav1.ConditionTrue
		case corev1.ConditionFalse:
			status = metav1.ConditionFalse
		default:
			status = metav1.ConditionUnknown
		}

		// metav1.Condition requires Reason and Message to be non-empty
		// Provide defaults if missing from v1alpha1 condition
		reason := srcCondition.Reason
		if reason == "" {
			reason = "Unknown"
		}
		message := srcCondition.Message
		if message == "" {
			message = "No message provided"
		}

		dst.Status.Conditions[i] = metav1.Condition{
			Type:               srcCondition.Type,
			Status:             status,
			LastTransitionTime: srcCondition.LastTransitionTime,
			Reason:             reason,
			Message:            message,
		}
	}

	return nil
}

// ConvertFrom converts the v1 TrustyAIService (Hub version) to v1alpha1 TrustyAIService
func (dst *TrustyAIService) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1.TrustyAIService)

	// Convert ObjectMeta
	dst.ObjectMeta = src.ObjectMeta

	// Convert Spec
	dst.Spec.Replicas = src.Spec.Replicas
	dst.Spec.Storage.Format = src.Spec.Storage.Format
	dst.Spec.Storage.Folder = src.Spec.Storage.Folder
	dst.Spec.Storage.Size = src.Spec.Storage.Size
	dst.Spec.Storage.DatabaseConfigurations = src.Spec.Storage.DatabaseConfigurations
	dst.Spec.Data.Filename = src.Spec.Data.Filename
	dst.Spec.Data.Format = src.Spec.Data.Format
	dst.Spec.Metrics.Schedule = src.Spec.Metrics.Schedule
	dst.Spec.Metrics.BatchSize = src.Spec.Metrics.BatchSize

	// Convert Status
	dst.Status.Phase = src.Status.Phase
	dst.Status.Replicas = src.Status.Replicas
	dst.Status.Ready = src.Status.Ready

	// Convert Conditions from metav1.Condition (v1) to common.Condition (v1alpha1)
	dst.Status.Conditions = make([]Condition, len(src.Status.Conditions))
	for i, srcCondition := range src.Status.Conditions {
		// Convert metav1.ConditionStatus to corev1.ConditionStatus
		var status corev1.ConditionStatus
		switch srcCondition.Status {
		case metav1.ConditionTrue:
			status = corev1.ConditionTrue
		case metav1.ConditionFalse:
			status = corev1.ConditionFalse
		default:
			status = corev1.ConditionUnknown
		}

		dst.Status.Conditions[i] = Condition{
			Type:               srcCondition.Type,
			Status:             status,
			LastTransitionTime: srcCondition.LastTransitionTime,
			Reason:             srcCondition.Reason,
			Message:            srcCondition.Message,
		}
	}

	return nil
}

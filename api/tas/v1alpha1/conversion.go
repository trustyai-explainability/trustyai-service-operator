package v1alpha1

import (
	"github.com/trustyai-explainability/trustyai-service-operator/api/common"
	v1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

// ConvertTo converts the v1alpha1 TrustyAIService to v1 TrustyAIService
func (src *TrustyAIService) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1.TrustyAIService)

	// Convert ObjectMeta
	dst.ObjectMeta = src.ObjectMeta
	dst.TypeMeta = src.TypeMeta

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

	// Convert Conditions
	dst.Status.Conditions = make([]common.Condition, len(src.Status.Conditions))
	for i, srcCondition := range src.Status.Conditions {
		dst.Status.Conditions[i] = common.Condition{
			Type:               srcCondition.Type,
			Status:             srcCondition.Status,
			LastTransitionTime: srcCondition.LastTransitionTime,
			Reason:             srcCondition.Reason,
			Message:            srcCondition.Message,
		}
	}

	return nil
}

// ConvertFrom converts the v1 TrustyAIService (Hub version) to v1alpha1 TrustyAIService
func (dst *TrustyAIService) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1.TrustyAIService)

	// Convert ObjectMeta
	dst.ObjectMeta = src.ObjectMeta
	dst.TypeMeta = src.TypeMeta

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

	// Convert Conditions
	dst.Status.Conditions = make([]Condition, len(src.Status.Conditions))
	for i, srcCondition := range src.Status.Conditions {
		dst.Status.Conditions[i] = Condition{
			Type:               srcCondition.Type,
			Status:             srcCondition.Status,
			LastTransitionTime: srcCondition.LastTransitionTime,
			Reason:             srcCondition.Reason,
			Message:            srcCondition.Message,
		}
	}

	return nil
}

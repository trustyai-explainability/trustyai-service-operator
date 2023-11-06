package controllers

import (
	"context"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *TrustyAIServiceReconciler) updateStatus(ctx context.Context, original *trustyaiopendatahubiov1alpha1.TrustyAIService, update func(saved *trustyaiopendatahubiov1alpha1.TrustyAIService),
) (*trustyaiopendatahubiov1alpha1.TrustyAIService, error) {
	saved := &trustyaiopendatahubiov1alpha1.TrustyAIService{}
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := r.Client.Get(ctx, client.ObjectKeyFromObject(original), saved)
		if err != nil {
			return err
		}
		// update status here
		update(saved)

		// Try to update
		err = r.Client.Status().Update(ctx, saved)
		return err
	})
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to update TrustyAIService status")
	}
	return saved, err
}

func UpdateInferenceServiceNotPresent(saved *trustyaiopendatahubiov1alpha1.TrustyAIService) {
	saved.SetStatus(StatusTypeInferenceServicesPresent, StatusReasonInferenceServicesNotFound, "InferenceServices not found", v1.ConditionFalse)
	saved.Status.Ready = v1.ConditionFalse

}

func UpdateInferenceServicePresent(saved *trustyaiopendatahubiov1alpha1.TrustyAIService) {
	saved.SetStatus(StatusTypeInferenceServicesPresent, StatusReasonInferenceServicesFound, "InferenceServices found", v1.ConditionTrue)
}

func UpdatePVCNotAvailable(saved *trustyaiopendatahubiov1alpha1.TrustyAIService) {
	saved.SetStatus(StatusTypePVCAvailable, StatusReasonPVCNotFound, "PersistentVolumeClaim not found", v1.ConditionFalse)
	saved.Status.Phase = "Not Ready"
	saved.Status.Ready = v1.ConditionFalse
}

func UpdatePVCAvailable(saved *trustyaiopendatahubiov1alpha1.TrustyAIService) {
	saved.SetStatus(StatusTypePVCAvailable, StatusReasonPVCFound, "PersistentVolumeClaim found", v1.ConditionTrue)
}

func UpdateRouteAvailable(saved *trustyaiopendatahubiov1alpha1.TrustyAIService) {
	saved.SetStatus(StatusTypeRouteAvailable, StatusReasonRouteFound, "Route found", v1.ConditionTrue)
}

func UpdateRouteNotAvailable(saved *trustyaiopendatahubiov1alpha1.TrustyAIService) {
	saved.SetStatus(StatusTypeRouteAvailable, StatusReasonRouteNotFound, "Route not found", v1.ConditionFalse)
}

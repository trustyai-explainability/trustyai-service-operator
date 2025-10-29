package tas

import (
	"context"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"

	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1alpha1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// IsAllReady checks if all the necessary readiness fields are true for the specific mode
func (rs *AvailabilityStatus) IsAllReady(mode string) bool {
	return (rs.PVCReady && rs.DeploymentReady && rs.RouteReady && mode == STORAGE_PVC) ||
		(rs.DeploymentReady && rs.RouteReady && rs.DBReady && mode == STORAGE_DATABASE)
}

// AvailabilityStatus has the readiness status of various resources.
type AvailabilityStatus struct {
	PVCReady              bool
	DeploymentReady       bool
	RouteReady            bool
	InferenceServiceReady bool
	DBReady               bool
}

func (r *TrustyAIServiceReconciler) updateStatus(ctx context.Context, original *trustyaiopendatahubiov1alpha1.TrustyAIService, update func(saved *trustyaiopendatahubiov1alpha1.TrustyAIService),
) (*trustyaiopendatahubiov1alpha1.TrustyAIService, error) {
	saved := &trustyaiopendatahubiov1alpha1.TrustyAIService{}
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := r.Client.Get(ctx, client.ObjectKeyFromObject(original), saved)
		if err != nil {
			return err
		}
		// Update status here
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

// reconcileStatuses checks the readiness status of required resources
func (r *TrustyAIServiceReconciler) reconcileStatuses(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) (ctrl.Result, error) {
	var err error
	status := AvailabilityStatus{}

	if instance.Spec.Storage.IsStoragePVC() || instance.IsMigration() {
		// Check for PVC readiness
		status.PVCReady, err = r.checkPVCReady(ctx, instance)
	}

	// Check for deployment readiness
	status.DeploymentReady, err = r.checkDeploymentReady(ctx, instance)

	if instance.Spec.Storage.IsStorageDatabase() || instance.IsMigration() {
		status.DBReady, _ = r.checkDatabaseAccessible(ctx, instance)
	}

	// Check for route readiness
	status.RouteReady, err = utils.CheckRouteReady(ctx, r.Client, instance.Name, instance.Namespace)

	// Check if InferenceServices present
	status.InferenceServiceReady, err = r.checkInferenceServicesPresent(ctx, instance.Namespace)

	// All checks passed, resources are ready
	if status.IsAllReady(instance.Spec.Storage.Format) {
		_, updateErr := r.updateStatus(ctx, instance, func(saved *trustyaiopendatahubiov1alpha1.TrustyAIService) {

			if status.InferenceServiceReady {
				UpdateInferenceServicePresent(saved)
			} else {
				UpdateInferenceServiceNotPresent(saved)
			}

			if instance.Spec.Storage.IsStoragePVC() || instance.IsMigration() {
				UpdatePVCAvailable(saved)
			}

			UpdateRouteAvailable(saved)

			if instance.Spec.Storage.IsStorageDatabase() || instance.IsMigration() {
				if status.DBReady {
					UpdateDBAvailable(saved)
				} else {
					UpdateDBConnectionError(saved)
					return
				}
			}

			UpdateTrustyAIServiceAvailable(saved)
			saved.Status.Phase = PhaseReady
			saved.Status.Ready = v1.ConditionTrue
		})
		if updateErr != nil {
			return RequeueWithErrorMessage(ctx, err, "Failed to update status")
		}
	} else {
		_, updateErr := r.updateStatus(ctx, instance, func(saved *trustyaiopendatahubiov1alpha1.TrustyAIService) {

			if status.InferenceServiceReady {
				UpdateInferenceServicePresent(saved)
			} else {
				UpdateInferenceServiceNotPresent(saved)
			}

			if instance.Spec.Storage.IsStoragePVC() || instance.IsMigration() {
				if status.PVCReady {
					UpdatePVCAvailable(saved)
				} else {
					UpdatePVCNotAvailable(saved)
				}
			}

			if instance.Spec.Storage.IsStorageDatabase() || instance.IsMigration() {
				UpdateDBConnectionError(saved)
			}

			if status.RouteReady {
				UpdateRouteAvailable(saved)
			} else {
				UpdateRouteNotAvailable(saved)
			}

			UpdateTrustyAIServiceNotAvailable(saved)
			saved.Status.Phase = PhaseNotReady
			saved.Status.Ready = v1.ConditionFalse
		})
		if updateErr != nil {
			return RequeueWithErrorMessage(ctx, err, "Failed to update status")
		}
	}
	// All resources are reconciled, return no error and do not requeue
	return ctrl.Result{}, nil
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
	saved.Status.Phase = PhaseNotReady
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

func UpdateTrustyAIServiceAvailable(saved *trustyaiopendatahubiov1alpha1.TrustyAIService) {
	saved.SetStatus(StatusTypeAvailable, StatusAvailable, StatusAvailable, v1.ConditionTrue)
}

func UpdateTrustyAIServiceNotAvailable(saved *trustyaiopendatahubiov1alpha1.TrustyAIService) {
	saved.SetStatus(StatusTypeAvailable, StatusNotAvailable, "Not all components available", v1.ConditionFalse)
	saved.Status.Phase = PhaseNotReady
	saved.Status.Ready = v1.ConditionFalse
}

func UpdateDBCredentialsNotFound(saved *trustyaiopendatahubiov1alpha1.TrustyAIService) {
	saved.SetStatus(StatusTypeDBAvailable, StatusDBCredentialsNotFound, "Database credentials not found", v1.ConditionFalse)
	saved.Status.Phase = PhaseNotReady
	saved.Status.Ready = v1.ConditionFalse
}

func UpdateDBCredentialsError(saved *trustyaiopendatahubiov1alpha1.TrustyAIService) {
	saved.SetStatus(StatusTypeDBAvailable, StatusDBCredentialsError, "Error with database credentials", v1.ConditionFalse)
	saved.Status.Phase = PhaseNotReady
	saved.Status.Ready = v1.ConditionFalse
}

func UpdateDBConnectionError(saved *trustyaiopendatahubiov1alpha1.TrustyAIService) {
	saved.SetStatus(StatusTypeDBAvailable, StatusDBConnectionError, "Error connecting to database", v1.ConditionFalse)
	saved.Status.Phase = PhaseNotReady
	saved.Status.Ready = v1.ConditionFalse
}

func UpdateDBAvailable(saved *trustyaiopendatahubiov1alpha1.TrustyAIService) {
	saved.SetStatus(StatusTypeDBAvailable, StatusDBAvailable, "Database available", v1.ConditionTrue)
}

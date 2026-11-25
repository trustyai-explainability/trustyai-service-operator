package tas

import (
	"context"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"

	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	pkgtls "github.com/trustyai-explainability/trustyai-service-operator/pkg/tls"
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
	PVCReady        bool
	DeploymentReady bool
	RouteReady      bool
	DBReady         bool
}

func (r *TrustyAIServiceReconciler) updateStatus(ctx context.Context, original *trustyaiopendatahubiov1.TrustyAIService, update func(saved *trustyaiopendatahubiov1.TrustyAIService),
) (*trustyaiopendatahubiov1.TrustyAIService, error) {
	saved := &trustyaiopendatahubiov1.TrustyAIService{}
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := r.Client.Get(ctx, client.ObjectKeyFromObject(original), saved)
		if err != nil {
			return err
		}
		// Update status here
		update(saved)

		// Set ObservedGeneration to track which generation this status reflects
		saved.Status.ObservedGeneration = saved.Generation

		// Try to update
		err = r.Client.Status().Update(ctx, saved)
		return err
	})
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to update TrustyAIService status")
	}
	return saved, err
}

// reconcileStatuses checks the readiness status of required resources and
// reports the serving state it is given. Serving is passed in rather than read
// here: the caller derives it once per pass, so the writer and this condition
// cannot disagree, and one pass costs one CRD read instead of two.
func (r *TrustyAIServiceReconciler) reconcileStatuses(ctx context.Context, instance *trustyaiopendatahubiov1.TrustyAIService, serving inferenceServicesReport) (ctrl.Result, error) {
	var pvcErr, deploymentErr, routeErr error
	status := AvailabilityStatus{}

	if instance.Spec.Storage.IsStoragePVC() || instance.IsMigration() {
		// Check for PVC readiness
		status.PVCReady, pvcErr = r.checkPVCReady(ctx, instance)
	}

	// Check for deployment readiness
	status.DeploymentReady, deploymentErr = r.checkDeploymentReady(ctx, instance)

	if instance.Spec.Storage.IsStorageDatabase() || instance.IsMigration() {
		status.DBReady, _ = r.checkDatabaseAccessible(ctx, instance)
	}

	// Check for route readiness
	status.RouteReady, routeErr = utils.CheckRouteReady(ctx, r.Client, instance.Name, instance.Namespace)

	// Every condition below is written even when one of the reads above failed:
	// each check reports "not ready" as a state on its own, and the failures are
	// logged after the write rather than suppressing it.

	// All checks passed, resources are ready
	if status.IsAllReady(instance.Spec.Storage.Format) {
		_, updateErr := r.updateStatus(ctx, instance, func(saved *trustyaiopendatahubiov1.TrustyAIService) {

			UpdateInferenceServicesCondition(saved, serving)

			if instance.Spec.Storage.IsStoragePVC() || instance.IsMigration() {
				UpdatePVCAvailable(saved)
			}

			UpdateRouteAvailable(saved)

			if instance.Spec.Storage.IsStorageDatabase() || instance.IsMigration() {
				if status.DBReady {
					UpdateDBAvailable(saved)
				} else {
					UpdateDBConnectionError(saved)
					// Don't return early - set standard conditions for inconsistent state
					saved.Status.Phase = PhaseNotReady
					saved.Status.Ready = v1.ConditionFalse
					saved.SetStatus(ConditionTypeReady, "DBNotReady", "Database is not accessible", v1.ConditionFalse)
					saved.SetStatus(ConditionTypeProvisioningSucceeded, "ProvisioningInProgress", "Service provisioning in progress", v1.ConditionFalse)
					saved.SetStatus(ConditionTypeDegraded, "PartialFunctionality", "PVC components are ready but database is not accessible", v1.ConditionTrue)
					return
				}
			}

			UpdateTrustyAIServiceAvailable(saved)
			saved.Status.Phase = PhaseReady
			saved.Status.Ready = v1.ConditionTrue

			// Set standard platform conditions
			saved.SetStatus(ConditionTypeReady, "AllComponentsReady", "All service components are ready", v1.ConditionTrue)
			saved.SetStatus(ConditionTypeProvisioningSucceeded, "ProvisioningComplete", "Service provisioning completed successfully", v1.ConditionTrue)
			saved.SetStatus(ConditionTypeDegraded, "FullyFunctional", "All components are fully functional", v1.ConditionFalse)
		})
		if updateErr != nil {
			return RequeueWithErrorMessage(ctx, updateErr, "Failed to update status")
		}
	} else {
		_, updateErr := r.updateStatus(ctx, instance, func(saved *trustyaiopendatahubiov1.TrustyAIService) {

			UpdateInferenceServicesCondition(saved, serving)

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

			// Set standard platform conditions
			saved.SetStatus(ConditionTypeReady, "ComponentsNotReady", "Not all service components are ready", v1.ConditionFalse)
			saved.SetStatus(ConditionTypeProvisioningSucceeded, "ProvisioningInProgress", "Service provisioning in progress", v1.ConditionFalse)

			// Set Degraded based on partial availability
			if status.DeploymentReady || status.RouteReady || status.PVCReady || status.DBReady {
				saved.SetStatus(ConditionTypeDegraded, "PartialFunctionality", "Some components are available but service is not fully functional", v1.ConditionTrue)
			} else {
				saved.SetStatus(ConditionTypeDegraded, "NoFunctionality", "No components are available", v1.ConditionTrue)
			}
		})
		if updateErr != nil {
			return RequeueWithErrorMessage(ctx, updateErr, "Failed to update status")
		}
	}

	// None of the required-component reads change the reconcile result: each check
	// already reports "not ready" as a state. They are logged rather than shared
	// through one err variable, which is how the earlier ones were discarded and a
	// read failure disappeared without trace.
	for _, readErr := range []error{pvcErr, deploymentErr, routeErr} {
		if readErr != nil {
			log.FromContext(ctx).Error(readErr, "Could not read a required component's readiness, reporting it as not ready")
		}
	}

	// Serving is the exception: its read failure is returned so the reconcile is
	// retried, and only after the status above was written, because a serving blip
	// may not suppress the conditions this pass established.
	if serving.err != nil {
		return RequeueWithError(serving.err)
	}

	// All resources are reconciled, return no error and do not requeue
	return ctrl.Result{}, nil
}

// UpdateInferenceServicesCondition reports the serving state. Absence of the
// serving CRD, an unsupported served version and an empty namespace are all
// reported but are not degradations, so this never touches Ready: IsAllReady has
// no serving term and the caller owns readiness.
//
// A failed serving read is not a state: the condition keeps its previous value
// rather than flapping to "absent", and the caller returns the error so the
// reconcile is retried.
func UpdateInferenceServicesCondition(saved *trustyaiopendatahubiov1.TrustyAIService, report inferenceServicesReport) {
	if report.err != nil {
		return
	}
	condition := v1.ConditionFalse
	if report.present {
		condition = v1.ConditionTrue
	}
	saved.SetStatus(StatusTypeInferenceServicesPresent, report.reason(), report.message(), condition)
}

func UpdatePVCNotAvailable(saved *trustyaiopendatahubiov1.TrustyAIService) {
	saved.SetStatus(StatusTypePVCAvailable, StatusReasonPVCNotFound, "PersistentVolumeClaim not found", v1.ConditionFalse)
	saved.Status.Phase = PhaseNotReady
	saved.Status.Ready = v1.ConditionFalse
}

func UpdatePVCAvailable(saved *trustyaiopendatahubiov1.TrustyAIService) {
	saved.SetStatus(StatusTypePVCAvailable, StatusReasonPVCFound, "PersistentVolumeClaim found", v1.ConditionTrue)
}

func UpdateRouteAvailable(saved *trustyaiopendatahubiov1.TrustyAIService) {
	saved.SetStatus(StatusTypeRouteAvailable, StatusReasonRouteFound, "Route found", v1.ConditionTrue)
}

func UpdateRouteNotAvailable(saved *trustyaiopendatahubiov1.TrustyAIService) {
	saved.SetStatus(StatusTypeRouteAvailable, StatusReasonRouteNotFound, "Route not found", v1.ConditionFalse)
}

func UpdateTrustyAIServiceAvailable(saved *trustyaiopendatahubiov1.TrustyAIService) {
	saved.SetStatus(StatusTypeAvailable, StatusAvailable, StatusAvailable, v1.ConditionTrue)
}

// UpdateTLSProfileUnsupported reports a cluster TLS profile the kube-rbac-proxy
// sidecars cannot serve. The Deployment is left untouched, so the last known good
// workload keeps serving, and reconcileStatuses rewrites Ready and
// ProvisioningSucceeded as soon as the profile is usable again.
func UpdateTLSProfileUnsupported(saved *trustyaiopendatahubiov1.TrustyAIService, message string) {
	UpdateTrustyAIServiceNotAvailable(saved)
	saved.SetStatus(ConditionTypeReady, pkgtls.ReasonProxyTLSProfileUnsupported, message, v1.ConditionFalse)
	saved.SetStatus(ConditionTypeProvisioningSucceeded, "ProvisioningBlocked", message, v1.ConditionFalse)
}

func UpdateTrustyAIServiceNotAvailable(saved *trustyaiopendatahubiov1.TrustyAIService) {
	saved.SetStatus(StatusTypeAvailable, StatusNotAvailable, "Not all components available", v1.ConditionFalse)
	saved.Status.Phase = PhaseNotReady
	saved.Status.Ready = v1.ConditionFalse
}

func UpdateDBCredentialsNotFound(saved *trustyaiopendatahubiov1.TrustyAIService) {
	saved.SetStatus(StatusTypeDBAvailable, StatusDBCredentialsNotFound, "Database credentials not found", v1.ConditionFalse)
	saved.Status.Phase = PhaseNotReady
	saved.Status.Ready = v1.ConditionFalse
}

func UpdateDBCredentialsError(saved *trustyaiopendatahubiov1.TrustyAIService) {
	saved.SetStatus(StatusTypeDBAvailable, StatusDBCredentialsError, "Error with database credentials", v1.ConditionFalse)
	saved.Status.Phase = PhaseNotReady
	saved.Status.Ready = v1.ConditionFalse
}

func UpdateDBConnectionError(saved *trustyaiopendatahubiov1.TrustyAIService) {
	saved.SetStatus(StatusTypeDBAvailable, StatusDBConnectionError, "Error connecting to database", v1.ConditionFalse)
	saved.Status.Phase = PhaseNotReady
	saved.Status.Ready = v1.ConditionFalse
}

func UpdateDBAvailable(saved *trustyaiopendatahubiov1.TrustyAIService) {
	saved.SetStatus(StatusTypeDBAvailable, StatusDBAvailable, "Database available", v1.ConditionTrue)
}

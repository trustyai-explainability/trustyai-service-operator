package gorch

import (
	"context"
	"fmt"
	"time"
	"errors"

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
	ConditionAvailable         gorchv1alpha1.ConditionType = "Available"
	ConditionDegraded          gorchv1alpha1.ConditionType = "Degraded"
)

const (
	PhaseProgressing = "Progressing"
	PhaseReady       = "Ready"
	PhaseFailed      = "Failed"
	PhaseUnknown     = "Unknown"
)

const (
	// Reconcile reasons
	ReconcileFailed           = "ReconcileFailed"
	ReconcileInit             = "ReconcileInit"
	ReconcileCompleted        = "ReconcileCompleted"
	ReconcileProgressing      = "ReconcileProgressing"

	// Reconcile messages
	ReconcileInitMessage             = "Initializing GuardrailsOrchestrator resources"
	ReconcileCompletedMessage        = "All GuardrailsOrchestrator components are ready and operational"
	ReconcileFailedMessage           = "Failed to reconcile GuardrailsOrchestrator resources"
	ReconcileProgressingMessage      = "GuardrailsOrchestrator reconciliation is in progress"

	// Component-specific messages
	DeploymentReadyMessage           = "GuardrailsOrchestrator deployment is running with all replicas available"
	DeploymentNotReadyMessage        = "Waiting for GuardrailsOrchestrator deployment to become ready"
	DeploymentFailedMessage          = "GuardrailsOrchestrator deployment failed to start"

	InferenceServiceReadyMessage     = "InferenceService is available and ready to serve requests"
	InferenceServiceNotReadyMessage  = "Waiting for InferenceService to become available"
	InferenceServiceNotFoundMessage  = "No InferenceService found in namespace"

	RouteReadyMessage                = "Routes are configured and accessible"
	RouteNotReadyMessage             = "Waiting for routes to be admitted by the router"
	RouteFailedMessage               = "Failed to configure routes"

	ConfigMapNotFoundMessage         = "Required ConfigMap not found"
	ServiceAccountCreatedMessage     = "ServiceAccount created successfully"
	ServiceCreatedMessage            = "Service created and configured"
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

func SetAvailableCondition(conditions *[]gorchv1alpha1.Condition, available bool, reason string, message string) {
	status := corev1.ConditionFalse
	if available {
		status = corev1.ConditionTrue
	}
	SetStatusCondition(conditions, gorchv1alpha1.Condition{
		Type:    ConditionAvailable,
		Status:  status,
		Reason:  reason,
		Message: message,
	})
}

func SetDegradedCondition(conditions *[]gorchv1alpha1.Condition, degraded bool, reason string, message string) {
	status := corev1.ConditionFalse
	if degraded {
		status = corev1.ConditionTrue
	}
	SetStatusCondition(conditions, gorchv1alpha1.Condition{
		Type:    ConditionDegraded,
		Status:  status,
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

// Updated reconcileStatuses function in status.go

func (r *GuardrailsOrchestratorReconciler) reconcileStatuses(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Check component statuses
	generatorReady, generatorErr := r.checkGeneratorPresent(ctx, orchestrator.Namespace)
	deploymentReady, deploymentErr := r.checkDeploymentReady(ctx, orchestrator)
	httpRouteReady, httpRouteErr := r.checkRouteReady(ctx, orchestrator, "")
	healthRouteReady, healthRouteErr := r.checkRouteReady(ctx, orchestrator, "-health")
	routeReady = httpRouteReady && healthRouteReady

	// Check if we have a NoInferenceServicesError
	var noInferenceServicesErr *NoInferenceServicesError
	hasNoInferenceServices := generatorErr != nil && errors.As(generatorErr, &noInferenceServicesErr)

	// Calculate overall status
	// InferenceService is considered a critical dependency - system cannot be ready without it
	allReady := generatorReady && deploymentReady && routeReady

	// System is failed if critical components have errors
	anyFailed := hasNoInferenceServices ||
	             (generatorErr != nil && !errors.As(generatorErr, &noInferenceServicesErr)) ||
	             (deploymentErr != nil && deploymentErr.Error() != "not ready") ||
	             (httpRouteErr != nil && httpRouteErr.Error() != "not ready") ||
	             (healthRouteErr != nil && healthRouteErr.Error() != "not ready")

	// Update status based on component states
	_, updateErr := r.updateStatus(ctx, orchestrator, func(saved *gorchv1alpha1.GuardrailsOrchestrator) {
		// Update InferenceService status with proper error handling
		if hasNoInferenceServices {
			logger.Info("No InferenceServices found in namespace", "namespace", orchestrator.Namespace)
			SetResourceCondition(&saved.Status.Conditions, "InferenceService",
				"InferenceServiceNotFound",
				fmt.Sprintf("No InferenceServices found in namespace '%s'. Please create an InferenceService before deploying GuardrailsOrchestrator", orchestrator.Namespace),
				corev1.ConditionFalse)
		} else if generatorErr != nil {
			logger.Error(generatorErr, "InferenceService check failed")
			SetResourceCondition(&saved.Status.Conditions, "InferenceService",
				"InferenceServiceCheckFailed",
				fmt.Sprintf("Failed to check InferenceService status: %v", generatorErr),
				corev1.ConditionFalse)
		} else if generatorReady {
			SetResourceCondition(&saved.Status.Conditions, "InferenceService",
				"InferenceServiceReady", InferenceServiceReadyMessage, corev1.ConditionTrue)
		} else {
			SetResourceCondition(&saved.Status.Conditions, "InferenceService",
				"InferenceServiceNotReady",
				"InferenceServices exist but none are ready. Waiting for at least one InferenceService to become ready",
				corev1.ConditionFalse)
		}

		// Update Deployment status with more detailed messages
		if deploymentErr != nil && deploymentErr.Error() != "not ready" {
			logger.Error(deploymentErr, "Deployment check failed")
			SetResourceCondition(&saved.Status.Conditions, "Deployment",
				"DeploymentFailed", fmt.Sprintf("%s: %v", DeploymentFailedMessage, deploymentErr), corev1.ConditionFalse)
		} else if deploymentReady {
			SetResourceCondition(&saved.Status.Conditions, "Deployment",
				"DeploymentReady", DeploymentReadyMessage, corev1.ConditionTrue)
		} else {
			SetResourceCondition(&saved.Status.Conditions, "Deployment",
				"DeploymentNotReady", DeploymentNotReadyMessage, corev1.ConditionFalse)
		}

		// Update Route status with combined message for both routes
		if (httpRouteErr != nil && httpRouteErr.Error() != "not ready") ||
		   (healthRouteErr != nil && healthRouteErr.Error() != "not ready") {
			var errMsg string
			if httpRouteErr != nil && httpRouteErr.Error() != "not ready" {
				errMsg = fmt.Sprintf("HTTP route error: %v", httpRouteErr)
			}
			if healthRouteErr != nil && healthRouteErr.Error() != "not ready" {
				if errMsg != "" {
					errMsg += "; "
				}
				errMsg += fmt.Sprintf("Health route error: %v", healthRouteErr)
			}
			logger.Error(fmt.Errorf(errMsg), "Route check failed")
			SetResourceCondition(&saved.Status.Conditions, "Route",
				"RouteFailed", fmt.Sprintf("%s: %s", RouteFailedMessage, errMsg), corev1.ConditionFalse)
		} else if routeReady {
			SetResourceCondition(&saved.Status.Conditions, "Route",
				"RouteReady", RouteReadyMessage, corev1.ConditionTrue)
		} else {
			routeMsg := RouteNotReadyMessage
			if !httpRouteReady && !healthRouteReady {
				routeMsg = "Waiting for both HTTP and health routes to be admitted"
			} else if !httpRouteReady {
				routeMsg = "Waiting for HTTP route to be admitted"
			} else if !healthRouteReady {
				routeMsg = "Waiting for health route to be admitted"
			}
			SetResourceCondition(&saved.Status.Conditions, "Route",
				"RouteNotReady", routeMsg, corev1.ConditionFalse)
		}

		// Set overall status conditions
		if allReady {
			SetCompleteCondition(&saved.Status.Conditions, corev1.ConditionTrue,
				ReconcileCompleted, ReconcileCompletedMessage)
			SetAvailableCondition(&saved.Status.Conditions, true,
				"AllComponentsReady", "All GuardrailsOrchestrator components are operational")
			SetDegradedCondition(&saved.Status.Conditions, false,
				"NoIssues", "No degradation detected")
			SetProgressingCondition(&saved.Status.Conditions,
				ReconcileCompleted, "Reconciliation complete")
			saved.Status.Phase = PhaseReady
		} else if hasNoInferenceServices {
			// Special handling for missing InferenceServices - this is a blocking error
			SetCompleteCondition(&saved.Status.Conditions, corev1.ConditionFalse,
				"MissingDependency",
				fmt.Sprintf("Cannot complete reconciliation: No InferenceServices found in namespace '%s'", orchestrator.Namespace))
			SetAvailableCondition(&saved.Status.Conditions, false,
				"InferenceServiceMissing",
				"GuardrailsOrchestrator requires at least one InferenceService to be present")
			SetDegradedCondition(&saved.Status.Conditions, true,
				"MissingCriticalComponent",
				"System is degraded due to missing InferenceService")
			SetProgressingCondition(&saved.Status.Conditions,
				"WaitingForDependency",
				fmt.Sprintf("Waiting for InferenceService to be created in namespace '%s'", orchestrator.Namespace))
			saved.Status.Phase = PhaseFailed
		} else if anyFailed {
			SetCompleteCondition(&saved.Status.Conditions, corev1.ConditionFalse,
				ReconcileFailed, ReconcileFailedMessage)
			SetAvailableCondition(&saved.Status.Conditions, false,
				"ComponentsFailed", "One or more components have failed")
			SetDegradedCondition(&saved.Status.Conditions, true,
				"ComponentFailure", "System is degraded due to component failures")
			SetProgressingCondition(&saved.Status.Conditions,
				ReconcileFailed, "Reconciliation failed with errors")
			saved.Status.Phase = PhaseFailed
		} else {
			// Components are still coming up
			SetCompleteCondition(&saved.Status.Conditions, corev1.ConditionFalse,
				ReconcileProgressing, ReconcileProgressingMessage)
			SetAvailableCondition(&saved.Status.Conditions, false,
				"ComponentsNotReady", "Waiting for all components to become ready")
			SetDegradedCondition(&saved.Status.Conditions, false,
				"Initializing", "Components are initializing")
			SetProgressingCondition(&saved.Status.Conditions,
				ReconcileProgressing, getProgressMessage(generatorReady, deploymentReady, routeReady))
			saved.Status.Phase = PhaseProgressing
		}
	})

	if updateErr != nil {
		logger.Error(updateErr, "Failed to update GuardrailsOrchestrator status")
		return ctrl.Result{}, updateErr
	}

	// If InferenceServices are missing, requeue more frequently to detect when they're created
	if hasNoInferenceServices {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

// Helper function to generate a detailed progress message
func getProgressMessage(generatorReady, deploymentReady, routeReady bool) string {
	readyComponents := []string{}
	waitingComponents := []string{}

	if generatorReady {
		readyComponents = append(readyComponents, "InferenceService")
	} else {
		waitingComponents = append(waitingComponents, "InferenceService")
	}

	if deploymentReady {
		readyComponents = append(readyComponents, "Deployment")
	} else {
		waitingComponents = append(waitingComponents, "Deployment")
	}

	if routeReady {
		readyComponents = append(readyComponents, "Routes")
	} else {
		waitingComponents = append(waitingComponents, "Routes")
	}

	message := "GuardrailsOrchestrator reconciliation in progress"
	if len(readyComponents) > 0 {
		message += fmt.Sprintf(". Ready: %v", readyComponents)
	}
	if len(waitingComponents) > 0 {
		message += fmt.Sprintf(". Waiting for: %v", waitingComponents)
	}

	return message
}
package module

import (
	"context"
	"fmt"
	"strings"
	"time"

	modulev1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/module/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// ControllerSetUp sets up the controller with the Manager
func ControllerSetUp(mgr manager.Manager, ns, operatorConfigMapName string, recorder record.EventRecorder, healthCheckers []ServiceHealthChecker) error {
	return (&TrustyAIReconciler{
		Client:                mgr.GetClient(),
		Scheme:                mgr.GetScheme(),
		Namespace:             ns,
		OperatorConfigMapName: operatorConfigMapName,
		EventRecorder:         recorder,
		HealthCheckers:        healthCheckers,
	}).SetupWithManager(mgr)
}

// TrustyAIReconciler reconciles a TrustyAI module object
type TrustyAIReconciler struct {
	client.Client
	Scheme                *runtime.Scheme
	Namespace             string
	OperatorConfigMapName string
	EventRecorder         record.EventRecorder
	HealthCheckers        []ServiceHealthChecker
}

//+kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=trustyais,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=trustyais/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=trustyais/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *TrustyAIReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the TrustyAI module instance
	module := &modulev1alpha1.TrustyAI{}
	err := r.Get(ctx, req.NamespacedName, module)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request
			logger.Info("TrustyAI module resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request
		logger.Error(err, "Failed to get TrustyAI module")
		return ctrl.Result{}, err
	}

	logger.Info("Reconciling TrustyAI module", "name", module.Name)

	// Handle deletion
	if module.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, module)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(module, FinalizerName) {
		controllerutil.AddFinalizer(module, FinalizerName)
		if err := r.Update(ctx, module); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		logger.Info("Added finalizer to TrustyAI module")
		// Requeue to continue reconciliation
		return ctrl.Result{Requeue: true}, nil
	}

	// Update observedGeneration
	module.Status.ObservedGeneration = module.Generation

	// Initialize status if needed
	if module.Status.Phase == "" {
		module.Status.Phase = PhaseNotReady
	}

	// Reconcile based on management state
	if module.Spec.ManagementState == modulev1alpha1.ManagementStateRemoved {
		return r.handleRemoval(ctx, module)
	}

	// Run health checks and update conditions
	if err := r.updateHealthStatus(ctx, module); err != nil {
		logger.Error(err, "Failed to update health status")
		return ctrl.Result{}, err
	}

	// Update releases information
	r.updateReleases(module)

	// Update status
	if err := r.Status().Update(ctx, module); err != nil {
		logger.Error(err, "Failed to update TrustyAI module status")
		return ctrl.Result{}, err
	}

	// Requeue after interval for periodic health checks
	return ctrl.Result{RequeueAfter: time.Duration(DefaultRequeueInterval) * time.Second}, nil
}

// handleDeletion handles the deletion of the TrustyAI module
func (r *TrustyAIReconciler) handleDeletion(ctx context.Context, module *modulev1alpha1.TrustyAI) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if controllerutil.ContainsFinalizer(module, FinalizerName) {
		logger.Info("Performing cleanup for TrustyAI module")

		// Perform any cleanup operations here
		// For now, we just remove the finalizer as the operator will clean up its own resources

		controllerutil.RemoveFinalizer(module, FinalizerName)
		if err := r.Update(ctx, module); err != nil {
			logger.Error(err, "Failed to remove finalizer")
			return ctrl.Result{}, err
		}
		logger.Info("Removed finalizer from TrustyAI module")
	}

	return ctrl.Result{}, nil
}

// handleRemoval handles the removal management state
func (r *TrustyAIReconciler) handleRemoval(ctx context.Context, module *modulev1alpha1.TrustyAI) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("TrustyAI module is in Removed state, skipping reconciliation")

	// Update phase and conditions
	module.Status.Phase = PhaseNotReady
	meta.SetStatusCondition(&module.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeReady,
		Status:             metav1.ConditionFalse,
		Reason:             "ModuleRemoved",
		Message:            "Module management state is set to Removed",
		ObservedGeneration: module.Generation,
	})

	// Update status
	if err := r.Status().Update(ctx, module); err != nil {
		logger.Error(err, "Failed to update TrustyAI module status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// updateHealthStatus runs health checks and updates status conditions
func (r *TrustyAIReconciler) updateHealthStatus(ctx context.Context, module *modulev1alpha1.TrustyAI) error {
	logger := log.FromContext(ctx)

	allHealthy := true
	partiallyHealthy := false
	unhealthyReasons := []string{}

	// Check each enabled service against health checkers
	for _, checker := range r.HealthCheckers {
		healthy, reason := checker.IsHealthy(ctx)

		if !healthy {
			allHealthy = false
			unhealthyReasons = append(unhealthyReasons, fmt.Sprintf("%s: %s", checker.Name(), reason))
			logger.Info("Service unhealthy", "service", checker.Name(), "reason", reason)
		} else {
			partiallyHealthy = true // At least one service is healthy
		}
	}

	// Set conditions based on aggregated health
	if allHealthy {
		module.Status.Phase = PhaseReady

		meta.SetStatusCondition(&module.Status.Conditions, metav1.Condition{
			Type:               ConditionTypeReady,
			Status:             metav1.ConditionTrue,
			Reason:             "AllServicesHealthy",
			Message:            "All enabled services are healthy",
			ObservedGeneration: module.Generation,
		})

		meta.SetStatusCondition(&module.Status.Conditions, metav1.Condition{
			Type:               ConditionTypeProvisioningSucceeded,
			Status:             metav1.ConditionTrue,
			Reason:             "ProvisioningComplete",
			Message:            "Module provisioning completed successfully",
			ObservedGeneration: module.Generation,
		})

		meta.SetStatusCondition(&module.Status.Conditions, metav1.Condition{
			Type:               ConditionTypeDegraded,
			Status:             metav1.ConditionFalse,
			Reason:             "FullyFunctional",
			Message:            "All services are fully functional",
			ObservedGeneration: module.Generation,
		})
	} else {
		module.Status.Phase = PhaseNotReady

		meta.SetStatusCondition(&module.Status.Conditions, metav1.Condition{
			Type:               ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             "ServicesUnhealthy",
			Message:            strings.Join(unhealthyReasons, "; "),
			ObservedGeneration: module.Generation,
		})

		// Set Degraded=True if some (but not all) services are healthy
		if partiallyHealthy {
			meta.SetStatusCondition(&module.Status.Conditions, metav1.Condition{
				Type:               ConditionTypeDegraded,
				Status:             metav1.ConditionTrue,
				Reason:             "PartialFunctionality",
				Message:            "Some services are unavailable: " + strings.Join(unhealthyReasons, "; "),
				ObservedGeneration: module.Generation,
			})
		}
	}

	logger.Info("Updated health status", "phase", module.Status.Phase, "ready", meta.IsStatusConditionTrue(module.Status.Conditions, ConditionTypeReady))

	return nil
}

// updateReleases populates the releases field with component version information
func (r *TrustyAIReconciler) updateReleases(module *modulev1alpha1.TrustyAI) {
	// TODO: Get actual component versions from somewhere (ConfigMap, image tags, etc.)
	// For now, we'll set placeholder data
	module.Status.Releases = []modulev1alpha1.ComponentRelease{
		{
			Name:    "trustyai-service-operator",
			Version: "unknown", // TODO: Get from operator metadata
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *TrustyAIReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&modulev1alpha1.TrustyAI{}).
		Complete(r)
}

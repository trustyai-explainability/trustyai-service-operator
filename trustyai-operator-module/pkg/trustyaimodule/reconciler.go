package trustyaimodule

import (
	"context"
	"fmt"
	"strings"
	"time"

	platformv1alpha1 "github.com/trustyai-explainability/trustyai-operator-module/pkg/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Version is the operator version, injected at build time via -ldflags.
var Version = "unknown"

// TrustyAIModuleReconciler reconciles TrustyAI module objects.
type TrustyAIModuleReconciler struct {
	client.Client
	Scheme                *runtime.Scheme
	Namespace             string
	ManifestsTemplatePath string
	EventRecorder         record.EventRecorder
	SkipDependencyChecks  bool // set to true in tests to skip external dependency checks
}

// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=trustyais,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=trustyais/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=trustyais/finalizers,verbs=update
// +kubebuilder:rbac:groups=serving.kserve.io,resources=inferenceservices,verbs=get;list
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=prometheuses,verbs=get;list
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch;update
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;patch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete

func (r *TrustyAIModuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	module := &platformv1alpha1.TrustyAI{}
	if err := r.Get(ctx, req.NamespacedName, module); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("TrustyAI module resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get TrustyAI module")
		return ctrl.Result{}, err
	}

	oldStatus := module.Status.DeepCopy()

	logger.Info("Reconciling TrustyAI module", "name", module.Name)

	if module.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, module)
	}

	if !controllerutil.ContainsFinalizer(module, FinalizerName) {
		controllerutil.AddFinalizer(module, FinalizerName)
		if err := r.Update(ctx, module); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		logger.Info("Added finalizer to TrustyAI module")
		r.EventRecorder.Event(module, "Normal", "FinalizerAdded", "Finalizer added to TrustyAI module")
		return ctrl.Result{Requeue: true}, nil
	}

	if err := r.adoptInTreeResources(ctx, module); err != nil {
		logger.Error(err, "Failed to adopt in-tree resources")
		r.EventRecorder.Event(module, "Warning", "MigrationFailed", fmt.Sprintf("SSA adoption failed: %v", err))

		module.Status.Phase = PhaseNotReady
		apimeta.SetStatusCondition(&module.Status.Conditions, metav1.Condition{
			Type:               ConditionTypeProvisioningSucceeded,
			Status:             metav1.ConditionFalse,
			Reason:             "MigrationFailed",
			Message:            fmt.Sprintf("SSA adoption failed: %v", err),
			ObservedGeneration: module.Generation,
		})
		if statusErr := r.Status().Update(ctx, module); statusErr != nil {
			logger.Error(statusErr, "Failed to update status after migration failure")
		}
		return ctrl.Result{}, err
	}

	module.Status.ObservedGeneration = module.Generation
	if module.Status.Phase == "" {
		module.Status.Phase = PhaseNotReady
	}

	if module.Spec.ManagementState == platformv1alpha1.ManagementStateRemoved {
		return r.handleRemoval(ctx, module, oldStatus)
	}
	if module.Spec.ManagementState == platformv1alpha1.ManagementStateUnmanaged {
		return r.handleUnmanaged(ctx, module, oldStatus)
	}

	if !r.SkipDependencyChecks {
		dependencyResults, err := r.checkDependencies(ctx)
		if err != nil {
			logger.Error(err, "Failed to check dependencies")
			return ctrl.Result{}, err
		}

		if allDependenciesSatisfied(dependencyResults) {
			apimeta.SetStatusCondition(&module.Status.Conditions, metav1.Condition{
				Type:               ConditionTypeDependenciesMet,
				Status:             metav1.ConditionTrue,
				Reason:             "AllDependenciesMet",
				Message:            formatDependencyMessages(dependencyResults),
				ObservedGeneration: module.Generation,
			})
		} else {
			apimeta.SetStatusCondition(&module.Status.Conditions, metav1.Condition{
				Type:               ConditionTypeDependenciesMet,
				Status:             metav1.ConditionFalse,
				Reason:             "DependenciesMissing",
				Message:            formatDependencyMessages(dependencyResults),
				ObservedGeneration: module.Generation,
			})
			module.Status.Phase = PhaseNotReady
			apimeta.SetStatusCondition(&module.Status.Conditions, metav1.Condition{
				Type:               ConditionTypeReady,
				Status:             metav1.ConditionFalse,
				Reason:             "DependenciesMissing",
				Message:            "Cannot deploy: " + formatDependencyMessages(dependencyResults),
				ObservedGeneration: module.Generation,
			})
			apimeta.SetStatusCondition(&module.Status.Conditions, metav1.Condition{
				Type:               ConditionTypeProvisioningSucceeded,
				Status:             metav1.ConditionFalse,
				Reason:             "DependenciesMissing",
				Message:            "Cannot provision: " + formatDependencyMessages(dependencyResults),
				ObservedGeneration: module.Generation,
			})
			apimeta.SetStatusCondition(&module.Status.Conditions, metav1.Condition{
				Type:               ConditionTypeDegraded,
				Status:             metav1.ConditionFalse,
				Reason:             "DependenciesMissing",
				Message:            "Module is not deployed",
				ObservedGeneration: module.Generation,
			})

			if err := r.Status().Update(ctx, module); err != nil {
				logger.Error(err, "Failed to update status after dependency check")
				return ctrl.Result{}, err
			}
			logger.Info("Blocking deployment due to missing dependencies", "message", formatDependencyMessages(dependencyResults))
			return ctrl.Result{RequeueAfter: time.Duration(DefaultRequeueInterval) * time.Second}, nil
		}
	} else {
		apimeta.SetStatusCondition(&module.Status.Conditions, metav1.Condition{
			Type:               ConditionTypeDependenciesMet,
			Status:             metav1.ConditionTrue,
			Reason:             "ChecksSkipped",
			Message:            "Dependency checks skipped (test mode)",
			ObservedGeneration: module.Generation,
		})
	}

	if err := r.reconcileConfigMap(ctx, module); err != nil {
		logger.Error(err, "Failed to reconcile DSC ConfigMap")
		return ctrl.Result{}, err
	}

	if err := r.updateHealthStatus(ctx, module); err != nil {
		logger.Error(err, "Failed to update health status")
		return ctrl.Result{}, err
	}

	r.updateReleases(module)

	if !equality.Semantic.DeepEqual(oldStatus, &module.Status) {
		if err := r.updateStatus(ctx, module, func(saved *platformv1alpha1.TrustyAI) {
			saved.Status = *module.Status.DeepCopy()
		}); err != nil {
			logger.Error(err, "Failed to update TrustyAI module status")
			return ctrl.Result{}, err
		}
		r.EventRecorder.Event(module, corev1.EventTypeNormal, EventReasonStatusUpdated,
			fmt.Sprintf("Module status updated, phase: %s", module.Status.Phase))
	}

	return ctrl.Result{RequeueAfter: time.Duration(DefaultRequeueInterval) * time.Second}, nil
}

func (r *TrustyAIModuleReconciler) handleDeletion(ctx context.Context, module *platformv1alpha1.TrustyAI) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if controllerutil.ContainsFinalizer(module, FinalizerName) {
		logger.Info("Performing cleanup for TrustyAI module")
		r.EventRecorder.Event(module, "Normal", "Cleanup", "Starting cleanup for TrustyAI module")

		if err := r.deleteDSCConfigMap(ctx); err != nil {
			logger.Error(err, "Failed to delete DSC ConfigMap during cleanup")
			r.EventRecorder.Event(module, "Warning", "CleanupFailed", "Failed to delete DSC ConfigMap during cleanup")
			return ctrl.Result{}, err
		}

		controllerutil.RemoveFinalizer(module, FinalizerName)
		if err := r.Update(ctx, module); err != nil {
			logger.Error(err, "Failed to remove finalizer")
			return ctrl.Result{}, err
		}
		logger.Info("Removed finalizer from TrustyAI module")
		r.EventRecorder.Event(module, "Normal", "FinalizerRemoved", "Finalizer removed from TrustyAI module")
	}

	return ctrl.Result{}, nil
}

func (r *TrustyAIModuleReconciler) handleRemoval(ctx context.Context, module *platformv1alpha1.TrustyAI, oldStatus *platformv1alpha1.TrustyAIStatus) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("TrustyAI module is in Removed state, skipping reconciliation")

	module.Status.Phase = PhaseNotReady

	apimeta.SetStatusCondition(&module.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeReady,
		Status:             metav1.ConditionFalse,
		Reason:             "ModuleRemoved",
		Message:            "Module management state is set to Removed",
		ObservedGeneration: module.Generation,
	})
	apimeta.SetStatusCondition(&module.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeProvisioningSucceeded,
		Status:             metav1.ConditionFalse,
		Reason:             "ModuleRemoved",
		Message:            "Module management state is set to Removed",
		ObservedGeneration: module.Generation,
	})
	apimeta.SetStatusCondition(&module.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "ModuleRemoved",
		Message:            "Module is not deployed",
		ObservedGeneration: module.Generation,
	})

	if !equality.Semantic.DeepEqual(oldStatus, &module.Status) {
		if err := r.updateStatus(ctx, module, func(saved *platformv1alpha1.TrustyAI) {
			saved.Status = *module.Status.DeepCopy()
		}); err != nil {
			logger.Error(err, "Failed to update TrustyAI module status")
			return ctrl.Result{}, err
		}
		r.EventRecorder.Event(module, corev1.EventTypeNormal, EventReasonRemoved,
			"Module management state is Removed; reconciliation skipped")
	}

	return ctrl.Result{}, nil
}

func (r *TrustyAIModuleReconciler) handleUnmanaged(ctx context.Context, module *platformv1alpha1.TrustyAI, oldStatus *platformv1alpha1.TrustyAIStatus) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("TrustyAI module is in Unmanaged state, skipping reconciliation")

	apimeta.SetStatusCondition(&module.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeReady,
		Status:             metav1.ConditionUnknown,
		Reason:             ReasonModuleUnmanaged,
		Message:            "Module management state is set to Unmanaged",
		ObservedGeneration: module.Generation,
	})
	apimeta.SetStatusCondition(&module.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeProvisioningSucceeded,
		Status:             metav1.ConditionUnknown,
		Reason:             ReasonModuleUnmanaged,
		Message:            "Module management state is set to Unmanaged",
		ObservedGeneration: module.Generation,
	})
	apimeta.SetStatusCondition(&module.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeDegraded,
		Status:             metav1.ConditionUnknown,
		Reason:             ReasonModuleUnmanaged,
		Message:            "Module management state is set to Unmanaged",
		ObservedGeneration: module.Generation,
	})

	if !equality.Semantic.DeepEqual(oldStatus, &module.Status) {
		if err := r.updateStatus(ctx, module, func(saved *platformv1alpha1.TrustyAI) {
			saved.Status = *module.Status.DeepCopy()
		}); err != nil {
			logger.Error(err, "Failed to update TrustyAI module status")
			return ctrl.Result{}, err
		}
		r.EventRecorder.Event(module, corev1.EventTypeNormal, EventReasonUnmanaged,
			"Module management state is Unmanaged; reconciliation skipped")
	}

	return ctrl.Result{}, nil
}

func (r *TrustyAIModuleReconciler) updateStatus(
	ctx context.Context,
	original *platformv1alpha1.TrustyAI,
	update func(saved *platformv1alpha1.TrustyAI),
) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		saved := &platformv1alpha1.TrustyAI{}
		if err := r.Client.Get(ctx, client.ObjectKeyFromObject(original), saved); err != nil {
			return err
		}
		update(saved)
		return r.Client.Status().Update(ctx, saved)
	})
}

func (r *TrustyAIModuleReconciler) buildHealthCheckers(module *platformv1alpha1.TrustyAI) []ServiceHealthChecker {
	var checkers []ServiceHealthChecker
	es := module.Spec.EnabledServices
	if es.TAS {
		checkers = append(checkers, NewRunningServiceChecker("TAS", r.Client, r.Namespace))
	}
	if es.LMES {
		checkers = append(checkers, NewRunningServiceChecker("LMES", r.Client, r.Namespace))
	}
	if es.EvalHub {
		checkers = append(checkers, NewRunningServiceChecker("EVALHUB", r.Client, r.Namespace))
	}
	if es.GORCH {
		checkers = append(checkers, NewRunningServiceChecker("GORCH", r.Client, r.Namespace))
	}
	if es.NemoGuardrails {
		checkers = append(checkers, NewRunningServiceChecker("NEMO_GUARDRAILS", r.Client, r.Namespace))
	}
	return checkers
}

func (r *TrustyAIModuleReconciler) updateHealthStatus(ctx context.Context, module *platformv1alpha1.TrustyAI) error {
	logger := log.FromContext(ctx)

	var (
		oldReadyStatus metav1.ConditionStatus
		hadReady       bool
	)
	if c := apimeta.FindStatusCondition(module.Status.Conditions, ConditionTypeReady); c != nil {
		oldReadyStatus = c.Status
		hadReady = true
	}

	healthCheckers := r.buildHealthCheckers(module)
	allHealthy := true
	partiallyHealthy := false
	var unhealthyReasons []string

	for _, checker := range healthCheckers {
		healthy, reason := checker.IsHealthy(ctx)
		if !healthy {
			allHealthy = false
			unhealthyReasons = append(unhealthyReasons, fmt.Sprintf("%s: %s", checker.Name(), reason))
			logger.Info("Service unhealthy", "service", checker.Name(), "reason", reason)
		} else {
			partiallyHealthy = true
		}
	}

	if allHealthy {
		module.Status.Phase = PhaseReady
		apimeta.SetStatusCondition(&module.Status.Conditions, metav1.Condition{
			Type:               ConditionTypeReady,
			Status:             metav1.ConditionTrue,
			Reason:             "AllServicesHealthy",
			Message:            "All enabled services are healthy",
			ObservedGeneration: module.Generation,
		})
		apimeta.SetStatusCondition(&module.Status.Conditions, metav1.Condition{
			Type:               ConditionTypeProvisioningSucceeded,
			Status:             metav1.ConditionTrue,
			Reason:             "ProvisioningComplete",
			Message:            "Module provisioning completed successfully",
			ObservedGeneration: module.Generation,
		})
		apimeta.SetStatusCondition(&module.Status.Conditions, metav1.Condition{
			Type:               ConditionTypeDegraded,
			Status:             metav1.ConditionFalse,
			Reason:             "FullyFunctional",
			Message:            "All services are fully functional",
			ObservedGeneration: module.Generation,
		})
	} else {
		module.Status.Phase = PhaseNotReady
		apimeta.SetStatusCondition(&module.Status.Conditions, metav1.Condition{
			Type:               ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             "ServicesUnhealthy",
			Message:            strings.Join(unhealthyReasons, "; "),
			ObservedGeneration: module.Generation,
		})
		apimeta.SetStatusCondition(&module.Status.Conditions, metav1.Condition{
			Type:               ConditionTypeProvisioningSucceeded,
			Status:             metav1.ConditionFalse,
			Reason:             "ServicesUnhealthy",
			Message:            "One or more services are not healthy",
			ObservedGeneration: module.Generation,
		})

		if partiallyHealthy {
			apimeta.SetStatusCondition(&module.Status.Conditions, metav1.Condition{
				Type:               ConditionTypeDegraded,
				Status:             metav1.ConditionTrue,
				Reason:             "PartialFunctionality",
				Message:            "Some services are unavailable: " + strings.Join(unhealthyReasons, "; "),
				ObservedGeneration: module.Generation,
			})
		} else {
			apimeta.SetStatusCondition(&module.Status.Conditions, metav1.Condition{
				Type:               ConditionTypeDegraded,
				Status:             metav1.ConditionTrue,
				Reason:             "AllServicesUnhealthy",
				Message:            "All services are unavailable: " + strings.Join(unhealthyReasons, "; "),
				ObservedGeneration: module.Generation,
			})
		}
	}

	logger.Info("Updated health status", "phase", module.Status.Phase,
		"ready", apimeta.IsStatusConditionTrue(module.Status.Conditions, ConditionTypeReady))

	newReady := apimeta.FindStatusCondition(module.Status.Conditions, ConditionTypeReady)
	if !hadReady || newReady == nil || oldReadyStatus != newReady.Status {
		if allHealthy {
			r.EventRecorder.Event(module, "Normal", "HealthCheckPassed", "All enabled services are healthy")
		} else if partiallyHealthy {
			r.EventRecorder.Event(module, "Warning", "HealthCheckPartial", "Some services are unhealthy")
		} else {
			r.EventRecorder.Event(module, "Warning", "HealthCheckFailed", "All services are unhealthy")
		}
	}

	return nil
}

func (r *TrustyAIModuleReconciler) updateReleases(module *platformv1alpha1.TrustyAI) {
	module.Status.Releases = []platformv1alpha1.ComponentRelease{
		{
			Name:    "trustyai-operator-module",
			Version: Version,
		},
	}
}

func (r *TrustyAIModuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.TrustyAI{}).
		Complete(r)
}

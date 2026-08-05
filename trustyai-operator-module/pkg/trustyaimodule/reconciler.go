package trustyaimodule

import (
	"context"
	"fmt"
	"strings"
	"time"

	common "github.com/opendatahub-io/odh-platform-utilities/api/common"
	"github.com/opendatahub-io/odh-platform-utilities/pkg/cluster"
	"github.com/opendatahub-io/odh-platform-utilities/pkg/controller/action"
	"github.com/opendatahub-io/odh-platform-utilities/pkg/controller/conditions"
	"github.com/opendatahub-io/odh-platform-utilities/pkg/controller/precondition"
	"github.com/opendatahub-io/odh-platform-utilities/pkg/deploy"
	statusPkg "github.com/opendatahub-io/odh-platform-utilities/pkg/status"
	platformv1alpha1 "github.com/trustyai-explainability/trustyai-operator-module/pkg/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
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
	Deployer              *deploy.Deployer
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

		condMgr := r.newConditionManager(module)
		condMgr.MarkFalse(string(common.ConditionTypeProvisioningSucceeded),
			conditions.WithReason("MigrationFailed"),
			conditions.WithMessage("SSA adoption failed: %v", err),
			conditions.WithObservedGeneration(module.Generation),
		)
		module.Status.ObservedGeneration = module.Generation
		condMgr.Sort()
		desired := module.Status.DeepCopy()
		if updateErr := statusPkg.Update(ctx, r.Client, module, func(o *platformv1alpha1.TrustyAI) {
			o.Status = *desired
		}); updateErr != nil {
			logger.Error(updateErr, "Failed to update status after migration failure")
		}
		return ctrl.Result{}, err
	}

	module.Status.ObservedGeneration = module.Generation
	if module.Status.Phase == "" {
		module.Status.Phase = common.PhaseNotReady
	}

	if module.Spec.ManagementState == common.Removed {
		return r.handleRemoval(ctx, module)
	}

	// Build the condition manager for this reconcile cycle.
	condMgr := r.newConditionManager(module)

	// Run pre-conditions (dependency checks) unless explicitly skipped (test mode).
	if !r.SkipDependencyChecks {
		rr := &action.ReconciliationRequest{
			Client:     r.Client,
			Instance:   module,
			Conditions: condMgr,
		}
		if stop := precondition.RunAll(ctx, rr, cluster.ClusterType(""), modulePreConditions); stop {
			module.Status.Phase = common.PhaseNotReady
			module.Status.ObservedGeneration = module.Generation
			condMgr.Sort()
			desired := module.Status.DeepCopy()
			if err := statusPkg.Update(ctx, r.Client, module, func(o *platformv1alpha1.TrustyAI) {
				o.Status = *desired
			}); err != nil {
				logger.Error(err, "Failed to update status after dependency check")
				return ctrl.Result{}, err
			}
			logger.Info("Blocking deployment due to missing dependencies")
			return ctrl.Result{RequeueAfter: time.Duration(DefaultRequeueInterval) * time.Second}, nil
		}
	} else {
		condMgr.MarkTrue(ConditionTypeDependenciesAvailable,
			conditions.WithReason("ChecksSkipped"),
			conditions.WithMessage("Dependency checks skipped (test mode)"),
			conditions.WithObservedGeneration(module.Generation),
		)
		condMgr.MarkTrue(ConditionTypeKServeAvailable,
			conditions.WithReason("ChecksSkipped"),
			conditions.WithMessage("Dependency checks skipped (test mode)"),
			conditions.WithObservedGeneration(module.Generation),
		)
	}

	if err := r.reconcileComponent(ctx, module, condMgr); err != nil {
		logger.Error(err, "Failed to deploy workload operator")
		module.Status.ObservedGeneration = module.Generation
		condMgr.Sort()
		desired := module.Status.DeepCopy()
		if updateErr := statusPkg.Update(ctx, r.Client, module, func(o *platformv1alpha1.TrustyAI) {
			o.Status = *desired
		}); updateErr != nil {
			logger.Error(updateErr, "Failed to update status after deploy failure")
		}
		return ctrl.Result{}, err
	}

	if err := r.reconcileConfigMap(ctx, module); err != nil {
		logger.Error(err, "Failed to reconcile DSC ConfigMap")
		return ctrl.Result{}, err
	}

	r.updateHealthStatus(ctx, module, condMgr)
	r.updateReleases(module)

	module.Status.ObservedGeneration = module.Generation
	condMgr.Sort()
	desired := module.Status.DeepCopy()
	if err := statusPkg.Update(ctx, r.Client, module, func(o *platformv1alpha1.TrustyAI) {
		o.Status = *desired
	}); err != nil {
		logger.Error(err, "Failed to update TrustyAI module status")
		return ctrl.Result{}, err
	}
	r.EventRecorder.Event(module, corev1.EventTypeNormal, EventReasonStatusUpdated,
		fmt.Sprintf("Module status updated, phase: %s", module.Status.Phase))

	return ctrl.Result{RequeueAfter: time.Duration(DefaultRequeueInterval) * time.Second}, nil
}

// newConditionManager creates a conditions.Manager bound to module, pre-registering
// the standard set of condition types used by TrustyAI.
func (r *TrustyAIModuleReconciler) newConditionManager(module *platformv1alpha1.TrustyAI) *conditions.Manager {
	return conditions.NewManager(
		module,
		string(common.ConditionTypeReady),
		string(common.ConditionTypeProvisioningSucceeded),
		string(common.ConditionTypeDegraded),
		ConditionTypeDependenciesAvailable,
	)
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

func (r *TrustyAIModuleReconciler) handleRemoval(ctx context.Context, module *platformv1alpha1.TrustyAI) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("TrustyAI module is in Removed state, skipping reconciliation")

	condMgr := r.newConditionManager(module)
	condMgr.MarkFalse(string(common.ConditionTypeReady),
		conditions.WithReason("ModuleRemoved"),
		conditions.WithMessage("Module management state is set to Removed"),
		conditions.WithObservedGeneration(module.Generation),
	)
	condMgr.MarkFalse(string(common.ConditionTypeProvisioningSucceeded),
		conditions.WithReason("ModuleRemoved"),
		conditions.WithMessage("Module management state is set to Removed"),
		conditions.WithObservedGeneration(module.Generation),
	)
	condMgr.MarkFalse(string(common.ConditionTypeDegraded),
		conditions.WithReason("ModuleRemoved"),
		conditions.WithMessage("Module is not deployed"),
		conditions.WithObservedGeneration(module.Generation),
	)

	module.Status.Phase = common.PhaseNotReady
	module.Status.ObservedGeneration = module.Generation
	condMgr.Sort()
	desired := module.Status.DeepCopy()
	if err := statusPkg.Update(ctx, r.Client, module, func(o *platformv1alpha1.TrustyAI) {
		o.Status = *desired
	}); err != nil {
		logger.Error(err, "Failed to update TrustyAI module status")
		return ctrl.Result{}, err
	}
	r.EventRecorder.Event(module, corev1.EventTypeNormal, EventReasonRemoved,
		"Module management state is Removed; reconciliation skipped")

	return ctrl.Result{}, nil
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

func (r *TrustyAIModuleReconciler) updateHealthStatus(ctx context.Context, module *platformv1alpha1.TrustyAI, condMgr *conditions.Manager) {
	logger := log.FromContext(ctx)

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

	prevPhase := module.Status.Phase

	if allHealthy {
		module.Status.Phase = common.PhaseReady
		condMgr.MarkTrue(string(common.ConditionTypeReady),
			conditions.WithReason("AllServicesHealthy"),
			conditions.WithMessage("All enabled services are healthy"),
			conditions.WithObservedGeneration(module.Generation),
		)
		condMgr.MarkTrue(string(common.ConditionTypeProvisioningSucceeded),
			conditions.WithReason("ProvisioningComplete"),
			conditions.WithMessage("Module provisioning completed successfully"),
			conditions.WithObservedGeneration(module.Generation),
		)
		condMgr.MarkFalse(string(common.ConditionTypeDegraded),
			conditions.WithReason("FullyFunctional"),
			conditions.WithMessage("All services are fully functional"),
			conditions.WithObservedGeneration(module.Generation),
		)
	} else {
		module.Status.Phase = common.PhaseNotReady
		condMgr.MarkFalse(string(common.ConditionTypeReady),
			conditions.WithReason("ServicesUnhealthy"),
			conditions.WithMessage("%s", strings.Join(unhealthyReasons, "; ")),
			conditions.WithObservedGeneration(module.Generation),
		)
		condMgr.MarkFalse(string(common.ConditionTypeProvisioningSucceeded),
			conditions.WithReason("ServicesUnhealthy"),
			conditions.WithMessage("One or more services are not healthy"),
			conditions.WithObservedGeneration(module.Generation),
		)

		if partiallyHealthy {
			condMgr.MarkTrue(string(common.ConditionTypeDegraded),
				conditions.WithReason("PartialFunctionality"),
				conditions.WithMessage("Some services are unavailable: %s", strings.Join(unhealthyReasons, "; ")),
				conditions.WithObservedGeneration(module.Generation),
			)
		} else {
			condMgr.MarkTrue(string(common.ConditionTypeDegraded),
				conditions.WithReason("AllServicesUnhealthy"),
				conditions.WithMessage("All services are unavailable: %s", strings.Join(unhealthyReasons, "; ")),
				conditions.WithObservedGeneration(module.Generation),
			)
		}
	}

	logger.Info("Updated health status", "phase", module.Status.Phase)

	if prevPhase != module.Status.Phase {
		if allHealthy {
			r.EventRecorder.Event(module, "Normal", "HealthCheckPassed", "All enabled services are healthy")
		} else if partiallyHealthy {
			r.EventRecorder.Event(module, "Warning", "HealthCheckPartial", "Some services are unhealthy")
		} else {
			r.EventRecorder.Event(module, "Warning", "HealthCheckFailed", "All services are unhealthy")
		}
	}
}

func (r *TrustyAIModuleReconciler) updateReleases(module *platformv1alpha1.TrustyAI) {
	module.Status.SetRelease(common.ComponentRelease{
		Name:    "trustyai-operator-module",
		Version: Version,
	})
}

// reconcileComponent renders the Kustomize overlay for the trustyai-service-operator
// and SSA-applies all resources into the cluster. On failure it marks
// ConditionTypeProvisioningSucceeded False and returns the error so the caller
// can persist status and requeue. On success it returns nil and lets
// updateHealthStatus own the condition.
//
// When r.Deployer is nil the method is a no-op (test mode or stub manifests).
func (r *TrustyAIModuleReconciler) reconcileComponent(
	ctx context.Context,
	module *platformv1alpha1.TrustyAI,
	condMgr *conditions.Manager,
) error {
	if r.Deployer == nil {
		return nil
	}

	objs, err := RenderManifests(ctx, r.ManifestsTemplatePath, r.Namespace)
	if err != nil {
		condMgr.MarkFalse(string(common.ConditionTypeProvisioningSucceeded),
			conditions.WithReason("RenderFailed"),
			conditions.WithMessage("Failed to render operator manifests: %v", err),
			conditions.WithObservedGeneration(module.Generation),
		)
		return err
	}

	if len(objs) == 0 {
		return nil
	}

	if err := r.Deployer.Deploy(ctx, deploy.DeployInput{
		Client:    r.Client,
		Owner:     module,
		Release:   deploy.ReleaseInfo{Version: Version},
		Resources: objs,
	}); err != nil {
		condMgr.MarkFalse(string(common.ConditionTypeProvisioningSucceeded),
			conditions.WithReason("DeployFailed"),
			conditions.WithMessage("Failed to deploy operator resources: %v", err),
			conditions.WithObservedGeneration(module.Generation),
		)
		return err
	}

	return nil
}

func (r *TrustyAIModuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.TrustyAI{}).
		Complete(r)
}

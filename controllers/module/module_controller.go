package module

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	modulev1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/module/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
)

const (
	finalizerName = "modules.platform.opendatahub.io/finalizer"

	ConditionReady                 = "Ready"
	ConditionProvisioningSucceeded = "ProvisioningSucceeded"
	ConditionDegraded              = "Degraded"

	PhaseReady    = "Ready"
	PhaseNotReady = "Not Ready"

	requeueInterval = 60 * time.Second
)

// ServiceHealthChecker is implemented by each sub-controller to report its
// health to the module reconciler for status aggregation.
type ServiceHealthChecker interface {
	Name() string
	IsHealthy(ctx context.Context) (bool, string)
}

// Reconciler reconciles the cluster-scoped TrustyAI module CR.
type Reconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Recorder       record.EventRecorder
	HealthCheckers []ServiceHealthChecker
}

//+kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=trustyais,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=trustyais/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=trustyais/finalizers,verbs=update

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	instance := &modulev1alpha1.TrustyAI{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		if errors.IsNotFound(err) {
			log.V(1).Info("TrustyAI module CR not found, ignoring")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion
	if instance.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, instance)
	}

	// Add finalizer if missing
	if !controllerutil.ContainsFinalizer(instance, finalizerName) {
		controllerutil.AddFinalizer(instance, finalizerName)
		if err := r.Update(ctx, instance); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Mark provisioning succeeded (operator is running and CRDs are installed)
	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:               ConditionProvisioningSucceeded,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: instance.Generation,
		Reason:             "OperatorRunning",
		Message:            "Operator started successfully, all CRDs installed",
	})

	// Aggregate health from sub-controllers
	r.aggregateHealth(ctx, instance)

	// Set releases
	instance.Status.Releases = []modulev1alpha1.ComponentRelease{
		{
			Name:    "trustyai-service-operator",
			Version: constants.Version,
			RepoURL: "https://github.com/trustyai-explainability/trustyai-service-operator",
		},
	}

	// Set observed generation
	instance.Status.ObservedGeneration = instance.Generation

	if err := r.Status().Update(ctx, instance); err != nil {
		log.Error(err, "Failed to update TrustyAI module status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: requeueInterval}, nil
}

// aggregateHealth checks all registered ServiceHealthCheckers and sets
// the Ready and Degraded conditions accordingly.
func (r *Reconciler) aggregateHealth(ctx context.Context, instance *modulev1alpha1.TrustyAI) {
	if len(r.HealthCheckers) == 0 {
		// No health checkers registered yet — report ready (operator is running).
		meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
			Type:               ConditionReady,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: instance.Generation,
			Reason:             "AllServicesHealthy",
			Message:            "Operator is running (no sub-controller health checkers registered)",
		})
		meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
			Type:               ConditionDegraded,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: instance.Generation,
			Reason:             "NoIssues",
		})
		instance.Status.Phase = PhaseReady
		return
	}

	var degraded []string
	for _, hc := range r.HealthCheckers {
		healthy, reason := hc.IsHealthy(ctx)
		if !healthy {
			degraded = append(degraded, hc.Name()+": "+reason)
		}
	}

	if len(degraded) == 0 {
		meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
			Type:               ConditionReady,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: instance.Generation,
			Reason:             "AllServicesHealthy",
			Message:            "All enabled services are operational",
		})
		meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
			Type:               ConditionDegraded,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: instance.Generation,
			Reason:             "NoIssues",
		})
		instance.Status.Phase = PhaseReady
	} else {
		msg := joinMessages(degraded)
		meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
			Type:               ConditionReady,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: instance.Generation,
			Reason:             "ServicesDegraded",
			Message:            msg,
		})
		meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
			Type:               ConditionDegraded,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: instance.Generation,
			Reason:             "ServicesDegraded",
			Message:            msg,
		})
		instance.Status.Phase = PhaseNotReady
	}
}

func (r *Reconciler) handleDeletion(ctx context.Context, instance *modulev1alpha1.TrustyAI) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	if controllerutil.ContainsFinalizer(instance, finalizerName) {
		log.Info("Running module CR finalizer")
		// Finalizer logic: nothing to clean up at the module level —
		// sub-controllers have their own finalizers on their own CRs.
		// The module CR does not own user workload CRs.

		controllerutil.RemoveFinalizer(instance, finalizerName)
		if err := r.Update(ctx, instance); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager registers the module reconciler. The module CR is
// cluster-scoped so no namespace filtering is needed.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("module-trustyai").
		For(&modulev1alpha1.TrustyAI{}).
		Complete(r)
}

func joinMessages(msgs []string) string {
	if len(msgs) == 1 {
		return msgs[0]
	}
	result := msgs[0]
	for _, m := range msgs[1:] {
		result += "; " + m
	}
	return result
}

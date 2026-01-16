package evalhub

import (
	"context"
	"fmt"
	"time"

	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func ControllerSetUp(mgr manager.Manager, ns string, recorder record.EventRecorder) error {
	return (&EvalHubReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		restMapper:    mgr.GetRESTMapper(),
		Namespace:     ns,
		EventRecorder: recorder,
	}).SetupWithManager(mgr)
}

// EvalHubReconciler reconciles an EvalHub object
type EvalHubReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	restMapper    meta.RESTMapper
	Namespace     string
	EventRecorder record.EventRecorder
}

//+kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=evalhubs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=evalhubs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=evalhubs/finalizers,verbs=update
//+kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=evalhubs/proxy,verbs=get;create;update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=list;watch;get;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch;create;update;delete
//+kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=list;watch;get;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch;update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *EvalHubReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the EvalHub instance
	instance := &evalhubv1alpha1.EvalHub{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			log.Info("EvalHub resource not found. Ignoring since object must be deleted")
			return DoNotRequeue()
		}
		// Error reading the object - requeue the request.
		log.Error(err, "Failed to get EvalHub")
		return RequeueWithError(err)
	}

	log.Info("Reconciling EvalHub", "name", instance.Name, "namespace", instance.Namespace)

	// Handle deletion first to avoid blocking removal with status init.
	if instance.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, instance)
	}

	// Set initial status if not set
	if instance.Status.Phase == "" {
		instance.Status.Phase = "Pending"
		instance.Status.Ready = corev1.ConditionFalse
		instance.SetStatus("Ready", "Initializing", "EvalHub is initializing", corev1.ConditionFalse)
		if err := r.Status().Update(ctx, instance); err != nil {
			log.Error(err, "Failed to update EvalHub status")
			return RequeueWithError(err)
		}
		return RequeueWithDelay(time.Second * 5)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(instance, evalhubv1alpha1.FinalizerName) {
		controllerutil.AddFinalizer(instance, evalhubv1alpha1.FinalizerName)
		if err := r.Update(ctx, instance); err != nil {
			log.Error(err, "Failed to add finalizer")
			return RequeueWithError(err)
		}
		return RequeueWithDelay(time.Second * 5)
	}

	// Create ServiceAccount for kube-rbac-proxy
	err = r.createServiceAccount(ctx, instance)
	if err != nil {
		log.Error(err, "Failed to create ServiceAccount")
		instance.SetStatus("Ready", "Error", fmt.Sprintf("Failed to create ServiceAccount: %v", err), corev1.ConditionFalse)
		r.Status().Update(ctx, instance)
		return RequeueWithError(err)
	}

	// Reconcile ConfigMap
	if err := r.reconcileConfigMap(ctx, instance); err != nil {
		log.Error(err, "Failed to reconcile ConfigMap")
		instance.SetStatus("Ready", "Error", fmt.Sprintf("Failed to reconcile ConfigMap: %v", err), corev1.ConditionFalse)
		r.Status().Update(ctx, instance)
		return RequeueWithError(err)
	}

	// Reconcile Proxy ConfigMap
	if err := r.reconcileProxyConfigMap(ctx, instance); err != nil {
		log.Error(err, "Failed to reconcile Proxy ConfigMap")
		instance.SetStatus("Ready", "Error", fmt.Sprintf("Failed to reconcile Proxy ConfigMap: %v", err), corev1.ConditionFalse)
		r.Status().Update(ctx, instance)
		return RequeueWithError(err)
	}

	// Reconcile Deployment
	if err := r.reconcileDeployment(ctx, instance); err != nil {
		log.Error(err, "Failed to reconcile Deployment")
		instance.SetStatus("Ready", "Error", fmt.Sprintf("Failed to reconcile Deployment: %v", err), corev1.ConditionFalse)
		r.Status().Update(ctx, instance)
		return RequeueWithError(err)
	}

	// Reconcile Service
	if err := r.reconcileService(ctx, instance); err != nil {
		log.Error(err, "Failed to reconcile Service")
		instance.SetStatus("Ready", "Error", fmt.Sprintf("Failed to reconcile Service: %v", err), corev1.ConditionFalse)
		r.Status().Update(ctx, instance)
		return RequeueWithError(err)
	}

	// Reconcile Route (if on OpenShift and enabled)
	if err := r.reconcileRoute(ctx, instance); err != nil {
		log.Error(err, "Failed to reconcile Route")
		// Log warning but don't update status - Route is optional (OpenShift only)
		// The Ready status will be determined by updateStatus based on deployment readiness
		// Route errors are not fatal, continue
	}

	// Check deployment status and update EvalHub status
	if err := r.updateStatus(ctx, instance); err != nil {
		log.Error(err, "Failed to update EvalHub status")
		return RequeueWithError(err)
	}

	// If everything is ready, requeue after a longer delay for periodic checks
	if instance.IsReady() {
		return RequeueWithDelay(time.Minute * 5)
	}

	return RequeueWithDelay(time.Second * 30)
}

// SetupWithManager sets up the controller with the Manager.
func (r *EvalHubReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&evalhubv1alpha1.EvalHub{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}

// Helper functions for reconcile results
func DoNotRequeue() (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

func RequeueWithError(err error) (ctrl.Result, error) {
	return ctrl.Result{}, err
}

func RequeueWithDelay(delay time.Duration) (ctrl.Result, error) {
	return ctrl.Result{RequeueAfter: delay}, nil
}

func Requeue() (ctrl.Result, error) {
	return ctrl.Result{Requeue: true}, nil
}

// handleDeletion handles the deletion of EvalHub resources
func (r *EvalHubReconciler) handleDeletion(ctx context.Context, instance *evalhubv1alpha1.EvalHub) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Handling EvalHub deletion", "name", instance.Name)

	// Clean up cluster-scoped resources that won't be garbage collected
	if err := r.cleanupClusterRoleBinding(ctx, instance); err != nil {
		log.Error(err, "Failed to cleanup ClusterRoleBinding")
		return RequeueWithError(err)
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(instance, evalhubv1alpha1.FinalizerName)
	if err := r.Update(ctx, instance); err != nil {
		log.Error(err, "Failed to remove finalizer")
		return RequeueWithError(err)
	}

	return DoNotRequeue()
}

// cleanupClusterRoleBinding deletes the EvalHub proxy ClusterRoleBinding upon instance deletion
func (r *EvalHubReconciler) cleanupClusterRoleBinding(ctx context.Context, instance *evalhubv1alpha1.EvalHub) error {
	log := log.FromContext(ctx)

	// Use the same naming pattern as createClusterRoleBinding
	crbName := instance.Name + "-" + instance.Namespace + "-proxy-rolebinding"

	crb := &rbacv1.ClusterRoleBinding{}
	log.Info("Deleting EvalHub proxy ClusterRoleBinding", "name", crbName)

	err := r.Get(ctx, types.NamespacedName{Name: crbName}, crb)
	if err == nil {
		// ClusterRoleBinding exists, delete it
		return r.Delete(ctx, crb)
	} else if errors.IsNotFound(err) {
		// ClusterRoleBinding doesn't exist, nothing to do
		log.Info("EvalHub proxy ClusterRoleBinding not found, may have been already deleted", "name", crbName)
		return nil
	} else {
		// Error getting ClusterRoleBinding
		return err
	}
}

// updateStatus updates the EvalHub status based on the deployment status
func (r *EvalHubReconciler) updateStatus(ctx context.Context, instance *evalhubv1alpha1.EvalHub) error {
	log := log.FromContext(ctx)

	// Get the deployment
	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, client.ObjectKey{
		Namespace: instance.Namespace,
		Name:      instance.Name,
	}, deployment)
	if err != nil {
		if errors.IsNotFound(err) {
			instance.Status.Phase = "Pending"
			instance.Status.Ready = corev1.ConditionFalse
			instance.SetStatus("Ready", "DeploymentNotFound", "Deployment not found", corev1.ConditionFalse)
			return r.Status().Update(ctx, instance)
		}
		return err
	}

	// Update replica counts
	instance.Status.Replicas = deployment.Status.Replicas
	instance.Status.ReadyReplicas = deployment.Status.ReadyReplicas

	// Determine readiness based on deployment status
	ready := deployment.Status.ReadyReplicas > 0 &&
		deployment.Status.ReadyReplicas == deployment.Status.Replicas

	if ready {
		instance.Status.Phase = "Ready"
		instance.Status.Ready = corev1.ConditionTrue
		instance.SetStatus("Ready", "DeploymentReady", "All replicas are ready", corev1.ConditionTrue)

		// Set URL based on service (kube-rbac-proxy)
		instance.Status.URL = fmt.Sprintf("https://%s.%s.svc.cluster.local:%d",
			instance.Name, instance.Namespace, kubeRBACProxyPort)
	} else {
		instance.Status.Phase = "Pending"
		instance.Status.Ready = corev1.ConditionFalse
		instance.SetStatus("Ready", "DeploymentNotReady",
			fmt.Sprintf("Waiting for deployment to be ready (%d/%d replicas ready)",
				deployment.Status.ReadyReplicas, deployment.Status.Replicas),
			corev1.ConditionFalse)
	}

	log.Info("Updating EvalHub status", "phase", instance.Status.Phase, "ready", instance.Status.Ready)
	return r.Status().Update(ctx, instance)
}

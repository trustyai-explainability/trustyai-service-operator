/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package gorch

import (
	"context"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/metrics"
	"strconv"
	"time"

	routev1 "github.com/openshift/api/route/v1"
	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// GuardrailsOrchestratorReconciler reconciles a GuardrailsOrchestrator object
type GuardrailsOrchestratorReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Namespace string
	Recorder  record.EventRecorder
}

// +kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=guardrailsorchestrators,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=guardrailsorchestrators/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=guardrailsorchestrators/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=list;watch;get;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apps,resources=deployments/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch;update
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=list;watch;get;create;update;patch;delete

// The registered function to set up GORCH controller
func ControllerSetUp(mgr manager.Manager, ns, configmap string, recorder record.EventRecorder) error {
	return (&GuardrailsOrchestratorReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Namespace: ns,
		Recorder:  recorder,
	}).SetupWithManager(mgr)
}

// createOrchestratorCreationMetrics collects and publishes metrics related to new-created Guardrails Orchestrators
func createOrchestratorCreationMetrics(orchestrator *gorchv1alpha1.GuardrailsOrchestrator) {
	// Update the Prometheus metrics for each task in the tasklist
	labels := make(map[string]string)
	labels["orchestrator_namespace"] = orchestrator.Namespace
	labels["using_built_in_detectors"] = strconv.FormatBool(orchestrator.Spec.EnableBuiltInDetectors)
	labels["using_sidecar_gateway"] = strconv.FormatBool(orchestrator.Spec.SidecarGatewayConfig != nil)

	// create/update metric counter
	counter := metrics.GetOrCreateGuardrailsOrchestratorCounter(labels)
	counter.Inc()
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the GuardrailsOrchestrator object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.1/pkg/reconcile
func (r *GuardrailsOrchestratorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	orchestrator := &gorchv1alpha1.GuardrailsOrchestrator{}

	err := r.Get(context.TODO(), req.NamespacedName, orchestrator)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("GuardrailsOrchestrator resource not found. Ignoring since object must be deleted.")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get GuardrailsOrchestrator")
		return ctrl.Result{}, err
	}

	// Start reconcilation
	if orchestrator.Status.Conditions == nil {
		reason := ReconcileInit
		message := "Initializing GuardrailsOrchestrator resource"
		orchestrator, err = r.updateStatus(ctx, orchestrator, func(saved *gorchv1alpha1.GuardrailsOrchestrator) {
			SetProgressingCondition(&saved.Status.Conditions, reason, message)
			saved.Status.Phase = PhaseProgressing
		})
		if err != nil {
			log.Error(err, "Failed to update GuardrailsOrchestrator status during initialization")
			return ctrl.Result{}, err
		}

		createOrchestratorCreationMetrics(orchestrator)
	}

	if !controllerutil.ContainsFinalizer(orchestrator, finalizerName) {
		log.Info("Adding Finalizer for the GuardrailsOrchestrator")
		if ok := controllerutil.AddFinalizer(orchestrator, finalizerName); !ok {
			log.Error(err, "Failed to add a finalizer into the custom resource")
			return ctrl.Result{Requeue: true}, nil
		}
		if err = r.Update(ctx, orchestrator); err != nil {
			log.Error(err, "Failed to update custom resource to add finalizer")
			return ctrl.Result{}, err
		}
	}

	// Check if the GuardrailsOrchestrator is marked to be deleted, which is
	// indicated by the deletion timestamp being set.
	isMarkedToBeDeleted := orchestrator.GetDeletionTimestamp() != nil
	if isMarkedToBeDeleted {
		if controllerutil.ContainsFinalizer(orchestrator, finalizerName) {
			if err = r.Get(ctx, req.NamespacedName, orchestrator); err != nil {
				log.Error(err, "Failed to re-fetch GuardrailsOrchestrator")
				return ctrl.Result{}, err
			}
			log.Info("Removing Finalizer for GuardrailsOrchestrator")
			if ok := controllerutil.RemoveFinalizer(orchestrator, finalizerName); !ok {
				log.Error(err, "Failed to remove finalizer from GuardrailsOrchestrator")
				return ctrl.Result{Requeue: true}, nil
			}
			if err = r.Update(ctx, orchestrator); err != nil {
				log.Error(err, "Failed to remove finalizer for GuardrailsOrchestrator")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	existingServiceAccount := &corev1.ServiceAccount{}
	err = r.Get(ctx, types.NamespacedName{Name: orchestrator.Name + "-serviceaccount", Namespace: orchestrator.Namespace}, existingServiceAccount)
	if err != nil && errors.IsNotFound(err) {
		serviceAccount := r.createServiceAccount(ctx, orchestrator)
		log.Info("Creating a new ServiceAccount", "ServiceAccount.Namespace", serviceAccount.Namespace, "ServiceAccount.Name", serviceAccount.Name)
		err = r.Create(ctx, serviceAccount)
		if err != nil {
			log.Error(err, "Failed to create new ServiceAccount", "ServiceAccount.Namespace", serviceAccount.Namespace, "ServiceAccount.Name", serviceAccount.Name)
			return ctrl.Result{}, err
		}
	} else if err != nil {
		log.Error(err, "Failed to get ServiceAccount")
		return ctrl.Result{}, err
	}

	existingConfigMap := &corev1.ConfigMap{}
	err = r.Get(ctx, types.NamespacedName{Name: *orchestrator.Spec.OrchestratorConfig, Namespace: orchestrator.Namespace}, existingConfigMap)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	existingDeployment := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: orchestrator.Name, Namespace: orchestrator.Namespace}, existingDeployment)
	if err != nil && errors.IsNotFound(err) {
		// Create a new deployment
		deployment := r.createDeployment(ctx, orchestrator)
		log.Info("Creating a new Deployment", "Deployment.Namespace", deployment.Namespace, "Deployment.Name", deployment.Name)
		err = r.Create(ctx, deployment)
		if err != nil {
			log.Error(err, "Failed to create new Deployment", "Deployment.Namespace", deployment.Namespace, "Deployment.Name", deployment.Name)
			return ctrl.Result{}, err
		}
	} else if err != nil {
		log.Error(err, "Failed to get Deployment")
		return ctrl.Result{}, err
	}

	existingService := &corev1.Service{}
	err = r.Get(ctx, types.NamespacedName{Name: orchestrator.Name + "-service", Namespace: orchestrator.Namespace}, existingService)
	if err != nil && errors.IsNotFound(err) {
		// Define a new service
		service := r.createService(ctx, orchestrator)
		log.Info("Creating a new Service", "Service.Namespace", service.Namespace, "Service.Name", service.Name)
		err = r.Create(ctx, service)
		if err != nil {
			log.Error(err, "Failed to create new Service", "Service.Namespace", service.Namespace, "Service.Name", service.Name)
			return ctrl.Result{}, err
		}
	} else if err != nil {
		log.Error(err, "Failed to get Service")
		return ctrl.Result{}, err
	}

	existingRoute := &routev1.Route{}
	err = r.Get(ctx, types.NamespacedName{Name: orchestrator.Name, Namespace: orchestrator.Namespace}, existingRoute)
	if err != nil && errors.IsNotFound(err) {
		// Define a new route
		httpRoute := r.createRoute(ctx, "https-route.tmpl.yaml", orchestrator)
		log.Info("Creating a new Route", "Route.Namespace", httpRoute.Namespace, "Route.Name", httpRoute.Name)
		err = r.Create(ctx, httpRoute)
		if err != nil {
			log.Error(err, "Failed to create new Route", "Route.Namespace", httpRoute.Namespace, "Route.Name", httpRoute.Name)
		}
	} else if err != nil {
		log.Error(err, "Failed to get Route")
		return ctrl.Result{}, err
	}

	err = r.Get(ctx, types.NamespacedName{Name: orchestrator.Name + "-health", Namespace: orchestrator.Namespace}, existingRoute)
	if err != nil && errors.IsNotFound(err) {
		// Define a new route
		healthRoute := r.createRoute(ctx, "health-route.tmpl.yaml", orchestrator)
		log.Info("Creating a new Route", "Route.Namespace", healthRoute.Namespace, "Route.Name", healthRoute.Name)
		err = r.Create(ctx, healthRoute)
		if err != nil {
			log.Error(err, "Failed to create new Route", "Route.Namespace", healthRoute.Namespace, "Route.Name", healthRoute.Name)
		}
	} else if err != nil {
		log.Error(err, "Failed to get Route")
		return ctrl.Result{}, err
	}

	// Finalize reconcilation
	_, updateErr := r.reconcileStatuses(ctx, orchestrator)
	if updateErr != nil {
		return ctrl.Result{}, updateErr
	}
	return ctrl.Result{Requeue: true, RequeueAfter: 30 * time.Second}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GuardrailsOrchestratorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gorchv1alpha1.GuardrailsOrchestrator{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

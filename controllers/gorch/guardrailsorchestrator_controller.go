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
	"fmt"

	routev1 "github.com/openshift/api/route/v1"
	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// +kubebuilder:rbac:groups=gorch.opendatahub.io,resources=guardrailsorchestrators,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gorch.opendatahub.io,resources=guardrailsorchestrators/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gorch.opendatahub.io,resources=guardrailsorchestrators/finalizers,verbs=update
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
		message := "Intializing GuardrailsOrchestrator resource"
		orchestrator, err = r.updateStatus(orchestrator, func(saved *gorchv1alpha1.GuardrailsOrchestrator) {
			SetProgressingCondition(&saved.Status.Conditions, reason, message)
			saved.Status.Phase = PhaseProgressing
		})
		if err != nil {
			log.Error(err, "Failed to update GuardrailsOrchestrator status during initialization")
			return ctrl.Result{}, err
		}
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
			log.Info("Performing Finalizer Operations for GuardrailsOrchestrator before delete CR")

			if err = r.doFinalizerOperationsForOrchestrator(ctx, orchestrator); err != nil {
				log.Error(err, "Failed to do finalizer operations for GuardrailsOrchestrator")
				return ctrl.Result{}, err
			}

			if err = r.Get(ctx, req.NamespacedName, orchestrator); err != nil {
				log.Error(err, "Failed to re-fetch GuardrailsOrchestrator")
				return ctrl.Result{}, err
			}
			log.Info("Removing Finalizer for GuardrailsOrchestrator after sucessfully performing the operations")
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

	existingConfigMap := &corev1.ConfigMap{}
	err = r.Get(ctx, types.NamespacedName{Name: orchestrator.Name + "-config", Namespace: orchestrator.Namespace}, existingConfigMap)
	if err != nil && errors.IsNotFound(err) {
		configMap := r.createConfigMap(ctx, orchestrator)
		log.Info("Creating a new ConfigMap", "ConfigMap.Namespace", configMap.Namespace, "ConfigMap.Name", configMap.Name)
		err = r.Create(ctx, configMap)
		if err != nil {
			log.Error(err, "Failed to create new ConfigMap", "ConfigMap.Namespace", configMap.Namespace, "ConfigMap.Name", configMap.Name)
			return ctrl.Result{}, err
		}
	} else if err != nil {
		log.Error(err, "Failed to get ConfigMap")
		return ctrl.Result{}, err
	}
	// TODO: add create constant
	orchestrator, err = r.updateStatus(orchestrator, func(saved *gorchv1alpha1.GuardrailsOrchestrator) {
		SetResourceCondition(&saved.Status.Conditions, "ConfigMap", "ConfigMapCreated", "ConfigMap created succesfully", corev1.ConditionTrue)
	})
	if err != nil {
		log.Error(err, "Failed to update GuardrailsOrchestrator status after ConfigMap creation")
		return ctrl.Result{}, err
	}

	existingServiceAccount := &corev1.ServiceAccount{}
	err = r.Get(ctx, types.NamespacedName{Name: orchestrator.Name, Namespace: orchestrator.Namespace}, existingServiceAccount)
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
	orchestrator, err = r.updateStatus(orchestrator, func(saved *gorchv1alpha1.GuardrailsOrchestrator) {
		SetResourceCondition(&saved.Status.Conditions, "ServiceAccount", "ServiceAccountCreated", "ServiceAccount created succesfully", corev1.ConditionTrue)
	})
	if err != nil {
		log.Error(err, "Failed to update GuardrailsOrchestrator status after Service Account creation")
		return ctrl.Result{}, err
	}

	existingDeployment := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: orchestratorName, Namespace: orchestrator.Namespace}, existingDeployment)
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
	orchestrator, err = r.updateStatus(orchestrator, func(saved *gorchv1alpha1.GuardrailsOrchestrator) {
		SetResourceCondition(&saved.Status.Conditions, "Deployment", "DeploymentCreated", "Deployment created succesfully", corev1.ConditionTrue)
	})
	if err != nil {
		log.Error(err, "Failed to update GuardrailsOrchestrator status after Deployment creation")
		return ctrl.Result{}, err
	}

	existingService := &corev1.Service{}
	err = r.Get(ctx, types.NamespacedName{Name: orchestrator.Name, Namespace: orchestrator.Namespace}, existingService)
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
	orchestrator, err = r.updateStatus(orchestrator, func(saved *gorchv1alpha1.GuardrailsOrchestrator) {
		SetResourceCondition(&saved.Status.Conditions, "Service", "ServiceCreated", "Service created succesfully", corev1.ConditionTrue)
	})
	if err != nil {
		log.Error(err, "Failed to update GuardrailsOrchestrator status after Service creation")
		return ctrl.Result{}, err
	}

	existingRoute := &routev1.Route{}
	err = r.Get(ctx, types.NamespacedName{Name: orchestrator.Name, Namespace: orchestrator.Namespace}, existingRoute)
	if err != nil && errors.IsNotFound(err) {
		// Define a new route
		route := r.createRoute(ctx, orchestrator)
		log.Info("Creating a new Route", "Route.Namespace", route.Namespace, "Route.Name", route.Name)
		err = r.Create(ctx, route)
		if err != nil {
			log.Error(err, "Failed to create new Route", "Route.Namespace", route.Namespace, "Route.Name", route.Name)
		}
	} else if err != nil {
		log.Error(err, "Failed to get Route")
		return ctrl.Result{}, err
	}

	orchestrator, err = r.updateStatus(orchestrator, func(saved *gorchv1alpha1.GuardrailsOrchestrator) {
		SetResourceCondition(&saved.Status.Conditions, "Route", "RouteCreated", "Route created succesfully", corev1.ConditionTrue)

	})
	if err != nil {
		log.Error(err, "Failed to update GuardrailsOrchestrator status after Route creation")
		return ctrl.Result{}, err
	}

	// finalize reconcilation
	orchestrator, err = r.updateStatus(orchestrator, func(saved *gorchv1alpha1.GuardrailsOrchestrator) {
		SetCompleteCondition(&saved.Status.Conditions, ReconcileCompleted, ReconcileCompletedMessage)
		saved.Status.Phase = PhaseReady
	})
	if err != nil {
		log.Error(err, "failed to update GuardrailsOrchestrator conditions after successfuly completed reconciliation")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *GuardrailsOrchestratorReconciler) deleteRoute(ctx context.Context, orchestrator gorchv1alpha1.GuardrailsOrchestrator) (err error) {
	route := routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      orchestrator.Name + "-route",
			Namespace: orchestrator.Namespace,
		},
	}
	return r.Delete(ctx, &route, &client.DeleteOptions{})
}

func (r *GuardrailsOrchestratorReconciler) doFinalizerOperationsForOrchestrator(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) (err error) {
	// delete orchestrator route
	if err = r.deleteRoute(ctx, *orchestrator); err != nil {
		return err
	}
	log := log.FromContext(ctx)
	log.Info("Successfully deleted route for orchestrator", "orchestrator", orchestrator.Name)
	r.Recorder.Event(orchestrator, "Warning", "Deleting",
		fmt.Sprintf("Custom resource %s is being deleted from the namespace %s",
			orchestrator.Name,
			orchestrator.Namespace))
	return
}

// SetupWithManager sets up the controller with the Manager.
func (r *GuardrailsOrchestratorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gorchv1alpha1.GuardrailsOrchestrator{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

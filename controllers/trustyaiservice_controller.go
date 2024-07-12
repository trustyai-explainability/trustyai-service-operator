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

package controllers

import (
	"context"
	goerrors "errors"
	"time"

	kservev1alpha1 "github.com/kserve/kserve/pkg/apis/serving/v1alpha1"
	kservev1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var ErrPVCNotReady = goerrors.New("PVC is not ready")

// TrustyAIServiceReconciler reconciles a TrustyAIService object
type TrustyAIServiceReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	Namespace     string
	EventRecorder record.EventRecorder
}

//+kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=trustyaiservices,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=trustyaiservices/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=trustyaiservices/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=list;watch;get;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apps,resources=deployments/finalizers,verbs=update
//+kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=list;watch;create
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=persistentvolumes,verbs=list;get;watch
//+kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=list;get;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=serving.kserve.io,resources=servingruntimes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=serving.kserve.io,resources=servingruntimes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=list;watch;get;create;update;patch;delete
//+kubebuilder:rbac:groups=serving.kserve.io,resources=inferenceservices,verbs=list;watch;get;update;patch
//+kubebuilder:rbac:groups=serving.kserve.io,resources=inferenceservices/finalizers,verbs=list;watch;get;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch;update
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch;create;update;delete
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;create;update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *TrustyAIServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	// Fetch the AppService instance
	instance := &trustyaiopendatahubiov1alpha1.TrustyAIService{}
	err := r.Get(context.TODO(), req.NamespacedName, instance)
	if err != nil {
		// Handle error
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			return DoNotRequeue()
		}
		// Error reading the object - requeue the request.
		return RequeueWithError(err)
	}

	// Check if the CR is being deleted
	if instance.DeletionTimestamp != nil {
		// CR is being deleted
		if containsString(instance.Finalizers, finalizerName) {
			// The finalizer is present, so we handle external dependency deletion
			if err := r.deleteExternalDependency(req.Name, instance, req.Namespace, ctx); err != nil {
				// Log the error instead of returning it, so we proceed to remove the finalizer without blocking
				log.FromContext(ctx).Error(err, "Failed to delete external dependencies, but proceeding with finalizer removal.")
			}

			// Remove the finalizer from the list and update it.
			instance.Finalizers = removeString(instance.Finalizers, finalizerName)
			if err := r.Update(ctx, instance); err != nil {
				return RequeueWithErrorMessage(ctx, err, "Failed to remove the finalizer.")
			}
		}
		return DoNotRequeue()
	}

	// Add the finalizer if it does not exist
	if !containsString(instance.Finalizers, finalizerName) {
		instance.Finalizers = append(instance.Finalizers, finalizerName)
		if err := r.Update(ctx, instance); err != nil {
			return RequeueWithErrorMessage(ctx, err, "Failed to add the finalizer.")
		}
	}

	err = r.createServiceAccount(ctx, instance)
	if err != nil {
		return RequeueWithError(err)
	}

	caBundle := r.GetCustomCertificatesBundle(ctx, instance)

	err = r.reconcileOAuthService(ctx, instance, caBundle)
	if err != nil {
		return RequeueWithError(err)
	}

	// CR found, add or update the URL
	// Call the function to patch environment variables for Deployments that match the label
	shouldContinue, err := r.handleInferenceServices(ctx, instance, req.Namespace, modelMeshLabelKey, modelMeshLabelValue, payloadProcessorName, req.Name, false)
	if err != nil {
		return RequeueWithErrorMessage(ctx, err, "Could not patch environment variables for Deployments.")
	}
	if !shouldContinue {
		return RequeueWithDelayMessage(ctx, time.Minute, "Not all replicas are ready, requeue the reconcile request")
	}

	if instance.Spec.Storage.IsStoragePVC() || instance.IsMigration() {
		// Ensure PVC
		err = r.ensurePVC(ctx, instance)
		if err != nil {
			// PVC not found condition
			log.FromContext(ctx).Error(err, "Error creating PVC storage.")
			_, updateErr := r.updateStatus(ctx, instance, UpdatePVCNotAvailable)
			if updateErr != nil {
				return RequeueWithErrorMessage(ctx, err, "Failed to update status")
			}

			// If there was an error finding the PV, requeue the request
			return RequeueWithErrorMessage(ctx, err, "Could not find requested PersistentVolumeClaim.")

		}
	}
	if instance.Spec.Storage.IsStorageDatabase() {
		// Get database configuration
		secret, err := r.findDatabaseSecret(ctx, instance)
		if err != nil {
			return RequeueWithErrorMessage(ctx, err, "Service configured to use database storage but no database configuration found.")
		}
		err = r.validateDatabaseSecret(secret)
		if err != nil {
			return RequeueWithErrorMessage(ctx, err, "Database configuration contains errors.")
		}
	}

	// Check for migration annotation
	if _, ok := instance.Annotations[migrationAnnotationKey]; ok {
		log.FromContext(ctx).Info("Found migration annotation. Migrating.")
		err = r.ensureDeployment(ctx, instance, caBundle, true)
		//err = r.redeployForMigration(ctx, instance)

		if err != nil {
			return RequeueWithErrorMessage(ctx, err, "Retrying to restart deployment during migration.")
		}

		// Remove the migration annotation after processing to avoid restarts
		delete(instance.Annotations, migrationAnnotationKey)
		log.FromContext(ctx).Info("Deleting annotation")
		if err := r.Update(ctx, instance); err != nil {
			return RequeueWithErrorMessage(ctx, err, "Failed to remove migration annotation.")
		}
	} else {
		// Ensure Deployment object
		err = r.ensureDeployment(ctx, instance, caBundle, false)
		log.FromContext(ctx).Info("No annotation found")
		if err != nil {
			return RequeueWithError(err)
		}
	}

	// Fetch the TrustyAIService instance
	trustyAIServiceService := &trustyaiopendatahubiov1alpha1.TrustyAIService{}
	err = r.Get(ctx, req.NamespacedName, trustyAIServiceService)
	if err != nil {
		return RequeueWithErrorMessage(ctx, err, "Could not fetch service.")
	}

	// Create service
	service, err := r.reconcileService(ctx, trustyAIServiceService)
	if err != nil {
		// handle error
		return RequeueWithError(err)
	}
	if err := r.Create(ctx, service); err != nil {
		if errors.IsAlreadyExists(err) {
			// Service already exists, no problem
		} else {
			// handle any other error
			return RequeueWithError(err)
		}
	}

	// Local Service Monitor
	err = r.ensureLocalServiceMonitor(instance, ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Central Service Monitor
	err = r.ensureCentralServiceMonitor(ctx)
	if err != nil {
		return RequeueWithError(err)
	}

	// Create route
	// TODO: Change argument order
	err = r.ReconcileRoute(instance, ctx)
	//err = r.reconcileRoute(instance, ctx)
	if err != nil {
		// Could not create Route object, update status and return.
		_, updateErr := r.updateStatus(ctx, instance, UpdateRouteNotAvailable)
		if updateErr != nil {
			return RequeueWithErrorMessage(ctx, err, "Failed to update status")
		}
		return RequeueWithErrorMessage(ctx, err, "Failed to get or create Route")
	}

	// Reconcile statuses
	result, err := r.reconcileStatuses(ctx, instance)
	if err != nil {
		return result, err
	}

	// Requeue the request with a delay
	return RequeueWithDelay(defaultRequeueDelay)
}

// SetupWithManager sets up the controller with the Manager.
func (r *TrustyAIServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Watch ServingRuntime objects (not managed by this controller)
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &appsv1.Deployment{}, ".metadata.controller", func(rawObj client.Object) []string {
		// Grab the deployment object and extract the owner
		deployment := rawObj.(*appsv1.Deployment)
		owner := metav1.GetControllerOf(deployment)
		if owner == nil {
			return nil
		}
		// Retain ServingRuntimes only
		if owner.APIVersion != kservev1beta1.APIVersion || owner.Kind != "ServingRuntime" {
			return nil
		}
		return []string{owner.Name}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&trustyaiopendatahubiov1alpha1.TrustyAIService{}).
		Owns(&appsv1.Deployment{}).
		Watches(&source.Kind{Type: &kservev1beta1.InferenceService{}}, &handler.EnqueueRequestForObject{}).
		Watches(&source.Kind{Type: &kservev1alpha1.ServingRuntime{}}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}

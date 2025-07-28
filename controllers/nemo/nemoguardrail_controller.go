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

package nemo

import (
	"context"
	nemov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo/v1alpha1"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/nemo/templates"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// NemoGuardrailReconciler reconciles a NemoGuardrails object
type NemoGuardrailsReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Namespace string
	Recorder  record.EventRecorder
}

const (
	serviceTemplate = "service.tmpl.yaml"
	routeTemplate   = "route.tmpl.yaml"
	routePort       = "oauth-proxy"
)

//+kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=nemoguardrails,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=nemoguardrails/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=nemoguardrails/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the NemoGuardrails object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.3/pkg/reconcile
func (r *NemoGuardrailsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// fetch instance of NemoGuardrails CR
	nemoGuardrails := &nemov1alpha1.NemoGuardrails{}
	err := r.Get(context.TODO(), req.NamespacedName, nemoGuardrails)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("NemoGuardrails resource not found. Ignoring since object must be deleted.")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get NemoGuardrails.")
		return ctrl.Result{}, err
	}

	existingDeployment := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: nemoGuardrails.Name, Namespace: nemoGuardrails.Namespace}, existingDeployment)
	if err != nil && errors.IsNotFound(err) {
		// Create a new deployment
		deployment, err := r.createDeployment(ctx, nemoGuardrails)
		if err != nil {
			return ctrl.Result{}, err
		}
		logger.Info("Creating a new Deployment", "Deployment.Namespace", deployment.Namespace, "Deployment.Name", deployment.Name)
		err = r.Create(ctx, deployment)
		if err != nil {
			logger.Error(err, "Failed to create new Deployment", "Deployment.Namespace", deployment.Namespace, "Deployment.Name", deployment.Name)
			return ctrl.Result{}, err
		}
	} else if err != nil {
		logger.Error(err, "Failed to get Deployment")
		return ctrl.Result{}, err
	}

	_, err = utils.ReconcileService(ctx, r.Client, nemoGuardrails, serviceTemplate, templateParser.ParseResource)
	if err != nil {
		logger.Error(err, "Failed to reconcile service")
		return ctrl.Result{}, err
	}

	_, err = utils.ReconcileDefaultRoute(ctx, r.Client, nemoGuardrails, routeTemplate, templateParser.ParseResource)
	if err != nil {
		logger.Error(err, "Failed to reconcile service")
		return ctrl.Result{}, err
	}

	// Finalize reconcilation
	_, updateErr := r.reconcileStatuses(ctx, nemoGuardrails)
	if updateErr != nil {
		return ctrl.Result{}, updateErr
	}
	return ctrl.Result{Requeue: true, RequeueAfter: 30 * time.Second}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NemoGuardrailsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&nemov1alpha1.NemoGuardrails{}).
		Complete(r)
}

// The registered function to set up NEMO-GUARDRAILS controller
func ControllerSetUp(mgr manager.Manager, ns, configmap string, recorder record.EventRecorder) error {
	return (&NemoGuardrailsReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Namespace: ns,
		Recorder:  recorder,
	}).SetupWithManager(mgr)
}

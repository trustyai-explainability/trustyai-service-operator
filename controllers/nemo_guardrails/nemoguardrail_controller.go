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

package nemo_guardrails

import (
	"context"
	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/nemo_guardrails/templates"
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
	serviceTemplate  = "service.tmpl.yaml"
	caBundleTemplate = "ca-bundle-configmap.tmpl.yaml"
	routeTemplate    = "route.tmpl.yaml"
)

// +kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=nemoguardrails,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=nemoguardrails/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=nemoguardrails/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments/finalizers,verbs=update

func (r *NemoGuardrailsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// ====== Fetch instance of NemoGuardrails CR ======================================================================
	nemoGuardrails := &nemoguardrailsv1alpha1.NemoGuardrails{}
	err := r.Get(ctx, req.NamespacedName, nemoGuardrails)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("NemoGuardrails resource not found. Ignoring since object must be deleted.")
			return ctrl.Result{}, nil
		}

		utils.LogErrorRetrieving(ctx, err, "NemoGuardrails Custom Resource", nemoGuardrails.Name, nemoGuardrails.Namespace)
		return ctrl.Result{}, err
	}

	// ====== Deletion handling ========================================================================================
	if err := utils.AddFinalizerIfNeeded(ctx, r.Client, nemoGuardrails, finalizerName); err != nil {
		return ctrl.Result{}, err
	}

	// define all the cleanup steps needed before the finalizer can be removed in the CleanupFunc
	cleanupFunc := func() error {
		return utils.CleanupClusterRoleBinding(ctx, r.Client, nemoGuardrails)
	}
	shouldExit, err := utils.HandleDeletionIfNeeded(ctx, r.Client, nemoGuardrails, finalizerName, cleanupFunc)
	if err != nil {
		return ctrl.Result{}, err
	}
	if shouldExit {
		return ctrl.Result{}, nil
	}

	// ====== Deploy the CA bundle configmap ============================================================================
	caBundleConfigMapName := nemoGuardrails.Name + "-ca-bundle"
	_, _, err = utils.ReconcileConfigMap(ctx, r.Client, nemoGuardrails, caBundleConfigMapName, constants.Version, caBundleTemplate, templateParser.ParseResource)
	if err != nil {
		utils.LogErrorReconciling(ctx, err, "configmap", caBundleConfigMapName, nemoGuardrails.Namespace)
		return ctrl.Result{}, err
	}

	// ====== Load available CA configs and create a unified CA bundle ===================================================
	caBundleInitContainerConfig, configMapsToMount, caStatus := r.LoadCAConfigs(ctx, logger, *nemoGuardrails)
	nemoGuardrails.Status.CA = caStatus
	if err := r.updateCAStatusWithRetry(ctx, req.NamespacedName, caStatus); err != nil {
		utils.LogErrorUpdating(ctx, err, "CA status", nemoGuardrails.Name, nemoGuardrails.Namespace)
		return ctrl.Result{}, err
	}

	// ====== Deploy kube-rbac-proxy configmap if needed ================================================================
	if utils.RequiresAuth(nemoGuardrails) {
		_, _, err := utils.ReconcileConfigMap(ctx, r.Client, nemoGuardrails, GetRBACConfigName(*nemoGuardrails), constants.Version, "kube-rbac-proxy-config.tmpl.yaml", templateParser.ParseResource)
		if err != nil {
			return ctrl.Result{}, err
		}

		// create auth serviceaccount
		if err := utils.ReconcileServiceAccount(ctx, r.Client, nemoGuardrails); err != nil {
			return ctrl.Result{}, err
		}

		// create auth CRB
		if err := utils.ReconcileAuthDelegatorClusterRoleBinding(ctx, r.Client, nemoGuardrails); err != nil {
			return ctrl.Result{}, err
		}
	}

	// ====== Create deployment ========================================================================================
	existingDeployment := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: nemoGuardrails.Name, Namespace: nemoGuardrails.Namespace}, existingDeployment)
	if err != nil && errors.IsNotFound(err) {
		// Create a new deployment
		deployment, err := r.createDeployment(ctx, nemoGuardrails, caBundleInitContainerConfig, configMapsToMount)
		if err != nil {
			return ctrl.Result{}, err
		}

		utils.LogInfoCreating(ctx, "deployment", nemoGuardrails.Name, nemoGuardrails.Namespace)
		err = r.Create(ctx, deployment)
		if err != nil {
			utils.LogErrorCreating(ctx, err, "deployment", deployment.Name, deployment.Namespace)
			return ctrl.Result{}, err
		}
	} else if err != nil {
		utils.LogErrorRetrieving(ctx, err, "deployment", nemoGuardrails.Name, nemoGuardrails.Namespace)
		return ctrl.Result{}, err
	}

	// ====== Reconcile Service ===========================================================================================
	serviceConfig := utils.ServiceConfig{
		Name:         nemoGuardrails.Name,
		Namespace:    nemoGuardrails.Namespace,
		Owner:        nemoGuardrails,
		Version:      constants.Version,
		UseAuthProxy: utils.RequiresAuth(nemoGuardrails),
	}
	err = utils.ReconcileService(ctx, r.Client, nemoGuardrails, serviceConfig, serviceTemplate, templateParser.ParseResource)
	if err != nil {
		utils.LogErrorReconciling(ctx, err, "service", serviceConfig.Name, serviceConfig.Namespace)
		return ctrl.Result{}, err
	}

	// ====== Reconcile Route ==========================================================================================
	termination := utils.Edge
	if utils.RequiresAuth(nemoGuardrails) {
		termination = utils.Reencrypt
	}
	routeConfig := utils.RouteConfig{
		PortName:    nemoGuardrails.Name, // only one available port in the service, so don't need to specify any port name
		ServiceName: nemoGuardrails.Name,
		Termination: utils.StringPointer(termination),
	}
	err = utils.ReconcileRoute(ctx, r.Client, nemoGuardrails, routeConfig, routeTemplate, templateParser.ParseResource)
	if err != nil {
		utils.LogErrorReconciling(ctx, err, "route", nemoGuardrails.Name, nemoGuardrails.Namespace)
		return ctrl.Result{}, err
	}

	// ====== Finalize reconciliation ==================================================================================
	_, updateErr := r.reconcileStatuses(ctx, nemoGuardrails)
	if updateErr != nil {
		return ctrl.Result{}, updateErr
	}
	log.FromContext(ctx).Info("RECONCILE DONE")
	return ctrl.Result{Requeue: true, RequeueAfter: 30 * time.Second}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NemoGuardrailsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&nemoguardrailsv1alpha1.NemoGuardrails{}).
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

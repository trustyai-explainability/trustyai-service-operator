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
	"github.com/go-logr/logr"
	kservev1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/gorch/templates"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/metrics"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"strconv"
	"time"

	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
// +kubebuilder:rbac:groups=serving.kserve.io,resources=servingruntimes,verbs=get;list;watch
// +kubebuilder:rbac:groups=serving.kserve.io,resources=inferenceservices,verbs=get;list
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

func (r *GuardrailsOrchestratorReconciler) refreshOrchestrator(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator, log logr.Logger) (*gorchv1alpha1.GuardrailsOrchestrator, error) {
	latestOrchestrator := &gorchv1alpha1.GuardrailsOrchestrator{}
	if err := r.Get(ctx, types.NamespacedName{Name: orchestrator.Name, Namespace: orchestrator.Namespace}, latestOrchestrator); err != nil {
		log.Error(err, "Failed to re-fetch Orchestrator before updating status")
		return nil, err
	}
	return latestOrchestrator, nil
}

func (r *GuardrailsOrchestratorReconciler) handleReconciliationError(ctx context.Context, log logr.Logger, orchestrator *gorchv1alpha1.GuardrailsOrchestrator, err error, reason string, message string) {
	r.handleReconciliationErrorWithTrace(ctx, log, orchestrator, err, reason, message)
}

func (r *GuardrailsOrchestratorReconciler) handleReconciliationErrorWithTrace(ctx context.Context, log logr.Logger, orchestrator *gorchv1alpha1.GuardrailsOrchestrator, err error, reason string, message string, keysAndValues ...any) {
	log.Info("Marking " + orchestrator.Name + " as failed. Reconciliation will not be reattempted.")
	orchestrator, err = r.updateStatus(ctx, orchestrator, func(saved *gorchv1alpha1.GuardrailsOrchestrator) {
		utils.UnsetProgressingCondition(&saved.Status.Conditions, reason, "")
		utils.SetFailedCondition(&saved.Status.Conditions, reason, message)
		saved.Status.Phase = utils.PhaseError
	})
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
		r.handleReconciliationError(ctx, log, orchestrator, err, utils.ReconcileFailed, "Failed to get GuardrailsOrchestrator")
		return ctrl.Result{}, err
	}

	// Start reconcilation
	if orchestrator.Status.Conditions == nil {
		reason := utils.ReconcileInit
		message := "Initializing GuardrailsOrchestrator resource"
		orchestrator, err = r.updateStatus(ctx, orchestrator, func(saved *gorchv1alpha1.GuardrailsOrchestrator) {
			utils.SetProgressingCondition(&saved.Status.Conditions, reason, message)
			saved.Status.Phase = utils.PhaseProgressing
		})
		if err != nil {
			r.handleReconciliationError(ctx, log, orchestrator, err, utils.ReconcileFailed, "Failed to update GuardrailsOrchestrator status during initialization")
			return ctrl.Result{}, err
		}
		createOrchestratorCreationMetrics(orchestrator)
	}

	// === DELETION HANDLING =======================================================================================================
	if err := utils.AddFinalizerIfNeeded(ctx, r.Client, orchestrator, finalizerName); err != nil {
		return ctrl.Result{}, err
	}

	// define all the cleanup steps needed before the finalizer can be removed in the CleanupFunc
	cleanupFunc := func() error {
		return utils.CleanupClusterRoleBinding(ctx, r.Client, orchestrator)
	}
	shouldExit, err := utils.HandleDeletionIfNeeded(ctx, r.Client, orchestrator, finalizerName, cleanupFunc)
	if err != nil {
		return ctrl.Result{}, err
	}
	if shouldExit {
		return ctrl.Result{}, nil
	}

	if orchestrator.Status.Conditions != nil {
		// skip reconciliation of failed GuardrailsOrchestrator
		for _, cond := range orchestrator.Status.Conditions {
			if cond.Type == utils.ReconcileFailed && cond.Status == corev1.ConditionTrue {
				return ctrl.Result{}, nil
			}
		}
	}

	// == VERIFY DEPLOYMENT CONDITIONS =============================================================================================================
	if orchestrator.Spec.DisableOrchestrator && !orchestrator.Spec.EnableBuiltInDetectors {
		err = fmt.Errorf("guardrails orchestrator spec is invalid: if the orchestrator is disabled, the built-in detector must be enabled")
		return ctrl.Result{}, err
	}

	// === SERVICE ACCOUNT  ========================================================================================================================
	err = utils.ReconcileServiceAccount(ctx, r.Client, orchestrator)
	if err != nil {
		r.handleReconciliationError(ctx, log, orchestrator, err, utils.ReconcileFailed, "Failed to get reconcile serviceAccount")
		return ctrl.Result{}, err
	}

	if err = utils.ReconcileAuthDelegatorClusterRoleBinding(ctx, r.Client, orchestrator); err != nil {
		r.handleReconciliationError(ctx, log, orchestrator, err, utils.ReconcileFailed, "Failed to reconcile ClusterRoleBinding")
		return ctrl.Result{}, err
	}

	// === AUTO CONFIG ========================================================================================================================
	var tlsMounts []gorchv1alpha1.DetectedService
	if orchestrator.Spec.AutoConfig != nil {
		// Only perform autoconfig logic if the relevant resources have changed
		shouldRegen, err := r.shouldRegenerateAutoConfig(ctx, orchestrator)
		if err != nil {
			r.handleReconciliationError(ctx, log, orchestrator, err, AutoConfigFailed, "Failed to check if autoconfig should be regenerated")
			return ctrl.Result{}, err
		}
		if shouldRegen {
			tlsMounts, err = r.runAutoConfig(ctx, orchestrator)
			if err != nil {
				r.handleReconciliationError(ctx, log, orchestrator, err, AutoConfigFailed, "Failed to perform AutoConfig")
				return ctrl.Result{}, err
			}
			orchestrator, _ = r.refreshOrchestrator(ctx, orchestrator, log)
		} else {
			tlsMounts = getTLSInfo(*orchestrator)
		}
	} else if orchestrator.Spec.OrchestratorConfig != nil {
		existingConfigMap := &corev1.ConfigMap{}
		err = r.Get(ctx, types.NamespacedName{Name: *orchestrator.Spec.OrchestratorConfig, Namespace: orchestrator.Namespace}, existingConfigMap)
		if err != nil {
			if client.IgnoreNotFound(err) != nil {
				r.handleReconciliationErrorWithTrace(ctx, log, orchestrator, err, utils.ReconcileFailed, "Failed to get existing ConfigMap", "ConfigMap.Name", *orchestrator.Spec.OrchestratorConfig, "ConfigMap.Namespace", orchestrator.Namespace)
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
	}

	if orchestrator.Spec.AutoConfig != nil && (getOrchestratorConfigMap(orchestrator) == nil || (orchestrator.Spec.EnableGuardrailsGateway && getGatewayConfigMap(orchestrator) == nil)) {
		log.Info("Waiting for orchestrator status to register AutoConfig information before starting deployment")
		return ctrl.Result{}, nil
	}

	// === RBAC CONFIGMAPS ========================================================================================================================
	// Ensure kube-rbac-proxy ConfigMaps exist if OAuth is required
	if utils.RequiresAuth(orchestrator) {
		if !orchestrator.Spec.DisableOrchestrator {
			if err := r.ensureOrchestratorKubeRBACProxyConfigMap(ctx, orchestrator); err != nil {
				log.Error(err, "Failed to ensure orchestrator kube-rbac-proxy ConfigMap")
				return ctrl.Result{}, err
			}
		}

		if orchestrator.Spec.EnableGuardrailsGateway {
			if err := r.ensureGatewayKubeRBACProxyConfigMap(ctx, orchestrator); err != nil {
				log.Error(err, "Failed to ensure gateway kube-rbac-proxy ConfigMap")
				return ctrl.Result{}, err
			}
		}

		if orchestrator.Spec.EnableBuiltInDetectors {
			if err := r.ensureBuiltInKubeRBACProxyConfigMap(ctx, orchestrator); err != nil {
				log.Error(err, "Failed to ensure built-in detectors kube-rbac-proxy ConfigMap")
				return ctrl.Result{}, err
			}
		}
	}

	// === SERVICE ========================================================================================================================
	err = utils.ReconcileService(ctx, r.Client, orchestrator, getServiceConfig(orchestrator), serviceTemplatePath, templateParser.ParseResource)
	if err != nil {
		r.handleReconciliationError(ctx, log, orchestrator, err, utils.ReconcileFailed, "Failed to reconcile service")
		return ctrl.Result{}, err
	}

	_, _, err = utils.ReconcileConfigMap(ctx, r.Client, orchestrator, orchestrator.Name+"-ca-bundle", "", "ca-bundle-configmap.tmpl.yaml", templateParser.ParseResource)
	if err != nil {
		r.handleReconciliationError(ctx, log, orchestrator, err, utils.ReconcileFailed, "CA Bundle ConfigMap reconciliation failed")
		return ctrl.Result{}, err
	}

	// === DEPLOYMENT ========================================================================================================================
	existingDeployment := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: orchestrator.Name, Namespace: orchestrator.Namespace}, existingDeployment)
	if err != nil && errors.IsNotFound(err) {
		// Create a new deployment
		deployment, err := r.createDeployment(ctx, orchestrator)
		if err != nil {
			r.handleReconciliationErrorWithTrace(ctx, log, orchestrator, err, utils.ReconcileFailed, "Failed to create Deployment", "Deployment", orchestrator.Name, "Namespace", orchestrator.Namespace)
			return ctrl.Result{}, err
		}

		if orchestrator.Spec.TLSSecrets != nil && len(*orchestrator.Spec.TLSSecrets) > 0 {
			for _, tlsSecret := range *orchestrator.Spec.TLSSecrets {
				tlsMounts = append(tlsMounts, gorchv1alpha1.DetectedService{
					Name:      "",
					Type:      "",
					Scheme:    "",
					Hostname:  "",
					Port:      "",
					TLSSecret: tlsSecret,
				})
			}
		}

		// add TLS mounts to deployment
		err = r.addTLSMounts(ctx, orchestrator, deployment, tlsMounts)
		if err != nil {
			if errors.IsNotFound(err) {
				log.Info("Could not find required TLS serving secrets, will try again.")
				return ctrl.Result{}, nil
			}
			r.handleReconciliationError(ctx, log, orchestrator, err, utils.ReconcileFailed, "Failed to add TLS Mounts")
			return ctrl.Result{}, err
		}

		log.Info("Creating a new Deployment", "Deployment.Namespace", deployment.Namespace, "Deployment.Name", deployment.Name)

		// Ensure correct configmap hash annotations on first creation
		annotations := deployment.Spec.Template.Annotations
		if annotations == nil {
			annotations = map[string]string{}
		}
		r.setConfigMapHashAnnotations(ctx, orchestrator, annotations)
		deployment.Spec.Template.Annotations = annotations

		err = r.Create(ctx, deployment)
		if err != nil {
			r.handleReconciliationErrorWithTrace(ctx, log, orchestrator, err, utils.ReconcileFailed, "Failed to create new Deployment", "Deployment.Namespace", deployment.Namespace, "Deployment.Name", deployment.Name)
			return ctrl.Result{}, err
		}
	} else if err != nil {
		r.handleReconciliationError(ctx, log, orchestrator, err, utils.ReconcileFailed, "Failed to get Deployment")
		return ctrl.Result{}, err
	}

	// monitor the orchestrator or gateway config for changes
	if getOrchestratorConfigMap(orchestrator) != nil {
		if result, err := r.redeployOnConfigMapChange(ctx, log, orchestrator, tlsMounts); err != nil {
			r.handleReconciliationError(ctx, log, orchestrator, err, AutoConfigFailed, "Failed to monitor autoconfig configmaps for changes")
			return result, err
		}
	}

	// === ROUTES ========================================================================================================================
	if !orchestrator.Spec.DisableOrchestrator {
		err = r.reconcileOrchestratorRoute(ctx, orchestrator)
		if err != nil {
			r.handleReconciliationError(ctx, log, orchestrator, err, utils.ReconcileFailed, "Main orchestrator route reconciliation failed")
			return ctrl.Result{}, err
		}

		err = r.reconcileHealthRoute(ctx, orchestrator)
		if err != nil {
			r.handleReconciliationError(ctx, log, orchestrator, err, utils.ReconcileFailed, "Health route reconciliation failed")
			return ctrl.Result{}, err
		}
	}

	if orchestrator.Spec.EnableGuardrailsGateway {
		err = r.reconcileGatewayRoute(ctx, orchestrator)
		if err != nil {
			r.handleReconciliationError(ctx, log, orchestrator, err, utils.ReconcileFailed, "Gateway route reconciliation failed")
			return ctrl.Result{}, err
		}
	}

	if orchestrator.Spec.EnableBuiltInDetectors {
		err = r.reconcileBuiltInDetectorRoute(ctx, orchestrator)
		if err != nil {
			r.handleReconciliationError(ctx, log, orchestrator, err, utils.ReconcileFailed, "Built-in route reconciliation failed")
			return ctrl.Result{}, err
		}
	}

	// === METRIC SERVICE MONITOR ======================================================================================================================
	existingSM := &monitoringv1.ServiceMonitor{}
	err = r.Get(ctx, types.NamespacedName{Name: orchestrator.Name + "-service-monitor", Namespace: orchestrator.Namespace}, existingSM)
	if orchestrator.Spec.EnableBuiltInDetectors {
		if err != nil && errors.IsNotFound(err) {
			// Define a new route
			serviceMonitor := r.createServiceMonitor(ctx, orchestrator)
			log.Info("Creating a new Service Monitor", "ServiceMonitor.Namespace", serviceMonitor.Namespace, "ServiceMonitor.Name", serviceMonitor.Name)
			err = r.Create(ctx, serviceMonitor)
			if err != nil {
				r.handleReconciliationErrorWithTrace(ctx, log, orchestrator, err, utils.ReconcileFailed, "Failed to create new ServiceMonitor", "ServiceMonitor.Namespace", serviceMonitor.Namespace, "ServiceMonitor.Name", serviceMonitor.Name)
			}
		} else if err != nil {
			r.handleReconciliationError(ctx, log, orchestrator, err, utils.ReconcileFailed, "Failed to get ServiceMonitor")
			return ctrl.Result{}, err
		}
	} else {
		if err == nil {
			log.Info("Deleting ServiceMonitor because EnableBuiltInDetectors is false", "ServiceMonitor.Namespace", existingSM.Namespace, "ServiceMonitor.Name", existingSM.Name)
			if delErr := r.Delete(ctx, existingSM); delErr != nil && !errors.IsNotFound(delErr) {
				r.handleReconciliationErrorWithTrace(ctx, log, orchestrator, delErr, utils.ReconcileFailed, "Failed to delete ServiceMonitor", "ServiceMonitor.Namespace", existingSM.Namespace, "ServiceMonitor.Name", existingSM.Name)
				return ctrl.Result{}, delErr
			}
		} else if err != nil && !errors.IsNotFound(err) {
			r.handleReconciliationError(ctx, log, orchestrator, err, utils.ReconcileFailed, "Failed to get ServiceMonitor for deletion")
			return ctrl.Result{}, err
		}
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
		// Add a watch for changes to orchestrator-config or gateway-config ConfigMaps
		Watches(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []ctrl.Request {
				var requests []ctrl.Request
				// List all GuardrailsOrchestrators in the namespace
				var orchestrators gorchv1alpha1.GuardrailsOrchestratorList
				if err := r.List(ctx, &orchestrators, &client.ListOptions{Namespace: obj.GetNamespace()}); err != nil {
					return nil
				}
				for _, orch := range orchestrators.Items {
					orchConfigMap := getOrchestratorConfigMap(&orch)
					gatewayConfigMap := getGatewayConfigMap(&orch)

					// apply a watch to the orch and gateway configs
					if (orchConfigMap != nil && *orchConfigMap == obj.GetName()) ||
						(gatewayConfigMap != nil && *gatewayConfigMap == obj.GetName()) {
						requests = append(requests, ctrl.Request{
							NamespacedName: types.NamespacedName{
								Name:      orch.Name,
								Namespace: orch.Namespace,
							},
						})
					}
				}
				return requests
			}),
			builder.WithPredicates(predicate.Or(
				predicate.AnnotationChangedPredicate{},
				predicate.ResourceVersionChangedPredicate{},
				predicate.GenerationChangedPredicate{},
			)),
		).
		// Watch for changes to any matching InferenceService
		Watches(
			&kservev1beta1.InferenceService{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []ctrl.Request {
				var requests []ctrl.Request
				var orchestrators gorchv1alpha1.GuardrailsOrchestratorList
				if err := r.List(ctx, &orchestrators, &client.ListOptions{Namespace: obj.GetNamespace()}); err != nil {
					return nil
				}
				for _, orch := range orchestrators.Items {
					// apply a watch to any inference service being used by an orchestrator config
					if orch.Spec.AutoConfig != nil {
						val, ok := obj.GetLabels()[orch.Spec.AutoConfig.DetectorServiceLabelToMatch]
						if (ok && val == "true") || obj.GetName() == orch.Spec.AutoConfig.InferenceServiceToGuardrail {
							requests = append(requests, ctrl.Request{
								NamespacedName: types.NamespacedName{
									Name:      orch.Name,
									Namespace: orch.Namespace,
								},
							})
						}
					}
				}
				return requests
			}),
			builder.WithPredicates(predicate.Or(
				predicate.AnnotationChangedPredicate{},
				predicate.ResourceVersionChangedPredicate{},
				predicate.GenerationChangedPredicate{},
			)),
		).
		Complete(r)
}

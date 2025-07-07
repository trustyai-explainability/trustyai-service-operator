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
	"crypto/sha256"
	"fmt"
	"github.com/go-logr/logr"
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
// +kubebuilder:rbac:groups=serving.kserve.io,resources=servingruntimes,verbs=get;list
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

	if orchestrator.Spec.AutoConfig != nil {
		result, err := r.reconcileAutoConfig(ctx, log, orchestrator)
		return result, err
	} else {
		log.Info("Using manually-configured OrchestratorConfig")
		existingConfigMap := &corev1.ConfigMap{}
		err = r.Get(ctx, types.NamespacedName{Name: *orchestrator.Spec.OrchestratorConfig, Namespace: orchestrator.Namespace}, existingConfigMap)
		if err != nil {
			log.Error(err, "Failed to get existing ConfigMap", "ConfigMap.Name", *orchestrator.Spec.OrchestratorConfig, "ConfigMap.Namespace", orchestrator.Namespace)
			if client.IgnoreNotFound(err) != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
	}

	// monitor the gateway config for changes
	if orchestrator.Spec.EnableGuardrailsGateway && orchestrator.Spec.SidecarGatewayConfig != nil {
		// Monitor the configmap named in orchestrator.Spec.SidecarGatewayConfig
		sidecarGatewayConfigName := *orchestrator.Spec.SidecarGatewayConfig
		existingGatewayConfigMap := &corev1.ConfigMap{}
		err = r.Get(ctx, types.NamespacedName{Name: sidecarGatewayConfigName, Namespace: orchestrator.Namespace}, existingGatewayConfigMap)
		if err != nil {
			log.Error(err, "Failed to get SidecarGatewayConfig ConfigMap", "ConfigMap.Name", sidecarGatewayConfigName, "ConfigMap.Namespace", orchestrator.Namespace)
			return ctrl.Result{}, err
		} else {
			// Check if the configmap has changed by comparing a hash annotation
			existingDeployment := &appsv1.Deployment{}
			err = r.Get(ctx, types.NamespacedName{Name: orchestrator.Name, Namespace: orchestrator.Namespace}, existingDeployment)
			if err == nil {
				// Compute a hash of the configmap data
				configData := existingGatewayConfigMap.Data["gateway-config.yaml"]
				hash := fmt.Sprintf("%x", sha256.Sum256([]byte(configData)))
				annotationKey := "trustyai.opendatahub.io/sidecar-gateway-config-hash"
				annotations := existingDeployment.Spec.Template.Annotations
				if annotations == nil {
					annotations = map[string]string{}
				}
				if annotations[annotationKey] != hash {
					annotations[annotationKey] = hash
					existingDeployment.Spec.Template.Annotations = annotations
					if updateErr := r.Update(ctx, existingDeployment); updateErr != nil {
						log.Error(updateErr, "Failed to redeploy orchestrator after SidecarGatewayConfig change")
						return ctrl.Result{}, updateErr
					}
					log.Info("Redeployed orchestrator deployment due to SidecarGatewayConfig change")
				}
			} else if errors.IsNotFound(err) {
				log.Info("Deployment not found, will be created in subsequent reconciliation")
			} else {
				log.Error(err, "Failed to get orchestrator deployment for redeploy after SidecarGatewayConfig change")
				return ctrl.Result{}, err
			}
		}
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

func (r *GuardrailsOrchestratorReconciler) orchestratorConfigLogic(ctx context.Context, log logr.Logger, orchestrator *gorchv1alpha1.GuardrailsOrchestrator, cm *corev1.ConfigMap) (bool, error) {
	// Check if the configmap already exists and is up-to-date
	existingCM := &corev1.ConfigMap{}
	configMapChanged := false
	err := r.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, existingCM)
	if err == nil {
		// ConfigMap exists, check if data is the same
		if existingCM.Data["config.yaml"] == cm.Data["config.yaml"] {
			//pass
		} else {
			// Update only if data is different
			existingCM.Data = cm.Data
			if updateErr := r.Update(ctx, existingCM); updateErr != nil {
				log.Error(updateErr, "Failed to update existing orchestrator configmap")
				return configMapChanged, updateErr
			}
			configMapChanged = true
			log.Info("Updated existing OrchestratorConfig ConfigMap with new configuration")
		}
	} else if errors.IsNotFound(err) {
		// ConfigMap does not exist, create it
		if createErr := r.Create(ctx, cm); createErr != nil {
			log.Error(createErr, "Failed to create orchestrator configmap")
			return configMapChanged, createErr
		}
		configMapChanged = true
		log.Info("Automatically generated an OrchestratorConfig from resources in namespace")
	} else {
		log.Error(err, "Failed to get orchestrator configmap")
		return configMapChanged, err
	}

	// Set orchestrator.Spec.OrchestratorConfig to use the automatically generated config
	latestOrchestrator := &gorchv1alpha1.GuardrailsOrchestrator{}
	if err := r.Get(ctx, types.NamespacedName{Name: orchestrator.Name, Namespace: orchestrator.Namespace}, latestOrchestrator); err != nil {
		log.Error(err, "Failed to re-fetch Orchestrator before patching")
		return configMapChanged, err
	}
	// Patch only the OrchestratorConfig field in the spec
	patch := client.MergeFrom(latestOrchestrator.DeepCopy())
	latestOrchestrator.Spec.OrchestratorConfig = &cm.Name
	if err := r.Patch(ctx, latestOrchestrator, patch); err != nil {
		log.Error(err, "Failed to patch OrchestratorConfig in CR")
		return configMapChanged, err
	}

	return configMapChanged, nil
}

func (r *GuardrailsOrchestratorReconciler) gatewayConfigLogic(ctx context.Context, log logr.Logger, orchestrator *gorchv1alpha1.GuardrailsOrchestrator, cm *corev1.ConfigMap) (bool, error) {
	// Check if the configmap already exists and is up-to-date
	existingCM := &corev1.ConfigMap{}
	configMapChanged := false
	err := r.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, existingCM)
	if errors.IsNotFound(err) {
		// ConfigMap does not exist, create it
		if createErr := r.Create(ctx, cm); createErr != nil {
			log.Error(createErr, "Failed to create orchestrator configmap")
			return false, createErr
		}
		configMapChanged = true
		log.Info("Automatically generated a GatewayConfig from resources in namespace")
	}

	// Set orchestrator.Spec.OrchestratorConfig to use the automatically generated config
	latestOrchestrator := &gorchv1alpha1.GuardrailsOrchestrator{}
	if err := r.Get(ctx, types.NamespacedName{Name: orchestrator.Name, Namespace: orchestrator.Namespace}, latestOrchestrator); err != nil {
		log.Error(err, "Failed to re-fetch Orchestrator before patching")
		return configMapChanged, err
	}
	// Patch only the OrchestratorConfig field in the spec
	patch := client.MergeFrom(latestOrchestrator.DeepCopy())
	latestOrchestrator.Spec.SidecarGatewayConfig = &cm.Name
	if err := r.Patch(ctx, latestOrchestrator, patch); err != nil {
		log.Error(err, "Failed to patch GatewayConfig in CR")
		return configMapChanged, err
	}

	return configMapChanged, nil
}

func (r *GuardrailsOrchestratorReconciler) reconcileAutoConfig(ctx context.Context, log logr.Logger, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) (ctrl.Result, error) {
	// Generate orchestrator configmap spec
	cm, cmGateway, err := r.GenerateOrchestratorConfigMaps(ctx, orchestrator)
	if err != nil {
		log.Error(err, "Failed to automatically generate orchestrator configmap")
		return ctrl.Result{}, err
	}

	orchestratorConfigChange, err := r.orchestratorConfigLogic(ctx, log, orchestrator, cm)
	if err != nil {
		log.Error(err, "Failed to automatically create/update orchestrator configmap")
	}

	gatewayConfigChange := false
	if cmGateway != nil {
		gatewayConfigChange, err = r.gatewayConfigLogic(ctx, log, orchestrator, cmGateway)
		if err != nil {
			log.Error(err, "Failed to automatically create/update orchestrator configmap")
		}
	}

	// If the configmap changed, redeploy the orchestrator deployment
	if orchestratorConfigChange || gatewayConfigChange {
		existingDeployment := &appsv1.Deployment{}
		err = r.Get(ctx, types.NamespacedName{Name: orchestrator.Name, Namespace: orchestrator.Namespace}, existingDeployment)
		if err == nil {
			// Annotate the deployment to force a rollout
			if existingDeployment.Spec.Template.Annotations == nil {
				existingDeployment.Spec.Template.Annotations = map[string]string{}
			}
			existingDeployment.Spec.Template.Annotations["trustyai.opendatahub.io/configmap-redeployed-at"] = time.Now().Format(time.RFC3339Nano)
			if updateErr := r.Update(ctx, existingDeployment); updateErr != nil {
				log.Error(updateErr, "Failed to redeploy orchestrator after configmap change")
				return ctrl.Result{}, updateErr
			}
			log.Info("Redeployed orchestrator deployment due to configmap change")
		} else if errors.IsNotFound(err) {
			log.Info("Deployment not found, will be created in subsequent reconciliation")
		} else {
			log.Error(err, "Failed to get orchestrator deployment for redeploy")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GuardrailsOrchestratorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gorchv1alpha1.GuardrailsOrchestrator{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

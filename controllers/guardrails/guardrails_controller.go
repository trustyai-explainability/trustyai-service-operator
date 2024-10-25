/*
Copyright 2024.

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

package guardrails

import (
	"context"

	corev1 "k8s.io/api/core/v1"

	guardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/guardrails/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type ServiceOptions struct {
	OrchestratorImage string
	ImagePullPolicy   corev1.PullPolicy
	GrpcPort          int
	GrpcService       string
	GrpcServerSecret  string
	GrpcClientSecret  string
}

// GuardrailsOrchestrator reconciles a GuardrailsOrchestrator object
type GuardrailsOrchestratorReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	EventRecorder record.EventRecorder
	ConfigMap     string
	Namespace     string
}

func ControllerSetUp(mgr manager.Manager, ns, configmap string, recorder record.EventRecorder) error {
	return (&GuardrailsOrchestratorReconciler{
		ConfigMap:     configmap,
		Namespace:     ns,
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		EventRecorder: recorder,
	}).SetupWithManager(mgr)
}

// +kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=guardrailsorchestrators,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=guardrailsorchestrators/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=guardrailsorchestrators/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=deployments,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;watch;list
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;watch;list
func (r *GuardrailsOrchestratorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	orchestrator := &guardrailsv1alpha1.GuardrailsOrchestrator{}
	err := r.Get(ctx, req.NamespacedName, orchestrator)
	if err != nil {
		if errors.IsNotFound {
			log.Info("unable to find GuardrailsOrchestrator. check if CR is being deleted")
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		log.Error(err, "failed to get GuardrailsOrchestrator")
		return ctrl.Result{}, err
	}

	if orchestrator.DeletionTimestamp != nil {
		if utils.ContainsString(orchestrator.Finalizers, finalizerName) {
			if err := r.deleteExternalDependency(req.Name, orchestrator, req.Namespace, ctx); err != nil {
				log.FromContext(ctx).Error(err, "Failed to delete external dependencies, but preceeding with finalizer removal.")
			}
		}
		//  Remove the finalizer and update the orchestrator
		orchestrator.Finalizers = utils.RemoveString(orchestrator.Finalizers, finalizerName)
		if err := r.Update(ctx, orchestrator); err != nil {
			log.FromContext(ctx).Error(err, "Failed to remove the finalizer")
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{Requeue: false}, nil
	}

	// Add the finalizer if it does not exist
	if !utils.ContainsString(orchestrator.Finalizers, finalizerName) {
		orchestrator.Finalizers = append(orchestrator.Finalizers, finalizerName)
		if err := r.Update(ctx, orchestrator); err != nil {
			log.FromContext(ctx).Error(err, "Failed to add the finalizer")
		}
	}

	// Apply the deployment spec
	if err := r.ensureDeployment(ctx, orchestrator); err != nil {
		log.FromContext(ctx).Error(err, "Deployment failedf")
		return ctrl.Result{Requeue: true}, nil
	}

	// Reconcile statuses
	result, err := r.reconcileStatuses(ctx, orchestrator)
	if err != nil {
		return result, err
	}
	return ctrl.Result{Requeue: true}, nil
}

func (r *GuardrailsOrchestratorReconciler) SetupWithManager(mgr ctrl.Manager) error {

	return ctrl.NewControllerManagedBy(mgr).
		For(&guardrailsv1alpha1.GuardrailsOrchestrator{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.Service{}).
		Owns(&appsv1.Route{}).
		Complete(r)
}

// func (*GuardrailsOrchestratorReconciler) patchForMTLS(orchestrator guardrailsv1alpha1.GuardrailOrchestrator) {
// 	envVars := guardrails.spec.containers.env
// 	if tlsMode == TLSMode_mTLS {
// 		envVars = append(envVars,
// 			corev1.EnvVar{
// 			Name: TLS_KEY_PATH,
// 			Value: "/tls/orch/server.key",

// 			},
// 			corev1.EnvVar{
// 				Name: TLS_CERT_PATH,
// 				Value: "/tls/orch/server.crt",
// 			},
// 			corev1.EnvVar{
// 				Name: TLS_CLIENT_CA_CERT_PATH,
// 				Value: "/tls/orch/ca.crt",
// 			},
// 		)

// 	} else if tlsMode == TLSMode_TLS {
// 		envVars = append(envVars,
// 			corev1.EnvVar{
// 			},
// 		)
// 	}
// 	return envVars
// }

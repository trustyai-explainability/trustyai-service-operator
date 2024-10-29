package guardrails

import (
	"context"

	guardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/guardrails/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type GuardrailsReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	EventRecorder record.EventRecorder
	Namespace     string
}

func ControllerSetUp(mgr ctrl.Manager, recorder record.EventRecorder, ns string) error {
	return (&GuardrailsReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		EventRecorder: recorder,
		Namespace:     ns,
	}).SetupWithManager(mgr)
}

func (r *GuardrailsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// your logic here
	log := log.FromContext(ctx)
	orchestrator := &guardrailsv1alpha1.GuardrailsOrchestrator{}
	err := r.Get(context.TODO(), req.NamespacedName, orchestrator)
	if err != nil {
		log.Error(err, "unable to fetch GuardrailsOrchestrator")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if orchestrator.DeletionTimestamp != nil {
		if utils.ContainsString(orchestrator.Finalizers, FinalizerName) {
			// Delete the deployment

			if err := r.deleteDeployment(ctx, orchestrator); err != nil {
				log.Error(err, "unable to delete GuardrailsOrchestrator deployment")
			}
			log.Info("deleted GuardrailsOrchestrator deployment")
		}
		orchestrator.Finalizers = utils.RemoveString(orchestrator.Finalizers, FinalizerName)
		if err := r.Update(ctx, orchestrator); err != nil {
			log.Error(err, "failed to remove the finalizer")
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Handle creation
	if !utils.ContainsString(orchestrator.Finalizers, FinalizerName) {
		orchestrator.Finalizers = append(orchestrator.Finalizers, FinalizerName)
		if err := r.Update(ctx, orchestrator); err != nil {
			log.Error(err, "failed to add the finalizer")
		}
	}
	deployment := createDeployment(orchestrator, log)
	if err := r.Create(ctx, deployment); err != nil {
		log.Error(err, "unable to create deployment")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *GuardrailsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&guardrailsv1alpha1.GuardrailsOrchestrator{}).
		Watches(
			&source.Kind{Type: &appsv1.Deployment{}},
			&handler.EnqueueRequestForOwner{
				OwnerType:    &guardrailsv1alpha1.GuardrailsOrchestrator{},
				IsController: true,
			},
		).
		Complete(r)
}

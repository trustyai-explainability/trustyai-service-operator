package guardrails

import (
	"context"
	"reflect"

	guardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/guardrails/v1alpha1"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/tas/templates"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	deploymentTemplatePath = "templates/deployment.tmpl.yaml"
)

type DeploymentConfig struct {
	Orchestrator      *guardrailsv1alpha1.GuardrailsOrchestrator
	OrchestratorImage string
}

func (r *GuardrailsOrchestratorReconciler) createDeploymentObject(ctx context.Context, orchestrator *guardrailsv1alpha1.GuardrailsOrchestrator, orchestratorImage string) (*appsv1.Deployment, error) {
	deploymentConfig := DeploymentConfig{
		Orchestrator:      orchestrator,
		OrchestratorImage: orchestratorImage,
	}

	var deployment *appsv1.Deployment
	deployment, err := .ParseResource[appsv1.Deployment](deploymentTemplatePath, deploymentConfig, reflect.TypeOf(&appsv1.Deployment{}))
	if err != nil {
		log.FromContext(ctx).Error(err, "Error parsing the orchestrator's deployment template")
		return nil, err
	}
	return deployment, nil
}

func (r *GuardrailsOrchestratorReconciler) createDeployment(ctx context.Context, orchestrator *guardrailsv1alpha1.GuardrailsOrchestrator) error {
	deployment, err := r.createDeploymentObject(ctx, orchestrator, defaultImage)
	if err != nil {
		return err
	}
	if err := ctrl.SetControllerReference(orchestrator, deployment, r.Scheme); err != nil {
		log.FromContext(ctx).Error(err, "Error setting Guardrails Orchestrator as the owner of Deployment")
		return err
	}
	log.FromContext(ctx).Info("Creating Deployment")
	err = r.Create(ctx, deployment)
	if err != nil {
		log.FromContext(ctx).Error(err, "Error creating Deployment")
	}
	return nil
}

func (r *GuardrailsOrchestratorReconciler) checkDeploymentReady(ctx context.Context, orchestrator *guardrailsv1alpha1.GuardrailsOrchestrator) (bool, error) {
	deployment := &appsv1.Deployment{}

	err := r.Get(ctx, types.NamespacedName{Name: orchestrator.Name, Namespace: orchestrator.Namespace}, deployment)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	for _, cond := range deployment.Status.Conditions {
		if cond.Type == appsv1.DeploymentAvailable && cond.Status == corev1.ConditionTrue {
			if deployment.Status.ReadyReplicas == *deployment.Spec.Replicas {
				podList := &corev1.PodList{}
				listOpts := []client.ListOption{
					client.InNamespace(orchestrator.Namespace),
					client.MatchingLabels(deployment.Spec.Selector.MatchLabels),
				}
				if err := r.List(ctx, podList, listOpts...); err != nil {
					return false, err
				}

				for _, pod := range podList.Items {
					for _, cs := range pod.Status.ContainerStatuses {
						if cs.State.Waiting != nil && cs.State.Waiting.Reason == StateReasonCrashLoopBackOff {
							return false, nil
						}
						if cs.State.Terminated != nil && cs.State.Terminated.ExitCode != 0 {
							return false, nil
						}
					}
				}

				return true, nil
			}
		}
	}

	return false, nil
}

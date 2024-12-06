package guardrails

import (
	"context"
	"reflect"

	guardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/guardrails/v1alpha1"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var deploymentTemplatePath = "templates/deployment.yaml"

type DeploymentConfig struct {
	Orchestrator    *guardrailsv1alpha1.GuardrailsOrchestrator
	ContainerImage  string
	VolumeMountName string
}

func (r *GuardrailsReconciler) ensureDeployment(ctx context.Context, orchestrator *guardrailsv1alpha1.GuardrailsOrchestrator) error {
	// Check if the deployment already exists, if not create a new one
	existingDeployment := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{Name: orchestrator.Name, Namespace: orchestrator.Namespace}, existingDeployment)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create the deployment
			return r.createDeployment(ctx, orchestrator, "")
		}
		// Error that isn't due to the deployment not existing
		return err
	}
	return nil
}

func (r *GuardrailsReconciler) createDeployment(ctx context.Context, orchestrator *guardrailsv1alpha1.GuardrailsOrchestrator, orchestratorImage string) error {

	deploymentConfig := DeploymentConfig{
		Orchestrator:    orchestrator,
		ContainerImage:  orchestratorImage,
		VolumeMountName: "",
	}
	var deployment *appsv1.Deployment
	deployment, err := templateParser.ParseResource[appsv1.Deployment](deploymentTemplatePath, deploymentConfig, reflect.TypeOf(appsv1.Deployment{}))
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to parse deployment template")
	}
	controllerutil.SetControllerReference(orchestrator, deployment, r.Scheme)
	err = r.Create(ctx, deployment)
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to create deployment")
		return err
	}
	return nil
}

func (r *GuardrailsReconciler) deleteDeployment(ctx context.Context, orchestrator *guardrailsv1alpha1.GuardrailsOrchestrator) error {
	deployment := appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      orchestrator.Name,
			Namespace: orchestrator.Namespace,
		},
	}
	return r.Delete(ctx, &deployment)
}

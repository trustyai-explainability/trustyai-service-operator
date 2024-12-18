package guardrails

import (
	"context"
	"reflect"

	guardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/guardrails/v1alpha1"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const serviceTemplatePath = "templates/service.yaml"

type ServiceConfig struct {
	Name      string
	Namespace string
}

func (r *GuardrailsReconciler) ensureService(ctx context.Context, orchestrator *guardrailsv1alpha1.GuardrailsOrchestrator) error {
	// Check if the service already exists, if not create a new one
	existingService := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: orchestrator.Name, Namespace: orchestrator.Namespace}, existingService)
	if err != nil {
		if errors.IsNotFound(err) {
			return r.createService(ctx, orchestrator)
		}
		return err
	}
	return nil
}

func (r *GuardrailsReconciler) createService(ctx context.Context, orchestrator *guardrailsv1alpha1.GuardrailsOrchestrator) error {

	ServiceConfig := ServiceConfig{
		Name:      orchestrator.Name,
		Namespace: orchestrator.Namespace,
	}

	var service *corev1.Service
	service, err := templateParser.ParseResource[corev1.Service](serviceTemplatePath, ServiceConfig, reflect.TypeOf(&corev1.Service{}))
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to parse service template")
		return err
	}

	controllerutil.SetControllerReference(orchestrator, service, r.Scheme)

	err = r.Create(ctx, service)
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to create deployment")
		return err
	}
	return nil
}

package gorch

import (
	"context"
	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/gorch/templates"
	corev1 "k8s.io/api/core/v1"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const serviceTemplatePath = "service.tmpl.yaml"

type ServiceConfig struct {
	Orchestrator  *gorchv1alpha1.GuardrailsOrchestrator
	UseOAuthProxy bool
}

func (r *GuardrailsOrchestratorReconciler) createService(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) *corev1.Service {

	serviceConfig := ServiceConfig{
		Orchestrator:  orchestrator,
		UseOAuthProxy: requiresOAuth(orchestrator),
	}

	var service *corev1.Service
	service, err := templateParser.ParseResource[corev1.Service](serviceTemplatePath, serviceConfig, reflect.TypeOf(&corev1.Service{}))
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to parse service template")
	}
	controllerutil.SetControllerReference(orchestrator, service, r.Scheme)
	return service

}

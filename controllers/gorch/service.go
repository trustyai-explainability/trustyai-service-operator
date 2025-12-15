package gorch

import (
	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
)

const serviceTemplatePath = "service.tmpl.yaml"

func getServiceConfig(orchestrator *gorchv1alpha1.GuardrailsOrchestrator) utils.ServiceConfig {
	return utils.ServiceConfig{
		Owner:        orchestrator,
		Name:         orchestrator.Name + "-service",
		Namespace:    orchestrator.Namespace,
		Version:      constants.Version,
		UseAuthProxy: utils.RequiresAuth(orchestrator),
	}
}

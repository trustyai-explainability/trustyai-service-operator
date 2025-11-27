package tas

import (
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
)

const (
	serviceTemplatePath    = "service/service-internal.tmpl.yaml"
	serviceTLSTemplatePath = "service/service-tls.tmpl.yaml"
)

func getServiceConfig(name string, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) utils.ServiceConfig {
	return utils.ServiceConfig{
		Name:         name,
		Namespace:    instance.GetNamespace(),
		Owner:        instance,
		Version:      constants.Version,
		UseAuthProxy: false,
	}
}

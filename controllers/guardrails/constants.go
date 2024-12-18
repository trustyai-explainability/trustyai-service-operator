package guardrails

import (
	"embed"
	"text/template"
)

var templateFS embed.FS

const (
	GuardrailsOrchestratorPath = "/app/bin/fms-guaradrails-orchestr8"
	DefaultContainerImage      = "quay.io/rh-ee-mmisiura/fms-orchestr8-nlp:main_79434f2"
	OrchestratorName           = "fms-orchestr8-nlp"
	FinalizerName              = "trustyai.opendatahub.io/finalizer"
	HTTPPort                   = "443"
	ServiceName                = "Guardrails"
)

func ParseTemplates() (*template.Template, error) {
	template, err := template.ParseFS(templateFS, "templates/*.yaml.tmpl")
	if err != nil {
		return nil, err
	}
	return template, err
}

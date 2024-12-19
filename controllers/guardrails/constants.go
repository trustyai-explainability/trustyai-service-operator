package guardrails

const (
	GuardrailsOrchestratorPath = "/app/bin/fms-guaradrails-orchestr8"
	DefaultContainerImage      = "quay.io/rh-ee-mmisiura/fms-orchestr8-nlp:main_79434f2"
	OrchestratorName           = "fms-orchestr8-nlp"
	FinalizerName              = "trustyai.opendatahub.io/finalizer"
	HTTPPort                   = "443"
	ServiceName                = "Guardrails"
)

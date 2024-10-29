package guardrails

const (
	GuardrailsOrchestratorPath = "/app/bin/fms-guaradrails-orchestr8"
	DefaultContainerImage      = " quay.io/rh-ee-mmisiura/fms-orchestr8-nlp:latest"
	OrchestratorName           = "fms-orchestr8-nlp"
	FinalizerName              = "trustyai.opendatahub.io/finalizer"
	HTTPSPort                  = "443"
)

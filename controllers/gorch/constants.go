package gorch

const (
	orchestratorName      = "guardrails-orchestrator"
	finalizerName         = "trustyai.opendatahub.io/gorch-finalizer"
	defaultContainerImage = string("quay.io/trustyai/ta-guardrails-orchestrator:latest")
	configMapKey          = "guardrailsOrchestratorImage"
	ServiceName           = "GORCH"
	Version               = "0.7.0"
)

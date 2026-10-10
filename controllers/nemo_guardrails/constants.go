package nemo_guardrails

const (
	ServiceName                       = "NEMO_GUARDRAILS"
	nemoGuardrailsImageKey            = "nemo-guardrails-image"
	nemoGuardrailsDefaultConfigPrefix = "trustyai-service-operator-nemo-guardrails-default"
	configMapKubeRBACProxyImageKey    = "kube-rbac-proxy"
	finalizerName                     = "trustyai.opendatahub.io/nemo-guardrails-finalizer"
	invalidAllowedConsumersReason     = "InvalidAllowedConsumers"
	validAllowedConsumersReason       = "ValidAllowedConsumers"
)

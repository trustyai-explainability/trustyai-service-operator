package gorch

const (
	orchestratorName      = "guardrails-orchestrator"
	finalizerName         = "trustyai.opendatahub.io/gorch-finalizer"
	configMapName         = "gorch-config"
	orchestratorImageKey  = "guardrails-orchestrator-image"
	gatewayImageKey       = "guardrails-sidecar-gateway-image"
	detectorImageKey      = "guardrails-built-in-detector-image"
	kubeRBACProxyImageKey = "kube-rbac-proxy"
	ServiceName           = "GORCH"
	builtInDetectorName   = "built-in-detector"
)

const (
	serviceAccountTemplatePath = "serviceaccount.tmpl.yaml"
)

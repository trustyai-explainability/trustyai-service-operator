package nemo_guardrails

const (
	ServiceName                    = "NEMO_GUARDRAILS"
	nemoGuardrailsImageKey         = "nemo-guardrails-image"
	configMapKubeRBACProxyImageKey = "kube-rbac-proxy"
	finalizerName                  = "trustyai.opendatahub.io/nemo-guardrails-finalizer"

	// manifestAnnotationKey surfaces the capability manifest URL on the CR so
	// in-cluster agents can discover it without hardcoded configuration
	// (RHAI-518). manifestPath mirrors MANIFEST_PATH in
	// nemoguardrails/server/manifest.py in the NeMo-Guardrails repo --
	// provisional pending EvalHub team alignment on the platform Agent
	// Discoverability contract (RHAI-517 AC); keep the two in sync until that
	// lands.
	manifestAnnotationKey = "trustyai.opendatahub.io/nemo-guardrails-manifest-url"
	manifestPath          = "/.well-known/ai-plugin.json"
)

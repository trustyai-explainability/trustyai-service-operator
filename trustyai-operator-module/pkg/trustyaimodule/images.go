package trustyaimodule

// paramsEnvMap maps each params.env key (as used in the workload operator's
// config/overlays/*/params.env files) to the RELATED_IMAGE_* environment
// variable injected by the ODH platform into the module controller pod.
//
// Keys are in the same format as the workload operator's params.env files.
// Non-image keys in params.env (e.g. kServeServerless, lmes-pod-checking-interval)
// are not listed here and are left unchanged by applyParams.
var paramsEnvMap = map[string]string{
	"trustyaiOperatorImage":            "RELATED_IMAGE_ODH_TRUSTYAI_OPERATOR_IMAGE",
	"trustyaiServiceImage":             "RELATED_IMAGE_ODH_TRUSTYAI_SERVICE_IMAGE",
	"evalHubImage":                     "RELATED_IMAGE_ODH_EVAL_HUB_IMAGE",
	"evalHubMCPImage":                  "RELATED_IMAGE_ODH_EVAL_HUB_MCP_IMAGE",
	"kube-rbac-proxy":                  "RELATED_IMAGE_ODH_KUBE_RBAC_PROXY_IMAGE",
	"lmes-pod-image":                   "RELATED_IMAGE_ODH_TA_LMES_JOB_IMAGE",
	"lmes-driver-image":                "RELATED_IMAGE_ODH_TA_LMES_DRIVER_IMAGE",
	"guardrails-orchestrator-image":    "RELATED_IMAGE_ODH_FMS_GUARDRAILS_ORCHESTRATOR_IMAGE",
	"guardrails-built-in-detector-image":   "RELATED_IMAGE_ODH_BUILT_IN_DETECTOR_IMAGE",
	"guardrails-sidecar-gateway-image": "RELATED_IMAGE_ODH_TRUSTYAI_VLLM_ORCHESTRATOR_GATEWAY_IMAGE",
	"garak-provider-image":             "RELATED_IMAGE_ODH_TRUSTYAI_GARAK_LLS_PROVIDER_DSP_IMAGE",
	"ragas-provider-image":             "RELATED_IMAGE_ODH_TRUSTYAI_RAGAS_PROVIDER_IMAGE",
	"nemo-guardrails-image":            "RELATED_IMAGE_ODH_TRUSTYAI_NEMO_GUARDRAILS_SERVER_IMAGE",
	"evalhub-provider-guidellm-image":  "RELATED_IMAGE_ODH_EVALHUB_PROVIDER_GUIDELLM_IMAGE",
	"evalhub-provider-lighteval-image": "RELATED_IMAGE_ODH_EVALHUB_PROVIDER_LIGHTEVAL_IMAGE",
	"evalhub-provider-ibm-clear-image": "RELATED_IMAGE_ODH_EVALHUB_PROVIDER_IBM_CLEAR_IMAGE",
	"evalhub-provider-deepeval-image":  "RELATED_IMAGE_ODH_EVALHUB_PROVIDER_DEEPEVAL_IMAGE",
	"evalhub-provider-ragas-image":     "RELATED_IMAGE_ODH_EVALHUB_PROVIDER_RAGAS_IMAGE",
	"evalhub-provider-inspect-image":   "RELATED_IMAGE_ODH_EVALHUB_PROVIDER_INSPECT_IMAGE",
}

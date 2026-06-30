package images

import (
	"context"
	"fmt"
	"os"

	"github.com/trustyai-explainability/trustyai-service-operator/pkg/configmap"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Image key constants matching ConfigMap keys
const (
	TrustyAIServiceImageKey           = "trustyaiServiceImage"
	EvalHubImageKey                   = "evalHubImage"
	KubeRBACProxyKey                  = "kube-rbac-proxy"
	LMESPodImageKey                   = "lmes-pod-image"
	LMESDriverImageKey                = "lmes-driver-image"
	GuardrailsOrchestratorImageKey    = "guardrails-orchestrator-image"
	GuardrailsBuiltInDetectorImageKey = "guardrails-built-in-detector-image"
	GuardrailsSidecarGatewayImageKey  = "guardrails-sidecar-gateway-image"
	GarakProviderImageKey             = "garak-provider-image"
	NemoGuardrailsImageKey            = "nemo-guardrails-image"
)

// Operand image env vars injected by the ODH platform operator at deploy time
const (
	RelatedImageTrustyAIService         = "RELATED_IMAGE_ODH_TRUSTYAI_SERVICE_IMAGE"
	RelatedImageEvalHub                 = "RELATED_IMAGE_ODH_EVAL_HUB_IMAGE"
	RelatedImageKubeRBACProxy           = "RELATED_IMAGE_ODH_KUBE_RBAC_PROXY_IMAGE"
	RelatedImageLMESJob                 = "RELATED_IMAGE_ODH_TA_LMES_JOB_IMAGE"
	RelatedImageLMESDriver              = "RELATED_IMAGE_ODH_TA_LMES_DRIVER_IMAGE"
	RelatedImageGuardrailsOrchestrator  = "RELATED_IMAGE_ODH_FMS_GUARDRAILS_ORCHESTRATOR_IMAGE"
	RelatedImageBuiltInDetector         = "RELATED_IMAGE_ODH_BUILT_IN_DETECTOR_IMAGE"
	RelatedImageVLLMOrchestratorGateway = "RELATED_IMAGE_ODH_TRUSTYAI_VLLM_ORCHESTRATOR_GATEWAY_IMAGE"
	RelatedImageGarakLLSProviderDSP     = "RELATED_IMAGE_ODH_TRUSTYAI_GARAK_LLS_PROVIDER_DSP_IMAGE"
	RelatedImageNemoGuardrailsServer    = "RELATED_IMAGE_ODH_TRUSTYAI_NEMO_GUARDRAILS_SERVER_IMAGE"
)

// imageMapping maps ConfigMap keys to their corresponding RELATED_IMAGE_* environment variable names
var imageMapping = map[string]string{
	TrustyAIServiceImageKey:           RelatedImageTrustyAIService,
	EvalHubImageKey:                   RelatedImageEvalHub,
	KubeRBACProxyKey:                  RelatedImageKubeRBACProxy,
	LMESPodImageKey:                   RelatedImageLMESJob,
	LMESDriverImageKey:                RelatedImageLMESDriver,
	GuardrailsOrchestratorImageKey:    RelatedImageGuardrailsOrchestrator,
	GuardrailsBuiltInDetectorImageKey: RelatedImageBuiltInDetector,
	GuardrailsSidecarGatewayImageKey:  RelatedImageVLLMOrchestratorGateway,
	GarakProviderImageKey:             RelatedImageGarakLLSProviderDSP,
	NemoGuardrailsImageKey:            RelatedImageNemoGuardrailsServer,
}

// GetImageFromConfigMap is the legacy function signature that now uses the unified resolver.
// Deprecated: Use ResolveImage directly for better clarity.
func GetImageFromConfigMap(ctx context.Context, c client.Client, configMapKey, configMapName, namespace string) (string, error) {
	return ResolveImage(ctx, c, configMapKey, configMapName, namespace, "")
}

// GetImageFromConfigMapWithFallback is the legacy function signature with a default fallback value.
// Deprecated: Use ResolveImage directly for better clarity.
func GetImageFromConfigMapWithFallback(ctx context.Context, c client.Client, configMapKey, configMapName, namespace string, fallbackValue string) (string, error) {
	if namespace == "" {
		return fallbackValue, nil
	}
	image, err := ResolveImage(ctx, c, configMapKey, configMapName, namespace, fallbackValue)
	if err != nil {
		return fallbackValue, err
	}
	return image, nil
}

// ResolveImage resolves an operand image URL by checking in this order:
//  1. Injected env var for the image key (if configMapKey is recognized)
//  2. ConfigMap value at configMapKey
//  3. fallbackValue (if provided and non-empty)
//
// Returns an error if none of the above sources provide a value.
func ResolveImage(ctx context.Context, c client.Client, configMapKey, configMapName, namespace string, fallbackValue string) (string, error) {
	logger := log.FromContext(ctx)

	// Step 1: Check if there's a corresponding RELATED_IMAGE_* env var
	if envVarName, ok := imageMapping[configMapKey]; ok {
		if envValue := os.Getenv(envVarName); envValue != "" {
			logger.Info("resolved image", "key", configMapKey, "source", "env", "var", envVarName, "value", envValue)
			return envValue, nil
		}
	}

	// Step 2: Fall back to ConfigMap
	image, err := getImageFromConfigMapDirect(ctx, c, configMapKey, configMapName, namespace)
	if err == nil && image != "" {
		logger.Info("resolved image", "key", configMapKey, "source", "configmap", "name", configMapName, "namespace", namespace, "value", image)
		return image, nil
	}

	// Step 3: Use fallback value if provided
	if fallbackValue != "" {
		logger.Info("resolved image", "key", configMapKey, "source", "fallback", "value", fallbackValue)
		return fallbackValue, nil
	}

	// No value found
	if err != nil {
		return "", fmt.Errorf("failed to resolve image for key %s: %w", configMapKey, err)
	}
	return "", fmt.Errorf("no image value found for key %s (env var, configmap, or fallback)", configMapKey)
}

// getImageFromConfigMapDirect is an internal helper that directly reads from ConfigMap without env var check.
// This is used internally by ResolveImage to avoid circular logic.
func getImageFromConfigMapDirect(ctx context.Context, c client.Client, configMapKey, configMapName, namespace string) (string, error) {
	cm, err := configmap.Get(ctx, c, configMapName, namespace)
	if err != nil {
		return "", err
	}

	value, ok := cm.Data[configMapKey]
	if !ok {
		return "", fmt.Errorf("configmap %s in namespace %s does not contain key %s", configMapName, namespace, configMapKey)
	}
	return value, nil
}

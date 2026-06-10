package images

import (
	"context"
	"os"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
)

// configMapKeyToEnvVar maps operator ConfigMap image keys to RELATED_IMAGE_*
// environment variable names. The platform injects these env vars into the
// module operator Deployment; they take precedence over ConfigMap values.
var configMapKeyToEnvVar = map[string]string{
	"trustyaiServiceImage":               "RELATED_IMAGE_TRUSTYAI_SERVICE",
	"evalHubImage":                       "RELATED_IMAGE_EVALHUB",
	"kube-rbac-proxy":                    "RELATED_IMAGE_KUBE_RBAC_PROXY",
	"lmes-pod-image":                     "RELATED_IMAGE_LMES_POD",
	"lmes-driver-image":                  "RELATED_IMAGE_LMES_DRIVER",
	"guardrails-orchestrator-image":      "RELATED_IMAGE_GUARDRAILS_ORCHESTRATOR",
	"guardrails-built-in-detector-image": "RELATED_IMAGE_GUARDRAILS_DETECTOR",
	"guardrails-sidecar-gateway-image":   "RELATED_IMAGE_GUARDRAILS_GATEWAY",
	"garak-provider-image":               "RELATED_IMAGE_GARAK_PROVIDER",
	"nemo-guardrails-image":              "RELATED_IMAGE_NEMO_GUARDRAILS",
	"evalhub-provider-guidellm-image":    "RELATED_IMAGE_EVALHUB_GUIDELLM",
	"evalhub-provider-lighteval-image":   "RELATED_IMAGE_EVALHUB_LIGHTEVAL",
	"evalhub-provider-ibm-clear-image":   "RELATED_IMAGE_EVALHUB_IBM_CLEAR",
}

// AllEnvVarNames returns all RELATED_IMAGE_* env var names declared in this
// package. Used by the ModuleHandler in the ODH operator to declare which
// images the module operator needs.
func AllEnvVarNames() []string {
	names := make([]string, 0, len(configMapKeyToEnvVar))
	for _, v := range configMapKeyToEnvVar {
		names = append(names, v)
	}
	return names
}

// Resolve checks the RELATED_IMAGE_* env var for the given ConfigMap key
// first. If set and non-empty, that value is returned without any API call.
// Otherwise it falls back to reading the operator ConfigMap.
func Resolve(ctx context.Context, c client.Client, configMapKey, configMapName, namespace string) (string, error) {
	if img := fromEnv(configMapKey); img != "" {
		log.FromContext(ctx).V(1).Info("image resolved from env var", "key", configMapKey, "image", img)
		return img, nil
	}
	return utils.GetImageFromConfigMap(ctx, c, configMapKey, configMapName, namespace)
}

// ResolveWithFallback is like Resolve but returns fallbackValue when the
// image cannot be found in either the env var or the ConfigMap.
func ResolveWithFallback(ctx context.Context, c client.Client, configMapKey, configMapName, namespace, fallbackValue string) (string, error) {
	if img := fromEnv(configMapKey); img != "" {
		log.FromContext(ctx).V(1).Info("image resolved from env var", "key", configMapKey, "image", img)
		return img, nil
	}
	return utils.GetImageFromConfigMapWithFallback(ctx, c, configMapKey, configMapName, namespace, fallbackValue)
}

// FromEnvByConfigMapKey looks up the RELATED_IMAGE_* env var for a given
// ConfigMap key. Returns empty string if the key has no mapping or the env
// var is not set. Exported for controllers that have their own ConfigMap
// lookup logic (e.g. EvalHub).
func FromEnvByConfigMapKey(configMapKey string) string {
	return fromEnv(configMapKey)
}

// fromEnv looks up the RELATED_IMAGE_* env var for a given ConfigMap key.
func fromEnv(configMapKey string) string {
	envName, ok := configMapKeyToEnvVar[configMapKey]
	if !ok {
		return ""
	}
	return os.Getenv(envName)
}

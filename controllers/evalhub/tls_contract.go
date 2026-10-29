package evalhub

import (
	"fmt"

	evalhubv1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	corev1 "k8s.io/api/core/v1"
)

// Managed API/MCP backends are HTTP. Only their kube-rbac-proxy sidecars serve
// TLS and receive the resolved cluster profile/adherence. Native HTTPS remains
// an upstream deployment option, not an operator CR environment-variable mode.
func validateManagedTLS(instance *evalhubv1.EvalHub) error {
	validate := func(path string, env []corev1.EnvVar) error {
		for _, variable := range env {
			switch variable.Name {
			case "TLS_REQUIRED", "TLS_MIN_VERSION", "TLS_CIPHERS", "TLS_CURVES",
				"TLS_CERT_FILE", "TLS_KEY_FILE", "EVALHUB_TLS_CERT_FILE", "EVALHUB_TLS_KEY_FILE":
				return fmt.Errorf("%s: %s is operator-owned: managed API/MCP backends use loopback HTTP; kube-rbac-proxy owns TLS", path, variable.Name)
			}
		}
		return nil
	}
	if err := validate("spec.env", instance.Spec.Env); err != nil {
		return err
	}
	if instance.Spec.IsMCPEnabled() {
		return validate("spec.mcp.env", instance.Spec.MCP.Env)
	}
	return nil
}

// Explicit values override certificate/required-TLS defaults in qualified images.
// Version/cipher/group controls must be absent, not empty: the upstream contract
// rejects empty policy inputs. Images must not bake in native-TLS policy controls.
func managedHTTPEnv(certVariable, keyVariable string) []corev1.EnvVar {
	return []corev1.EnvVar{{Name: "TLS_REQUIRED", Value: "false"}, {Name: certVariable, Value: ""}, {Name: keyVariable, Value: ""}}
}

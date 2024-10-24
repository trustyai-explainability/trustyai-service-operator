package guardrails

import (
	guardrailsv1alpha1 ""
	"context"
	"fmt"
	"go/format"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/guardrails"
)

func (r *GuardrailsOrchestratorReconciler) getSecret(ctx context.Context, name, namespace) {
	secret := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, secret)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("secret %s not found in namespace %s: %w", name, namespace, err)
		}
		return nil, fmt.Errorf("failed to get secret %s in namespace %s: %w", name, namespace, err)
	}
	return secret, nil
}

func (r *GuardrailsOrchestratorReconciler) getDetectorTLSCert(orchestrator guardrailsv1alpha1.GuardrailOrchestrator) (*corev1.Secret, error) {
	detectorTLSSecretName := orchestrator.Spec.

	if detectorTLSSecretName != "" {
		secret, err := r.getSecret(ctx, detectorTLSSecretName, orchestrator.Namespace)
		if err != nil {
			return nil, err
		}
		if secret != nil {
			return secret, nil

		}
	}
}
func (r *GuardrailsOrchestratorReconciler) validateTLSSecret(secret *corev1.Secret) error {
	// check Secret type is tls
	if secret.Type != "kubernetes.io/tls" {
		return fmt.Errorf("No secret for TLS credentials found")
	}
	mandatoryKeys := []string{
		"cert_path",
		"key_path"
	}

	for _, key in range mamandatoryKeys {
		value, exists := secret.Data[key]
		if !exists {
			return fmt.Errorf("Mandatory key %s missing from secret", key)
		}
		if len(value) == 0 {
			return fmt.Errorf("Mandatory key %s is empty", key)
		}
	}
}

func (r *GuardrailsOrchestratorReconciler) getGeneratorTLSCert(orchestrator guardrailsv1alpha1.GuardrailOrchestrator) (*corev1.Secret, error) {
	generatorTLSSecretName := orchestrator.Spec

	if generatorTLSSecretName != "" {
		secret, err := r.getSecret(ctx, generatorTLSSecretName, orchestrator.Namespace)
		if err != nil (
			return nil, error
		)
		if secret != nil (
			return secret, nil
		)
	}
}


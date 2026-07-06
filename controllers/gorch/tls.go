package gorch

import (
	"context"
	"github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// MountSecret adds a given secret name as a mounted volume to a deployment
func MountSecret(deployment *v1.Deployment, tlsSecretToMount string) {
	utils.MountSecret(deployment, tlsSecretToMount, tlsSecretToMount+"-vol", "/etc/tls/"+tlsSecretToMount, []int{0})
}

// findPredictorServingCertSecret checks if the serving secret for a given inference service exists
func (r *GuardrailsOrchestratorReconciler) findPredictorServingCertSecret(ctx context.Context, namespace string, isvcName string) (*corev1.Secret, error) {
	secretName := isvcName + "-predictor-serving-cert"
	return utils.GetSecret(ctx, r.Client, secretName, namespace)
}

// getTLSInfo scrapes the orchestrator's status for any tls configurations found in previous reconciliations
func getTLSInfo(orchestrator v1alpha1.GuardrailsOrchestrator) []v1alpha1.DetectedService {
	var tlsMounts []v1alpha1.DetectedService
	if orchestrator.Status.AutoConfigState != nil {
		if orchestrator.Status.AutoConfigState.GenerationService.Scheme == "https" {
			tlsMounts = append(tlsMounts, orchestrator.Status.AutoConfigState.GenerationService)
		}

		for i := range orchestrator.Status.AutoConfigState.DetectorServices {
			if orchestrator.Status.AutoConfigState.DetectorServices[i].Scheme == "https" {
				tlsMounts = append(tlsMounts, orchestrator.Status.AutoConfigState.DetectorServices[i])
			}
		}
	}
	return tlsMounts
}

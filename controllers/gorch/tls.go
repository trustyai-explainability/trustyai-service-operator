package gorch

import (
	"context"
	"github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// MountSecret adds a given secret name as a mounted volume to a deployment
func MountSecret(deployment *v1.Deployment, tlsSecretToMount string) {
	volumeName := tlsSecretToMount + "-vol"
	// Add to volumes
	deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName:  tlsSecretToMount,
				DefaultMode: func() *int32 { i := int32(420); return &i }(),
			},
		},
	})
	// Add to volumeMounts for the main container (assume first container)
	if len(deployment.Spec.Template.Spec.Containers) > 0 {
		deployment.Spec.Template.Spec.Containers[0].VolumeMounts = append(
			deployment.Spec.Template.Spec.Containers[0].VolumeMounts,
			corev1.VolumeMount{
				Name:      volumeName,
				MountPath: "/etc/tls/" + tlsSecretToMount,
				ReadOnly:  true,
			},
		)
	}
}

// getSecret returns a secret if it exists, nil otherwise
func getSecret(ctx context.Context, c client.Client, namespace, secretName string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretName}, secret)
	if err != nil {
		return nil, err
	}
	return secret, nil
}

// findPredictorServingCertSecret checks if the serving secret for a given inference service exists
func (r *GuardrailsOrchestratorReconciler) findPredictorServingCertSecret(ctx context.Context, namespace string, isvcName string) (*corev1.Secret, error) {
	secretName := isvcName + "-predictor-serving-cert"
	return getSecret(ctx, r.Client, namespace, secretName)
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

package gorch

import (
	"github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

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

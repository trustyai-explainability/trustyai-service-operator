package controllers

import (
	"context"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	tlsMountPath = "/etc/trustyai/tls"
)

// TLSCertVolumes holds the volume and volume mount for the TLS certificates
type TLSCertVolumes struct {
	volume      corev1.Volume
	volumeMount corev1.VolumeMount
}

// createFor creates the required volumes and volume mount for the TLS certificates for a specific Kubernetes secret
func (cert *TLSCertVolumes) createFor(instance *trustyaiopendatahubiov1alpha1.TrustyAIService) {
	volume := corev1.Volume{
		Name: instance.Name + "-internal",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: instance.Name + "-internal",
			},
		},
	}

	volumeMount := corev1.VolumeMount{
		Name:      instance.Name + "-internal",
		MountPath: tlsMountPath,
		ReadOnly:  true,
	}
	cert.volume = volume
	cert.volumeMount = volumeMount
}

func (r *TrustyAIServiceReconciler) GetCustomCertificatesBundle(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) CustomCertificatesBundle {

	var customCertificatesBundle CustomCertificatesBundle
	// Check for custom certificate bundle config map presence
	labelSelector := client.MatchingLabels{caBundleAnnotation: "true"}
	// Check for the presence of the ConfigMap in the operator's namespace
	configMapNames, err := r.getConfigMapNamesWithLabel(ctx, instance.Namespace, labelSelector)
	caNotFoundMessage := "CA bundle ConfigMap named '" + caBundleName + "' not found. Not using custom CA bundle."
	if err != nil {
		log.FromContext(ctx).Info(caNotFoundMessage)
		customCertificatesBundle.IsDefined = false
	} else {
		found := false
		for _, configMapName := range configMapNames {
			if configMapName == caBundleName {
				found = true
				break
			}
		}
		if found {
			log.FromContext(ctx).Info("Found trusted CA bundle ConfigMap. Using custom CA bundle.")
			customCertificatesBundle.IsDefined = true
			customCertificatesBundle.VolumeName = caBundleName
			customCertificatesBundle.ConfigMapName = caBundleName
		} else {
			log.FromContext(ctx).Info(caNotFoundMessage)
			customCertificatesBundle.IsDefined = false
		}
	}
	return customCertificatesBundle
}

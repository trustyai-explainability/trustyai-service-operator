package guardrails

import (
	"context"

	guardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-orchestrator-operator/api/guardrails/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	tlsMountPath = "/tls/server"
)

type TLSCertVolumes struct {
	volume      corev1.Volume
	volumeMount corev1.VolumeMount
}

func (cert *TLSCertVolumes) createCertVolume() {
	volume := corev1.Volume{
		Name: "server-tls",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: "",
			},
		},
	}
	volumeMount := corev1.VolumeMount{
		Name:      "server-tls",
		MountPath: tlsMountPath,
		ReadOnly:  true,
	}
	cert.volume = volume
	cert.volumeMount = volumeMount
}

func (r *GuardrailsOrchestratorReconciler) getConfigMapNamesWithLabel(ctx context.Context, namespace string, labelSelector client.MatchingLabels) ([]string, error) {
	configMapList := &corev1.ConfigMapList{}

	// List ConfigMaps with the specified label selector
	err := r.Client.List(ctx, configMapList, client.InNamespace(namespace), labelSelector)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, cm := range configMapList.Items {
		names = append(names, cm.Name)
	}

	return names, nil
}

func (r *GuardrailsOrchestratorReconciler) getCustomCertificateBundle(ctx context.Context, orchestrator *guardrailsv1alpha1.GuardrailOrchestrator) CustomCertificatesBundle {
	var customCertificatesBundle CustomCertificatesBundle
	labelSelector := client.MatchingLabels{caBundleAnnotation: "true"}
	configMapNames, err := r.getConfigMapNamesWithLabel(ctx, orchestrator.Namespace, labelSelector)
	caNotFoundMessage := "ConfigMap resource named '" + caBundleName + "' not found. Not using custom CA bundle."

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
			customCertificatesBundle.ConfigMapName = caBundleName
		} else {
			log.FromContext(ctx).Info(caNotFoundMessage)
			customCertificatesBundle.IsDefined = false
		}
	}
	return customCertificatesBundle
}

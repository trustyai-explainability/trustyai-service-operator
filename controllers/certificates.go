package controllers

import (
	"context"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

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

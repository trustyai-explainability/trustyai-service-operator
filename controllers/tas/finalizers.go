package tas

import (
	"context"

	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// deleteExternalDependency removes the payload processor from the ModelMesh deployment
func (r *TrustyAIServiceReconciler) deleteExternalDependency(crName string, instance *trustyaiopendatahubiov1alpha1.TrustyAIService, namespace string, ctx context.Context) error {
	// Call patchEnvVarsByLabelForDeployments with remove set to true
	_, err := r.patchEnvVarsByLabelForDeployments(ctx,
		instance,
		namespace,
		modelMeshLabelKey,
		modelMeshLabelValue,
		payloadProcessorName,
		crName,
		true)
	if err != nil {
		log.FromContext(ctx).Error(err, "Could not remove environment variable from ModelMesh Deployment.")
		// Do not return the error to avoid finalizer loop if the namespace or other resources are deleted
	}

	return nil
}

package controllers

import (
	"context"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// deleteExternalDependency removes the payload processor from the ModelMesh deployment
func (r *TrustyAIServiceReconciler) deleteExternalDependency(crName, namespace string, ctx context.Context) error {
	// Call patchEnvVarsByLabelForDeployments with remove set to true
	_, err := r.patchEnvVarsByLabelForDeployments(ctx, namespace, modelMeshLabelKey, modelMeshLabelValue, payloadProcessorName, crName, true)
	if err != nil {
		log.FromContext(ctx).Error(err, "Could not remove environment variable from ModelMesh Deployment.")
		// Do not return the error to avoid finalizer loop if the namespace or other resources are deleted
	}

	return nil
}

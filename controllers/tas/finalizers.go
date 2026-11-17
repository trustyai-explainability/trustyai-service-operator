package tas

import (
	"context"

	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// deleteExternalDependency removes the payload processor from the ModelMesh deployment
func (r *TrustyAIServiceReconciler) deleteExternalDependency(crName string, instance *trustyaiopendatahubiov1.TrustyAIService, namespace string, ctx context.Context) error {
	log.FromContext(ctx).Info("Deleting external dependencies")
	// The serving state is read here too, so removal sees the same contract the
	// reconcile path uses. A failed read is logged and the removal skipped rather
	// than retried forever: a stuck finalizer would block deletion of the very
	// resource this finalizer exists to clean up after.
	serving := r.inferenceServicesReport(ctx, instance.Namespace)
	// Call handleInferenceServices with remove set to true
	_, err := r.handleInferenceServices(ctx, instance, serving, namespace, modelMeshLabelKey, modelMeshLabelValue, payloadProcessorName, instance.Name, true)
	if err != nil {
		log.FromContext(ctx).Error(err, "Could not remove environment variable from ModelMesh Deployment.")
		// Do not return the error to avoid finalizer loop if the namespace or other resources are deleted
	}

	return nil
}

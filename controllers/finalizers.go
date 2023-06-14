package controllers

import "context"

// deleteExternalDependency removes the payload processor from the ModelMesh deployment
func (r *TrustyAIServiceReconciler) deleteExternalDependency(crName, namespace string, ctx context.Context) error {
	// Remove the payload processor from the deployment
	err := updatePayloadProcessor(context.Background(), r.Client, modelMeshContainer, "PAYLOAD_PROCESSOR", crName, namespace, true)
	if err != nil {
		return err
	}

	return nil
}

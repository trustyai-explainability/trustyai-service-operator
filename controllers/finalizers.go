package controllers

import "context"

// deleteExternalDependency removes the payload processor from the ModelMesh deployment
func (r *TrustyAIServiceReconciler) deleteExternalDependency(crName, namespace string, ctx context.Context) error {
	// Define the label selector for the deployments
	deploymentLabels := map[string]string{
		"app.kubernetes.io/instance": "modelmesh-controller",
		"app.kubernetes.io/name":     "modelmesh-controller",
		"name":                       "modelmesh-serving-mlserver-0.x",
	}

	// Remove the payload processor from the deployment
	err := updatePayloadProcessor(context.Background(), r.Client, deploymentLabels, "mlserver", "PAYLOAD_PROCESSOR", crName, namespace, true)
	if err != nil {
		return err
	}

	return nil
}

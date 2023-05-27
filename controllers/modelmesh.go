package controllers

import (
	"context"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

// updatePayloadProcessor updates the payload processor endpoint for the given container
func updatePayloadProcessor(ctx context.Context, r client.Client, deploymentLabels map[string]string, containerName string, envVarName string, crName string, namespace string, remove bool) error {
	// Define the matching labels
	matchingLabels := client.MatchingLabels(deploymentLabels)

	// Prepare the DeploymentList object
	deploymentList := &appsv1.DeploymentList{}

	// Fetch the Deployment instances
	err := r.List(ctx, deploymentList, matchingLabels)
	if err != nil {
		return err
	}

	// Build the payload processor endpoint
	url := "http://" + crName + "." + namespace + ".svc.cluster.local/consumer/kserve/v2"

	for _, deployment := range deploymentList.Items {
		// Loop over the containers
		for i := range deployment.Spec.Template.Spec.Containers {
			container := &deployment.Spec.Template.Spec.Containers[i]

			// Only update the specified container
			if container.Name == containerName {
				// Get the existing env var
				var envVar *corev1.EnvVar
				for i, e := range container.Env {
					if e.Name == envVarName {
						envVar = &container.Env[i]
						break
					}
				}

				// If the env var doesn't exist, add it (if we are not removing)
				if envVar == nil {
					if !remove {
						container.Env = append(container.Env, corev1.EnvVar{
							Name:  envVarName,
							Value: url,
						})
					}
				} else {
					// Split the existing value into parts
					parts := strings.Split(envVar.Value, " ")

					// Check if the payload processor already exists in the list
					exists := false
					for _, part := range parts {
						if part == url {
							exists = true
							break
						}
					}

					// If we are removing, and the payload processor exists, remove it
					if remove && exists {
						newParts := []string{}
						for _, part := range parts {
							if part != url {
								newParts = append(newParts, part)
							}
						}
						envVar.Value = strings.Join(newParts, " ")
					}

					// If we are not removing, and the URL doesn't exist, add it
					if !remove && !exists {
						envVar.Value += " " + url
					}
				}
			}
		}

		// Update the Deployment
		if err := r.Update(ctx, &deployment); err != nil {
			return err
		}
	}

	return nil
}

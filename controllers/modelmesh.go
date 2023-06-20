package controllers

import (
	"context"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strings"
)

func (r *TrustyAIServiceReconciler) patchEnvVarsForDeployments(ctx context.Context, deployments []appsv1.Deployment, envVarName string, url string, remove bool) (bool, error) {
	// Loop over the Deployments
	for _, deployment := range deployments {

		// Check if all Pods are ready
		if deployment.Status.ReadyReplicas != *deployment.Spec.Replicas {
			// Not all replicas are ready, return an error
			return false, fmt.Errorf("not all ModelMesh serving replicas are ready for deployment %s", deployment.Name)
		}

		// Loop over all containers in the Deployment's Pod template
		for i, _ := range deployment.Spec.Template.Spec.Containers {
			// Get the existing env var
			var envVar *corev1.EnvVar
			for j, e := range deployment.Spec.Template.Spec.Containers[i].Env {
				if e.Name == envVarName {
					envVar = &deployment.Spec.Template.Spec.Containers[i].Env[j]
					break
				}
			}

			// If the env var doesn't exist, add it (if we are not removing)
			if envVar == nil && !remove {
				deployment.Spec.Template.Spec.Containers[i].Env = append(deployment.Spec.Template.Spec.Containers[i].Env, corev1.EnvVar{
					Name:  envVarName,
					Value: url,
				})
			} else if envVar != nil {
				// If the env var exists and already contains the value, don't do anything
				existingValues := strings.Split(envVar.Value, " ")
				for _, v := range existingValues {
					if v == url {
						continue
					}
				}

				// Modify the existing env var based on the remove flag and current value
				envVar.Value = generateEnvVarValue(envVar.Value, url, remove)
			}

			// Update the Deployment
			if err := r.Update(ctx, &deployment); err != nil {
				log.FromContext(ctx).Error(err, "Could not update Deployment", "Deployment", deployment.Name)
				return false, err
			}
			log.FromContext(ctx).Info("Updating Deployment " + deployment.Name + ", container spec " + deployment.Spec.Template.Spec.Containers[i].Name + ", env var " + envVarName + " to " + url)
		}
	}

	return true, nil
}

func (r *TrustyAIServiceReconciler) patchEnvVarsByLabelForDeployments(ctx context.Context, namespace string, labelKey string, labelValue string, envVarName string, crName string, remove bool) (bool, error) {
	// Get all Deployments for the label
	deployments, err := r.GetDeploymentsByLabel(ctx, namespace, labelKey, labelValue)
	if err != nil {
		log.FromContext(ctx).Error(err, "Could not get Deployments by label.")
		return false, err
	}

	// Build the payload processor endpoint
	url := "http://" + crName + "." + namespace + ".svc.cluster.local/consumer/kserve/v2"

	// Patch environment variables for the Deployments
	if shouldContinue, err := r.patchEnvVarsForDeployments(ctx, deployments, envVarName, url, remove); err != nil {
		log.FromContext(ctx).Error(err, "Could not patch environment variables for Deployments.")
		return shouldContinue, err
	}

	return true, nil
}

// generateEnvVarValue Generates the final value of the MM_PAYLOAD_PROCESSOR environment variable based on the remove flag and current value
func generateEnvVarValue(currentValue, newValue string, remove bool) string {
	// Split the current value into parts
	parts := strings.Split(currentValue, " ")

	// Check if the new value already exists in the list
	exists := false
	for _, part := range parts {
		if part == newValue {
			exists = true
			break
		}
	}

	// If we are removing, and the new value exists, remove it
	if remove && exists {
		newParts := []string{}
		for _, part := range parts {
			if part != newValue {
				newParts = append(newParts, part)
			}
		}
		return strings.Join(newParts, " ")
	}

	// If we are not removing, and the new value doesn't exist, add it
	if !remove && !exists {
		return currentValue + " " + newValue
	}

	return currentValue
}

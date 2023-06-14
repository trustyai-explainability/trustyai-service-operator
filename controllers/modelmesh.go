package controllers

import (
	"context"
	kserveapi "github.com/kserve/kserve/pkg/apis/serving/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strings"
)

func (r *TrustyAIServiceReconciler) updatePayloadProcessor(ctx context.Context, containerName string, envVarName string, crName string, namespace string, remove bool) error {
	// Prepare the ServingRuntimeList
	runtimes := &kserveapi.ServingRuntimeList{}

	// Fetch the ServingRuntime instances in the specified namespace
	if err := r.List(ctx, runtimes, client.InNamespace(namespace)); err != nil {
		log.FromContext(ctx).Error(err, "Could not find any ServingRuntimes.")
		return err
	}

	// Build the payload processor endpoint
	url := "http://" + crName + "." + namespace + ".svc.cluster.local/consumer/kserve/v2"

	// Process ServingRuntimes
	for _, runtime := range runtimes.Items {
		err := r.processRuntime(ctx, &runtime, containerName, envVarName, url, remove)
		if err != nil {
			log.FromContext(ctx).Error(err, "Could not patch PAYLOAD_PROCESSOR on ServingRuntimes.")
			return err
		}
	}

	return nil
}

func (r *TrustyAIServiceReconciler) processRuntime(ctx context.Context, runtime *kserveapi.ServingRuntime, containerName string, envVarName string, url string, remove bool) error {

	// Fetch the Deployments owned by the ServingRuntime
	deployments := &appsv1.DeploymentList{}
	if err := r.List(ctx, deployments, client.InNamespace(runtime.Namespace), client.MatchingFields{".metadata.controller": string(runtime.UID)}); err != nil {
		log.FromContext(ctx).Error(err, "Could not list Deployments for ServingRuntime", "ServingRuntime", runtime.Name)
		return err
	}

	// Loop over the Deployments
	for _, deployment := range deployments.Items {
		// Loop over the containers
		for i, container := range deployment.Spec.Template.Spec.Containers {
			// Only update the specified container
			if container.Name == containerName {
				// Get the existing env var
				var envVar *corev1.EnvVar
				for j, e := range container.Env {
					if e.Name == envVarName {
						envVar = &deployment.Spec.Template.Spec.Containers[i].Env[j]
						break
					}
				}

				// If the env var doesn't exist, add it (if we are not removing)
				if envVar == nil {
					if !remove {
						deployment.Spec.Template.Spec.Containers[i].Env = append(container.Env, corev1.EnvVar{
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
			log.FromContext(ctx).Error(err, "Could not update Deployment for ServingRuntime", "ServingRuntime", runtime.Name)
			return err
		}
	}

	return nil
}

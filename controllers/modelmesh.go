package controllers

import (
	"context"
	kserveapi "github.com/kserve/kserve/pkg/apis/serving/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strings"
)

// updatePayloadProcessor updates the payload processor endpoint for the given container
func updatePayloadProcessor(ctx context.Context, r client.Client, containerName string, envVarName string, crName string, namespace string, remove bool) error {
	// Prepare the ServingRuntimeList and ClusterServingRuntimeList objects
	runtimes := &kserveapi.ServingRuntimeList{}

	// Fetch the ServingRuntime and ClusterServingRuntime instances in the specified namespace
	if err := r.List(ctx, runtimes, client.InNamespace(namespace)); err != nil {
		log.FromContext(ctx).Error(err, "Could not find any ServingRuntimes.")
		return err
	}

	// Build the payload processor endpoint
	url := "http://" + crName + "." + namespace + ".svc.cluster.local/consumer/kserve/v2"

	// Process ServingRuntimes
	for _, runtime := range runtimes.Items {
		err := processRuntime(ctx, r, &runtime, containerName, envVarName, url, remove)
		if err != nil {
			log.FromContext(ctx).Error(err, "Could not patch PAYLOAD_PROCESSOR on ServingRuntimes.")
			return err
		}
	}

	return nil
}

func processRuntime(ctx context.Context, r client.Client, runtime *kserveapi.ServingRuntime, containerName string, envVarName string, url string, remove bool) error {

	// Loop over the containers
	for i := range runtime.Spec.Containers {
		container := &runtime.Spec.Containers[i]

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

	// Update the runtime
	if err := r.Update(ctx, runtime); err != nil {
		return err
	}

	return nil
}

package controllers

import (
	"context"
	"fmt"
	kservev1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strings"
)

func (r *TrustyAIServiceReconciler) patchEnvVarsForDeployments(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService, deployments []appsv1.Deployment, envVarName string, url string, remove bool) (bool, error) {
	// Loop over the Deployments
	for _, deployment := range deployments {

		// Check if all Pods are ready
		if deployment.Status.ReadyReplicas != *deployment.Spec.Replicas {
			// Not all replicas are ready, will retry later
			log.FromContext(ctx).Info("Not all replicas are ready for deployment " + deployment.Name + ". Waiting.")
			return false, nil
		}

		// Loop over all containers in the Deployment's Pod template
		for i := range deployment.Spec.Template.Spec.Containers {
			// Store the original environment variable list
			// Get the existing env var
			var envVar *corev1.EnvVar
			for j, e := range deployment.Spec.Template.Spec.Containers[i].Env {
				if e.Name == envVarName {
					envVar = &deployment.Spec.Template.Spec.Containers[i].Env[j]
					break
				}
			}

			var originalValue string
			if envVar != nil {
				originalValue = envVar.Value
			}

			// If the env var doesn't exist, add it (if we are not removing)
			if envVar == nil && !remove {
				envVar = &corev1.EnvVar{
					Name:  envVarName,
					Value: url,
				}
				deployment.Spec.Template.Spec.Containers[i].Env = append(deployment.Spec.Template.Spec.Containers[i].Env, *envVar)
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

			// Only update the deployment if the var value has to change, or we are removing it
			if originalValue != envVar.Value || remove {
				// Update the Deployment
				if err := r.Update(ctx, &deployment); err != nil {
					log.FromContext(ctx).Error(err, "Could not update Deployment", "Deployment", deployment.Name)
					return false, err
				}
				r.eventModelMeshConfigured(instance)
				log.FromContext(ctx).Info("Updating Deployment " + deployment.Name + ", container spec " + deployment.Spec.Template.Spec.Containers[i].Name + ", env var " + envVarName + " to " + url)
			}
		}
	}

	return true, nil
}

func (r *TrustyAIServiceReconciler) patchEnvVarsByLabelForDeployments(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService, namespace string, labelKey string, labelValue string, envVarName string, crName string, remove bool) (bool, error) {
	// Get all Deployments for the label
	deployments, err := r.GetDeploymentsByLabel(ctx, namespace, labelKey, labelValue)
	if err != nil {
		log.FromContext(ctx).Error(err, "Could not get Deployments by label.")
		return false, err
	}

	// Build the payload processor endpoint
	url := generateServiceURL(crName, namespace) + "/consumer/kserve/v2"

	// Patch environment variables for the Deployments
	if shouldContinue, err := r.patchEnvVarsForDeployments(ctx, instance, deployments, envVarName, url, remove); err != nil {
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

func (r *TrustyAIServiceReconciler) handleInferenceServices(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService, namespace string, labelKey string, labelValue string, envVarName string, crName string, remove bool) (bool, error) {
	var inferenceServices kservev1beta1.InferenceServiceList

	if err := r.List(ctx, &inferenceServices, client.InNamespace(namespace)); err != nil {
		log.FromContext(ctx).Error(err, "Could not list InferenceService objects.")
		return false, err
	}

	if len(inferenceServices.Items) == 0 {
		return true, nil
	}

	for _, infService := range inferenceServices.Items {
		annotations := infService.GetAnnotations()
		// Check the annotation "serving.kserve.io/deploymentMode: ModelMesh"
		if val, ok := annotations["serving.kserve.io/deploymentMode"]; ok && val == "ModelMesh" {
			shouldContinue, err := r.patchEnvVarsByLabelForDeployments(ctx, instance, namespace, labelKey, labelValue, envVarName, crName, remove)
			if err != nil {
				log.FromContext(ctx).Error(err, "Could not patch environment variables for ModelMesh deployments.")
				return shouldContinue, err
			}
		} else {
			err := r.patchKServe(ctx, instance, infService, namespace, crName, remove)
			if err != nil {
				log.FromContext(ctx).Error(err, "Could not path InferenceLogger for KServe deployment.")
				return false, err
			}
		}
	}
	return true, nil
}

// patchKServe adds a TrustyAI service as an InferenceLogger to a KServe InferenceService
func (r *TrustyAIServiceReconciler) patchKServe(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService, infService kservev1beta1.InferenceService, namespace string, crName string, remove bool) error {

	url := generateServiceURL(crName, namespace)

	if remove {
		if infService.Spec.Predictor.Logger == nil || *infService.Spec.Predictor.Logger.URL != url {
			return nil // Removing, but InferenceLogger is not set or is not set to the expected URL. Do nothing.
		}

		// Remove the InferenceLogger
		infService.Spec.Predictor.Logger = nil

		// Remove Annotations
		annotations := infService.GetAnnotations()
		delete(annotations, "internal.serving.kserve.io/logger")
		delete(annotations, "internal.serving.kserve.io/logger-mode")
		delete(annotations, "internal.serving.kserve.io/logger-sink-url")
		infService.SetAnnotations(annotations)
	} else {
		if infService.Spec.Predictor.Logger != nil && *infService.Spec.Predictor.Logger.URL == url {
			return nil // InferenceLogger is already set to the expected URL. Do nothing.
		}

		// Construct the logger object
		logger := kservev1beta1.LoggerSpec{
			Mode: "all",
			URL:  &url,
		}

		// Set the InferenceLogger to the InferenceService instance
		infService.Spec.Predictor.Logger = &logger
	}

	// Update the InferenceService
	err := r.Update(ctx, &infService)
	if err == nil {
		r.eventKServeConfigured(instance)
	} else {
		return fmt.Errorf("failed to update InferenceService %s/%s: %v", infService.Namespace, infService.Name, err)
	}
	return nil
}

func (r *TrustyAIServiceReconciler) checkInferenceServicesPresent(ctx context.Context, namespace string) (bool, error) {
	infServiceList := &kservev1beta1.InferenceServiceList{}
	if err := r.List(ctx, infServiceList, client.InNamespace(namespace)); err != nil {
		return false, err
	}

	return len(infServiceList.Items) > 0, nil
}

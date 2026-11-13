package tas

import (
	"context"
	"fmt"
	"strings"

	kservev1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"
	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	DEPLOYMENT_MODE_MODELMESH  = "ModelMesh"
	DEPLOYMENT_MODE_RAW        = "RawDeployment"
	DEPLOYMENT_MODE_STANDARD   = "Standard"
	DEPLOYMENT_MODE_SERVERLESS = "Serverless"
	DEPLOYMENT_MODE_KNATIVE    = "Knative"
)

// GetDeploymentsByLabel returns a list of Deployments that match a label key-value pair
func (r *TrustyAIServiceReconciler) GetDeploymentsByLabel(ctx context.Context, namespace string, labelKey string, labelValue string) ([]appsv1.Deployment, error) {
	// Prepare a DeploymentList object
	deployments := &appsv1.DeploymentList{}

	// Define the selector based on the provided label key-value pair
	selector := labels.Set{labelKey: labelValue}.AsSelector()

	// Fetch the Deployments that match the selector
	if err := r.List(ctx, deployments, client.InNamespace(namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		log.FromContext(ctx).Error(err, "Could not list Deployments by label.")
		return nil, err
	}

	return deployments.Items, nil
}

func (r *TrustyAIServiceReconciler) patchEnvVarsForDeployments(ctx context.Context, instance *trustyaiopendatahubiov1.TrustyAIService, deployments []appsv1.Deployment, envVarName string, url string, remove bool) (bool, error) {
	// Create volume and volume mount for this intance's TLS secrets
	certVolumes := TLSCertVolumes{}
	certVolumes.createFor(instance)

	// Loop over the Deployments
	for _, deployment := range deployments {

		// Check if all Pods are ready
		if deployment.Status.ReadyReplicas != *deployment.Spec.Replicas {
			// Not all replicas are ready, will retry later
			log.FromContext(ctx).Info("Not all replicas are ready for deployment " + deployment.Name + ". Waiting.")
			return false, nil
		}

		// If the secret volume doesn't exist, add it
		volumeExists := false
		for _, vol := range deployment.Spec.Template.Spec.Volumes {
			if vol.Name == instance.Name+"-internal" {
				volumeExists = true
				break
			}
		}
		if !volumeExists {
			deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, certVolumes.volume)
		}

		// Loop over all containers in the Deployment's Pod template
		for i := range deployment.Spec.Template.Spec.Containers {
			mountExists := false
			for _, mount := range deployment.Spec.Template.Spec.Containers[i].VolumeMounts {
				if mount.Name == instance.Name+"-internal" {
					mountExists = true
					break
				}
			}
			if !mountExists {
				deployment.Spec.Template.Spec.Containers[i].VolumeMounts = append(deployment.Spec.Template.Spec.Containers[i].VolumeMounts, certVolumes.volumeMount)
			}

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
				valueExists := false
				for _, v := range existingValues {
					if v == url {
						valueExists = true
						break
					}
				}

				if !valueExists {
					envVar.Value = generateEnvVarValue(envVar.Value, url, remove)
				}
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

			// Check TLS environment variable on ModelMesh
			if deployment.Spec.Template.Spec.Containers[i].Name == mmContainerName {
				tlsKeyCertPathEnvValue := tlsMountPath + "/tls.crt"
				tlsKeyCertPathExists := false
				for _, envVar := range deployment.Spec.Template.Spec.Containers[i].Env {
					if envVar.Name == tlsKeyCertPathName {
						tlsKeyCertPathExists = true
						break
					}
				}

				// Doesn't exist, so we can add
				if !tlsKeyCertPathExists {
					deployment.Spec.Template.Spec.Containers[i].Env = append(deployment.Spec.Template.Spec.Containers[i].Env, corev1.EnvVar{
						Name:  tlsKeyCertPathName,
						Value: tlsKeyCertPathEnvValue,
					})

					if err := r.Update(ctx, &deployment); err != nil {
						log.FromContext(ctx).Error(err, "Could not update Deployment", "Deployment", deployment.Name)
						return false, err
					}
					log.FromContext(ctx).Info("Added environment variable " + tlsKeyCertPathName + " to deployment " + deployment.Name + " for container " + mmContainerName)
				}
			}
		}
	}

	return true, nil
}

func (r *TrustyAIServiceReconciler) patchEnvVarsByLabelForDeployments(ctx context.Context, instance *trustyaiopendatahubiov1.TrustyAIService, namespace string, labelKey string, labelValue string, envVarName string, crName string, remove bool) (bool, error) {
	// Get all Deployments for the label
	deployments, err := r.GetDeploymentsByLabel(ctx, namespace, labelKey, labelValue)
	if err != nil {
		log.FromContext(ctx).Error(err, "Could not get Deployments by label.")
		return false, err
	}

	// Build the payload processor endpoint
	url := utils.GenerateTLSServiceURL(crName, namespace) + "/consumer/kserve/v2"

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

func (r *TrustyAIServiceReconciler) handleInferenceServices(ctx context.Context, instance *trustyaiopendatahubiov1.TrustyAIService, namespace string, labelKey, labelValue, envVarName, crName string, remove bool) (bool, error) {
	contract, err := r.servingContractOf(ctx)
	if err != nil {
		log.FromContext(ctx).Error(err, "Error reading the InferenceService CRD")
		return false, err
	}
	if contract.state != servingStateUsable {
		log.FromContext(ctx).Info("Serving API not usable, skipping InferenceService handling. Will retry on next reconcile.",
			"state", contract.state, "reason", contract.servingReason(), "message", contract.unusableMessage())
		return true, nil
	}

	var inferenceServices kservev1beta1.InferenceServiceList

	if err := r.uncachedReader().List(ctx, &inferenceServices, client.InNamespace(namespace)); err != nil {
		log.FromContext(ctx).Error(err, "Could not list InferenceService objects.")
		return false, err
	}

	kServeServerlessEnabled, err := r.getKServeServerlessConfig(ctx)
	if err != nil {
		log.FromContext(ctx).Error(err, "Could not read KServeServerless configuration. Defaulting to disabled")
		kServeServerlessEnabled = false
	}

	if len(inferenceServices.Items) == 0 {
		return true, nil
	}

	for _, infService := range inferenceServices.Items {
		annotations := infService.GetAnnotations()
		deploymentMode := annotations["serving.kserve.io/deploymentMode"]

		switch deploymentMode {
		case DEPLOYMENT_MODE_MODELMESH:
			// Handle ModelMesh deployments
			shouldContinue, err := r.patchEnvVarsByLabelForDeployments(ctx, instance, namespace, labelKey, labelValue, envVarName, crName, remove)
			if err != nil {
				log.FromContext(ctx).Error(err, "Could not patch environment variables for ModelMesh deployments")
				return shouldContinue, err
			}
			continue

		case DEPLOYMENT_MODE_RAW, DEPLOYMENT_MODE_STANDARD:
			// Handle KServe Raw/Standard deployments (KServe v0.17+ renamed RawDeployment to Standard)
			err := r.patchKServe(ctx, instance, infService, namespace, crName, remove, true)
			if err != nil {
				log.FromContext(ctx).Error(err, "Could not patch InferenceLogger for KServe Raw deployment")
				return false, err
			}
			continue

		case DEPLOYMENT_MODE_SERVERLESS, DEPLOYMENT_MODE_KNATIVE:
			// Handle KServe Serverless/Knative deployments (KServe v0.17+ renamed Serverless to Knative)
			if kServeServerlessEnabled {
				err := r.patchKServe(ctx, instance, infService, namespace, crName, remove, false)
				if err != nil {
					log.FromContext(ctx).Error(err, "Could not patch InferenceLogger for KServe Serverless deployment")
					return false, err
				}
			}
			continue

		default:
			log.FromContext(ctx).Info("Unknown deployment mode, skipping InferenceService", "deploymentMode", deploymentMode, "inferenceService", infService.Name)
			continue
		}
	}
	return true, nil
}

// patchKServe adds a TrustyAI service as an InferenceLogger to a KServe InferenceService
func (r *TrustyAIServiceReconciler) patchKServe(ctx context.Context, instance *trustyaiopendatahubiov1.TrustyAIService, infService kservev1beta1.InferenceService, namespace string, crName string, remove bool, useHTTPS bool) error {

	var url string
	if useHTTPS {
		url = utils.GenerateHTTPSKServeLoggerURL(crName, namespace)
	} else {
		url = utils.GenerateKServeLoggerURL(crName, namespace)
	}

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

	// Only if the Istio sidecar annotation is set
	annotations := infService.GetAnnotations()
	if inject, exists := annotations["sidecar.istio.io/inject"]; exists && inject == "true" {

		// Check if DestinationRule CRD is present. If there's an error, don't proceed and return the error
		exists, err := r.isDestinationRuleCRDPresent(ctx)
		if err != nil {
			log.FromContext(ctx).Error(err, "Error verifying DestinationRule CRD is present")
			return err
		}

		// Try to create the DestinationRule, since CRD exists
		if exists {
			err := r.ensureDestinationRule(ctx, instance)
			if err != nil {
				return fmt.Errorf("failed to ensure DestinationRule: %v", err)
			}
		} else {
			// DestinationRule CRD does not exist. Do not attempt to create it and log error
			err := fmt.Errorf("the DestinationRule CRD is not present in this cluster")
			log.FromContext(ctx).Error(err, "InferenceService has service mesh annotation but DestinationRule CRD not found")
		}

		// Check if VirtualService CRD is present. If there's an error, don't proceed and return the error
		exists, err = r.isVirtualServiceCRDPresent(ctx)
		if err != nil {
			log.FromContext(ctx).Error(err, "Error verifying VirtualService CRD is present")
			return err
		}

		// Try to create the VirtualService, since CRD exists
		if exists {
			err := r.ensureVirtualService(ctx, instance)
			if err != nil {
				return fmt.Errorf("failed to ensure VirtualService: %v", err)
			}
		} else {
			// VirtualService CRD does not exist. Do not attempt to create it and log error
			err := fmt.Errorf("the VirtualService CRD is not present in this cluster")
			log.FromContext(ctx).Error(err, "InferenceService has service mesh annotation but VirtualService CRD not found")
		}

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

// inferenceServicesReport is the serving state as reported through the
// InferenceServicesPresent condition, with the reason that distinguishes the
// three ways serving can be unusable.
type inferenceServicesReport struct {
	contract  servingContract
	namespace string
	// present is true only when the serving API is usable and the namespace
	// holds at least one InferenceService.
	present bool
}

// reason is the condition reason for this state. Serving usable but idle has its
// own reason, so "no InferenceService yet" is distinguishable from "no serving".
func (rep inferenceServicesReport) reason() string {
	if rep.contract.state != servingStateUsable {
		return rep.contract.servingReason()
	}
	if rep.present {
		return StatusReasonInferenceServicesFound
	}
	return StatusReasonNoInferenceServices
}

// message names the group and version that was adopted whenever one was, which
// is what makes version adoption observable from outside the process.
func (rep inferenceServicesReport) message() string {
	if rep.contract.state != servingStateUsable {
		return rep.contract.unusableMessage()
	}
	if rep.present {
		return fmt.Sprintf("InferenceServices found (serving API %s)", rep.contract.groupVersion())
	}
	return fmt.Sprintf("no InferenceService in namespace %s (serving API %s); payload capture activates when one is created",
		rep.namespace, rep.contract.groupVersion())
}

// inferenceServicesReport resolves the serving contract and, when serving is
// usable, whether the namespace holds an InferenceService.
//
// A CRD read failure or a list failure while the CRD is present is transient and
// returned as an error so the reconcile requeues; both are distinct from an
// absent API, which is a state.
func (r *TrustyAIServiceReconciler) inferenceServicesReport(ctx context.Context, namespace string) (inferenceServicesReport, error) {
	contract, err := r.servingContractOf(ctx)
	if err != nil {
		return inferenceServicesReport{}, err
	}

	report := inferenceServicesReport{contract: contract, namespace: namespace}
	if contract.state != servingStateUsable {
		return report, nil
	}

	infServiceList := &kservev1beta1.InferenceServiceList{}
	if err := r.uncachedReader().List(ctx, infServiceList, client.InNamespace(namespace)); err != nil {
		return inferenceServicesReport{}, fmt.Errorf("could not list InferenceService objects: %w", err)
	}

	report.present = len(infServiceList.Items) > 0
	return report, nil
}

package gorch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/kserve/kserve/pkg/apis/serving/v1alpha1"
	kservev1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"
	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"net/url"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	generationType = "generation"
	detectorType   = "detector"

	// === orchestrator config constants ========
	orchestratorConfigPreamble = `chat_generation:
  service:
    hostname: %s
    port: %s
detectors:
`
	orchestratorBuiltInConfig = `  %s:
    type: text_contents
    service:
      hostname: 127.0.0.1
      port: 8080
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
`
	orchestratorExternalDetector = `  %s:
    type: text_contents
    service:
      hostname: %q
      port: %s
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
`

	// === gateway config constants ========
	gatewayPreamable = `orchestrator:
  host: "localhost"
  port: 8032
detectors:
`
	gatewayExternalDetector = `  - name: %s
    input: true
    output: true
    detector_params: {}
`
	gatewayBuiltInDetector = `  - name: %s
    input: true
    output: true
    detector_params:
      regex:
        - $^
`
	gatewayRoutesPreamble = `routes:
  - name: all
    detectors:
`
	gatewayPassthrough = `  - name: passthrough
    detectors:
`
)

// === UTILS ===========================================================================================================
func getOrchestratorConfigMap(orchestrator *gorchv1alpha1.GuardrailsOrchestrator) *string {
	if orchestrator.Spec.OrchestratorConfig != nil {
		return orchestrator.Spec.OrchestratorConfig
	} else if orchestrator.Status.AutoConfigState != nil && orchestrator.Status.AutoConfigState.GeneratedConfigMap != nil {
		return orchestrator.Status.AutoConfigState.GeneratedConfigMap
	} else {
		return nil
	}
}

func getGatewayConfigMap(orchestrator *gorchv1alpha1.GuardrailsOrchestrator) *string {
	if orchestrator.Spec.SidecarGatewayConfig != nil {
		return orchestrator.Spec.SidecarGatewayConfig
	} else if orchestrator.Status.AutoConfigState != nil && orchestrator.Status.AutoConfigState.GeneratedGatewayConfigMap != nil {
		return orchestrator.Status.AutoConfigState.GeneratedGatewayConfigMap
	} else {
		return nil
	}
}

// === ORCHESTRATOR CONFIGURATION ======================================================================================
// defineOrchestratorConfigMap defines ConfigMap *data* for the orchestrator according to the ISVCs+SRs in the namespace
func (r *GuardrailsOrchestratorReconciler) defineOrchestratorConfigMap(
	ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) (*corev1.ConfigMap, *gorchv1alpha1.DetectedService, []gorchv1alpha1.DetectedService, error) {
	log := ctrl.Log.WithName("AutoConfigurator | Orchestrator ConfigMap Definer |")
	orchestratorName := orchestrator.Name
	namespace := orchestrator.Namespace
	generationService := orchestrator.Spec.AutoConfig.InferenceServiceToGuardrail
	useBuiltInDetectors := orchestrator.Spec.EnableBuiltInDetectors
	matchLabel := orchestrator.Spec.AutoConfig.DetectorServiceLabelToMatch

	if matchLabel == "" {
		matchLabel = "trustyai/guardrails"
	}

	configMapName := orchestratorName + "-auto-config"

	log.Info("Starting automatic orchestrator configmap definition",
		"orchestratorName", orchestratorName,
		"namespace", namespace,
		"generationService", generationService,
		"useBuiltInDetectors", useBuiltInDetectors,
	)

	// get generation model
	var allInferenceServices kservev1beta1.InferenceServiceList
	err := r.List(ctx, &allInferenceServices, &client.ListOptions{
		Namespace: namespace,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("could not list all InferenceServices in namespace %s: %w", namespace, err)
	}

	// grab matching serving runtimes
	var servingRuntimes v1alpha1.ServingRuntimeList
	err = r.List(ctx, &servingRuntimes, &client.ListOptions{
		Namespace: namespace,
	})
	if err != nil || len(servingRuntimes.Items) == 0 {
		return nil, nil, nil, fmt.Errorf("could not automatically find serving runtimes: %w", err)
	}

	var detectedGenerationService gorchv1alpha1.DetectedService
	// Check if generationService is a fully qualified URL with port
	if strings.Contains(generationService, "://") && strings.Contains(generationService, ":") {
		// e.g. http://host:port or https://host:port
		generationURL, err := url.Parse(generationService)
		if err == nil {
			detectedGenerationService = gorchv1alpha1.DetectedService{Name: generationService, Hostname: generationURL.Hostname(), Port: generationURL.Port(), Type: generationType}
			log.Info("Generation service resolved from fully qualified URL", "generationService", generationService)
		} else {
			return nil, nil, nil, fmt.Errorf("could not parse host and port from fully qualified generationService: %q", generationService)
		}
	} else {
		for _, isvc := range allInferenceServices.Items {
			if isvc.Name == generationService && isvc.Status.URL != nil {

				var matchingGenerationRuntime *v1alpha1.ServingRuntime
				for _, servingRuntime := range servingRuntimes.Items {
					if servingRuntime.Name == *isvc.Spec.Predictor.Model.Runtime {
						matchingGenerationRuntime = &servingRuntime
						break
					}
				}
				if matchingGenerationRuntime == nil {
					log.Error(nil, "could not find ServingRuntime for generation model", "generator", isvc.Name, "runtime", *isvc.Spec.Predictor.Model.Runtime)
					continue
				}

				genPort := strconv.Itoa(int(matchingGenerationRuntime.Spec.Containers[0].Ports[0].ContainerPort))
				generationURL, err := url.Parse(isvc.Status.URL.String())
				if err != nil {
					return nil, nil, nil, fmt.Errorf("could not parse URL %s from generator service status %s", isvc.Status.URL.String(), isvc.Name)
				}
				detectedGenerationService = gorchv1alpha1.DetectedService{Name: isvc.Name, Hostname: generationURL.Hostname(), Port: genPort, Type: generationType}
				break
			}
		}
	}
	if detectedGenerationService.Hostname == "" || detectedGenerationService.Port == "" {
		return nil, nil, nil, fmt.Errorf("could not find InferenceService with name %q in namespace %s", generationService, namespace)
	} else {
		log.Info("Generation service resolved", "Generation service", detectedGenerationService)
	}

	// get detectors
	var detectorInferenceServices kservev1beta1.InferenceServiceList
	err = r.List(ctx, &detectorInferenceServices, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{matchLabel: "true"}),
		Namespace:     namespace,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("could not automatically find detector inference services: %w", err)
	}

	if len(detectorInferenceServices.Items) == 0 && orchestrator.Spec.EnableBuiltInDetectors == false {
		return nil, nil, nil, fmt.Errorf("no detector inference services found and no built-in detectors specified")
	}

	var detectedDetectorServices []gorchv1alpha1.DetectedService
	// sort the inference services by name, to ensure consistent configmap generation
	sortedDetectorItems := detectorInferenceServices.Items
	sort.Slice(sortedDetectorItems, func(i, j int) bool {
		return sortedDetectorItems[i].Name < sortedDetectorItems[j].Name
	})

	for _, isvc := range sortedDetectorItems {
		var matchingRuntime *v1alpha1.ServingRuntime
		for _, servingRuntime := range servingRuntimes.Items {
			if servingRuntime.Name == *isvc.Spec.Predictor.Model.Runtime {
				matchingRuntime = &servingRuntime
				break
			}
		}
		if matchingRuntime == nil {
			log.Error(nil, "could not find ServingRuntime for detector", "detector", isvc.Name, "runtime", *isvc.Spec.Predictor.Model.Runtime)
			continue
		}

		if len(matchingRuntime.Spec.Containers) == 0 || len(matchingRuntime.Spec.Containers[0].Ports) == 0 {
			log.Error(nil, "servingRuntime is misconfigured and does not have containers or ports, skipping.", "detector", isvc.Name, "runtime", matchingRuntime.Name)
			continue
		}

		httpPort := strconv.Itoa(int(matchingRuntime.Spec.Containers[0].Ports[0].ContainerPort))
		if isvc.Status.URL != nil {
			detectorURL, err := url.Parse(isvc.Status.URL.String())
			if err != nil {
				return nil, nil, nil, fmt.Errorf("could not parse URL %s from detector service status %s", isvc.Status.URL.String(), isvc.Name)
			}
			// for now, ignore the listed port in the inference service
			// real_port = append(ports, host_and_port[1])

			detectedDetectorServices = append(detectedDetectorServices, gorchv1alpha1.DetectedService{Name: isvc.Name, Hostname: detectorURL.Hostname(), Port: httpPort, Type: detectorType})

		}
	}

	log.Info("Detector services resolved", "detectedDetectorServices", detectedDetectorServices)

	// Build detectors YAML string
	var configYaml strings.Builder
	configYaml.WriteString(fmt.Sprintf(orchestratorConfigPreamble, detectedGenerationService.Hostname, detectedGenerationService.Port))
	for i := range detectedDetectorServices {
		configYaml.WriteString(fmt.Sprintf(orchestratorExternalDetector, detectedDetectorServices[i].Name, detectedDetectorServices[i].Hostname, detectedDetectorServices[i].Port))
	}

	if useBuiltInDetectors {
		configYaml.WriteString(fmt.Sprintf(orchestratorBuiltInConfig, builtInDetectorName))
	}

	log.Info("Defined orchestrator config.yaml", "configYaml", configYaml.String())

	cm := &corev1.ConfigMap{
		ObjectMeta: ctrl.ObjectMeta{
			Name:      configMapName,
			Namespace: namespace,
		},
		Data: map[string]string{
			"config.yaml": configYaml.String(),
		},
	}

	// Set owner reference so the ConfigMap is garbage collected with the CR
	if err := controllerutil.SetControllerReference(orchestrator, cm, r.Scheme); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to set owner reference: %w", err)
	}
	return cm, &detectedGenerationService, detectedDetectorServices, nil
}

// applyOrchestratorConfigMap creates/patches an orchestrator-config ConfigMap in the namespace
func (r *GuardrailsOrchestratorReconciler) applyOrchestratorConfigMap(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator, cm *corev1.ConfigMap) error {
	log := ctrl.Log.WithName("AutoConfigurator | Orchestrator ConfigMap Applicator |")

	// Check if the configmap already exists and is up-to-date
	existingCM := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, existingCM)
	if err == nil {
		// ConfigMap exists, check if data is the same
		if existingCM.Data["config.yaml"] == cm.Data["config.yaml"] {
			//pass
		} else {
			existingCM.Data = cm.Data
			if updateErr := r.Update(ctx, existingCM); updateErr != nil {
				log.Error(updateErr, "Failed to update existing orchestrator configmap")
				return updateErr
			}
			log.Info("Updated existing OrchestratorConfig ConfigMap with new configuration")
		}
	} else if errors.IsNotFound(err) {
		// ConfigMap does not exist, create it
		if createErr := r.Create(ctx, cm); createErr != nil {
			log.Error(createErr, "Failed to create orchestrator configmap")
			return createErr
		}
		log.Info("Automatically generated an orchestrator ConfigMap from resources in namespace")
	} else {
		log.Error(err, "Failed to get orchestrator configmap")
		return err
	}

	return nil
}

func (r *GuardrailsOrchestratorReconciler) updateStatusWithOrchestratorConfigInfo(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator, configMap corev1.ConfigMap, generationService gorchv1alpha1.DetectedService, detectorServices []gorchv1alpha1.DetectedService) error {
	orchestrator.Status.Conditions = []gorchv1alpha1.Condition{
		{
			Type:    "Available",
			Status:  "True",
			Reason:  "AutoConfigured",
			Message: "Orchestrator is running with auto-generated configuration",
			// You may want to set LastTransitionTime here as well
		},
	}

	numDetectors := len(detectorServices)
	if orchestrator.Spec.EnableBuiltInDetectors {
		numDetectors += 1
	}
	orchestrator.Status.AutoConfigState = &gorchv1alpha1.AutoConfigState{
		GeneratedConfigMap: &configMap.Name,
		LastGenerated:      configMap.CreationTimestamp.Format(time.RFC3339),
		DetectorServices:   detectorServices,
		GenerationService:  generationService,
		Status:             "Completed",
		Message:            fmt.Sprintf("Generated AutoConfig with 1 generation services and %d detector services", numDetectors),
	}
	return r.Status().Update(ctx, orchestrator)
}

// === GATEWAY CONFIGURATION ===========================================================================================
// defineGatewayConfigMap defines default configmap *data* for the guardrails gateway
func (r *GuardrailsOrchestratorReconciler) defineGatewayConfigMap(
	orchestrator *gorchv1alpha1.GuardrailsOrchestrator, detectorServices []gorchv1alpha1.DetectedService) (*corev1.ConfigMap, error) {
	log := ctrl.Log.WithName("AutoConfigurator | Gateway Config Definer |")

	var configYaml strings.Builder
	configYaml.WriteString(gatewayPreamable)

	for i := range detectorServices {
		configYaml.WriteString(fmt.Sprintf(gatewayExternalDetector, detectorServices[i].Name))
	}
	if orchestrator.Spec.EnableBuiltInDetectors {
		configYaml.WriteString(fmt.Sprintf(gatewayBuiltInDetector, builtInDetectorName))
	}

	configYaml.WriteString(gatewayRoutesPreamble)
	for i := range detectorServices {
		configYaml.WriteString(fmt.Sprintf("      - %s\n", detectorServices[i].Name))
	}
	if orchestrator.Spec.EnableBuiltInDetectors {
		configYaml.WriteString(fmt.Sprintf("      - %s\n", builtInDetectorName))
	}
	configYaml.WriteString(gatewayPassthrough)

	log.Info("Defined gateway config.yaml", "configYaml", configYaml.String())

	configMapName := orchestratorName + "-gateway-auto-config"
	cm := &corev1.ConfigMap{
		ObjectMeta: ctrl.ObjectMeta{
			Name:      configMapName,
			Namespace: orchestrator.Namespace,
		},
		Data: map[string]string{
			"config.yaml": configYaml.String(),
		},
	}

	if err := controllerutil.SetControllerReference(orchestrator, cm, r.Scheme); err != nil {
		return nil, fmt.Errorf("failed to set owner reference for gateway configmap: %w", err)
	}

	return cm, nil
}

// applyGatewayConfigMap creates/patches a guardrails-gateway-config configmap in the namespace
func (r *GuardrailsOrchestratorReconciler) applyGatewayConfigMap(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator, cm *corev1.ConfigMap) error {
	log := ctrl.Log.WithName("AutoConfigurator | Gateway ConfigMap Applicator |")

	// Check if the configmap already exists and is up-to-date
	existingCM := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, existingCM)
	if err == nil {
		// ConfigMap exists, check if data is the same
		if existingCM.Data["config.yaml"] == cm.Data["config.yaml"] {
			//pass
		} else {
			// if not, add "outdated" label
			if existingCM.Labels == nil {
				existingCM.Labels = map[string]string{}
			}
			existingCM.Labels["trustyai/has-diverged-from-auto-config"] = "true"
			if updateErr := r.Update(ctx, existingCM); updateErr != nil {
				log.Error(updateErr, "Failed to label existing orchestrator configmap as outdated")
				return updateErr
			}
		}
	}
	if errors.IsNotFound(err) {
		// ConfigMap does not exist, create it
		if createErr := r.Create(ctx, cm); createErr != nil {
			log.Error(createErr, "Failed to create orchestrator configmap")
			return createErr
		}
		log.Info("Automatically generated a default GatewayConfig from resources in namespace")
	}

	// update status
	orchestrator.Status.AutoConfigState.GeneratedGatewayConfigMap = &cm.Name
	return r.Status().Update(ctx, orchestrator)
}

// ===== Reconciliation Logic ==========================================================================================
// shouldRegenerateAutoConfig tracks whether the conditions in the namespace have changed, warranting another AutoConfig generation
// i.e., if the ISVCs or SRs have changed
func (r *GuardrailsOrchestratorReconciler) shouldRegenerateAutoConfig(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) (bool, error) {
	namespace := orchestrator.Namespace
	matchLabel := orchestrator.Spec.AutoConfig.DetectorServiceLabelToMatch
	if matchLabel == "" {
		matchLabel = "trustyai/guardrails"
	}

	// check if configmaps have been deleted
	// Check if orchestrator configmap exists
	if getOrchestratorConfigMap(orchestrator) != nil {
		cm := &corev1.ConfigMap{}
		err := r.Get(ctx, types.NamespacedName{Name: *getOrchestratorConfigMap(orchestrator), Namespace: namespace}, cm)
		if err != nil {
			if errors.IsNotFound(err) {
				return true, nil // ConfigMap deleted, trigger regeneration
			}
			return false, err
		}
	}

	// Check if gateway configmap exists
	if orchestrator.Spec.EnableGuardrailsGateway && getGatewayConfigMap(orchestrator) != nil {
		cm := &corev1.ConfigMap{}
		err := r.Get(ctx, types.NamespacedName{Name: *getGatewayConfigMap(orchestrator), Namespace: namespace}, cm)
		if err != nil {
			if errors.IsNotFound(err) {
				return true, nil // ConfigMap deleted, trigger regeneration
			}
			return false, err
		}
	}

	// check if ISVCs/ SRs have changed
	// Get all relevant InferenceServices
	var detectorInferenceServices kservev1beta1.InferenceServiceList
	if err := r.List(ctx, &detectorInferenceServices, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{matchLabel: "true"}),
		Namespace:     namespace,
	}); err != nil {
		return false, err
	}

	// Get all relevant ServingRuntimes
	var servingRuntimes v1alpha1.ServingRuntimeList
	if err := r.List(ctx, &servingRuntimes, &client.ListOptions{
		Namespace: namespace,
	}); err != nil {
		return false, err
	}

	// Compute a hash of the names and resourceVersions of the relevant resources
	var isvcKeys []string
	for _, isvc := range detectorInferenceServices.Items {
		isvcKeys = append(isvcKeys, isvc.Name+":"+isvc.ResourceVersion)
	}
	sort.Strings(isvcKeys)

	var srKeys []string
	for _, sr := range servingRuntimes.Items {
		srKeys = append(srKeys, sr.Name+":"+sr.ResourceVersion)
	}
	sort.Strings(srKeys)

	combined := strings.Join(isvcKeys, ",") + "|" + strings.Join(srKeys, ",")
	hash := sha256.Sum256([]byte(combined))
	hashStr := hex.EncodeToString(hash[:])

	// Store the hash as an annotation on the orchestrator CR
	if orchestrator.Annotations == nil {
		orchestrator.Annotations = map[string]string{}
	}
	const annotationKey = "trustyai.opendatahub.io/autoconfig-resource-hash"
	prevHash := orchestrator.Annotations[annotationKey]
	if prevHash == hashStr {
		return false, nil // No change
	}

	// Patch the annotation with the new hash
	patch := client.MergeFrom(orchestrator.DeepCopy())
	orchestrator.Annotations[annotationKey] = hashStr
	if err := r.Patch(ctx, orchestrator, patch); err != nil {
		return false, err
	}
	return true, nil
}

// runAutoConfig performs the definition and application of orchestrator and gateway configmaps
func (r *GuardrailsOrchestratorReconciler) runAutoConfig(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) error {
	// Generate orchestrator configmap spec
	log := ctrl.Log.WithName("AutoConfigurator |")
	log.Info("Starting AutoConfigurator")

	// make sure we're using an up-to-date instance of the orchestrator
	orchestrator, err := r.refreshOrchestrator(ctx, orchestrator, log)
	if err != nil {
		return err
	}

	cm, generationService, detectorServices, err := r.defineOrchestratorConfigMap(ctx, orchestrator)
	if err != nil {
		log.Error(err, "Failed to automatically generate orchestrator configmap")
		return err
	}

	if err := r.applyOrchestratorConfigMap(ctx, orchestrator, cm); err != nil {
		log.Error(err, "Failed to automatically create/update orchestrator configmap")
		return err
	} else {
		err := r.updateStatusWithOrchestratorConfigInfo(ctx, orchestrator, *cm, *generationService, detectorServices)
		if err != nil {
			log.Error(err, "Failed to automatically generate orchestrator configmap")
			return err
		}
	}

	if orchestrator.Spec.EnableGuardrailsGateway {
		gatewayCM, err := r.defineGatewayConfigMap(orchestrator, detectorServices)
		if err != nil {
			log.Error(err, "Failed to automatically generate gateway configmap")
			return err
		}

		if err = r.applyGatewayConfigMap(ctx, orchestrator, gatewayCM); err != nil {
			log.Error(err, "Failed to automatically create/update gateway configmap")
			return err
		}
	}
	log.Info("AutoConfigurator Finished")
	return nil
}

// redeployOnConfigMapChange redeploys the orchestrator deployment if either the gateway-config or orchestrator-config configmaps change
func (r *GuardrailsOrchestratorReconciler) redeployOnConfigMapChange(
	ctx context.Context,
	log logr.Logger,
	orchestrator *gorchv1alpha1.GuardrailsOrchestrator) (ctrl.Result, error) {

	existingConfigMap := &corev1.ConfigMap{}
	if err := r.Get(ctx, types.NamespacedName{Name: *getOrchestratorConfigMap(orchestrator), Namespace: orchestrator.Namespace}, existingConfigMap); err != nil {
		log.Error(err, "Failed to get ConfigMap", "ConfigMap.Name", configMapName, "ConfigMap.Namespace", orchestrator.Namespace)
		return ctrl.Result{}, err
	}

	var existingGatewayConfigMap *corev1.ConfigMap
	if getGatewayConfigMap(orchestrator) != nil {
		existingGatewayConfigMap = &corev1.ConfigMap{}
		err := r.Get(ctx, types.NamespacedName{Name: *getGatewayConfigMap(orchestrator), Namespace: orchestrator.Namespace}, existingGatewayConfigMap)
		if err != nil {
			log.Error(err, "Failed to get ConfigMap", "ConfigMap.Name", configMapName, "ConfigMap.Namespace", orchestrator.Namespace)
			return ctrl.Result{}, err
		}
	}

	existingDeployment := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: orchestrator.Name, Namespace: orchestrator.Namespace}, existingDeployment); err == nil {
		annotations := existingDeployment.Spec.Template.Annotations
		if annotations == nil {
			annotations = map[string]string{}
		}
		changedConfigs := r.setConfigMapHashAnnotations(ctx, orchestrator, annotations)

		if len(changedConfigs) > 0 {
			// refetch deployment
			err = r.Get(ctx, types.NamespacedName{Name: orchestrator.Name, Namespace: orchestrator.Namespace}, existingDeployment)
			if err != nil {
				log.Error(err, "Could not fetch up-to-date deployment for patching")
			}

			existingDeployment.Spec.Template.Annotations = annotations
			if updateErr := r.Update(ctx, existingDeployment); updateErr != nil {
				log.Error(updateErr, "Failed to redeploy orchestrator after ConfigMap change")
				return ctrl.Result{}, updateErr
			}

			log.Info("Redeployed orchestrator deployment due to changes in " + strings.Join(changedConfigs, ", "))
		}

	} else if errors.IsNotFound(err) {
		log.Info("Deployment not found, will be created in subsequent reconciliation")
	} else {
		log.Error(err, "Failed to get orchestrator deployment for redeploy after ConfigMap change", "ConfigMap.Name", configMapName)
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// setConfigMapHashAnnotations applies hashes of the gateway-config and orchestrator-config to the orchestrator deployment, and returns
// a list of any configmaps that have changed.
func (r *GuardrailsOrchestratorReconciler) setConfigMapHashAnnotations(
	ctx context.Context,
	orchestrator *gorchv1alpha1.GuardrailsOrchestrator,
	annotations map[string]string,
) []string {
	var changedConfigs []string

	// Orchestrator config hash
	if getOrchestratorConfigMap(orchestrator) != nil {
		cm := &corev1.ConfigMap{}
		if err := r.Get(ctx, types.NamespacedName{Name: *getOrchestratorConfigMap(orchestrator), Namespace: orchestrator.Namespace}, cm); err == nil {
			configData := cm.Data["config.yaml"]
			hash := fmt.Sprintf("%x", sha256.Sum256([]byte(configData)))
			if annotations["trustyai.opendatahub.io/orchestrator-config-hash"] != hash {
				annotations["trustyai.opendatahub.io/orchestrator-config-hash"] = hash
				changedConfigs = append(changedConfigs, "Orchestrator ConfigMap")
			}
		}
	}

	// Gateway config hash
	if orchestrator.Spec.EnableGuardrailsGateway && getGatewayConfigMap(orchestrator) != nil {
		cm := &corev1.ConfigMap{}
		if err := r.Get(ctx, types.NamespacedName{Name: *getGatewayConfigMap(orchestrator), Namespace: orchestrator.Namespace}, cm); err == nil {
			configData := cm.Data["config.yaml"]
			hash := fmt.Sprintf("%x", sha256.Sum256([]byte(configData)))
			if annotations["trustyai.opendatahub.io/orchestrator-gateway-config-hash"] != hash {
				annotations["trustyai.opendatahub.io/orchestrator-gateway-config-hash"] = hash
				changedConfigs = append(changedConfigs, "Gateway ConfigMap")
			}
		}
	}
	return changedConfigs
}

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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sort"
	"strconv"
	"strings"
)

// === ORCHESTRATOR CONFIGURATION ======================================================================================
// defineOrchestratorConfigMap defines ConfigMap *data* for the orchestrator according to the ISVCs+SRs in the namespace
func (r *GuardrailsOrchestratorReconciler) defineOrchestratorConfigMap(
	ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) (*corev1.ConfigMap, []string, error) {
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
		return nil, nil, fmt.Errorf("Could not list all InferenceServices: %w", err)
	}

	// grab matching serving runtimes
	var servingRuntimes v1alpha1.ServingRuntimeList
	err = r.List(ctx, &servingRuntimes, &client.ListOptions{
		Namespace: namespace,
	})
	if err != nil || len(servingRuntimes.Items) == 0 {
		return nil, nil, fmt.Errorf("could not automatically find serving runtimes: %w", err)
	}

	var genHost, genPort string
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
			genPort = strconv.Itoa(int(matchingGenerationRuntime.Spec.Containers[0].Ports[0].ContainerPort))

			split := strings.Split(isvc.Status.URL.String(), "//")[1]
			if strings.Contains(split, ":") {
				host_and_port := strings.Split(split, ":")
				genHost = host_and_port[0]
			} else {
				genHost = split
			}
			break
		}
	}
	log.Info("Generation service resolved", "genHost", genHost, "genPort", genPort)
	if genHost == "" || genPort == "" {
		return nil, nil, fmt.Errorf("could not find InferenceService with name %q in namespace %q", generationService, namespace)
	}

	// get label-to-match

	// get detectors
	var detectorInferenceServices kservev1beta1.InferenceServiceList
	err = r.List(ctx, &detectorInferenceServices, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{matchLabel: "true"}),
		Namespace:     namespace,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("could not automatically find detector inference services: %w", err)
	}

	if len(detectorInferenceServices.Items) == 0 && orchestrator.Spec.EnableBuiltInDetectors == false {
		return nil, nil, fmt.Errorf("no detector inference services found and no built-in detectors specified")
	}

	// Extract status.address.uri from each InferenceService
	var hosts []string
	var ports []string
	var names []string

	// sort the inference services by name, to ensure consistent configmap generation
	sortedDetectorItems := detectorInferenceServices.Items
	sort.Slice(sortedDetectorItems, func(i, j int) bool {
		return sortedDetectorItems[i].Name < sortedDetectorItems[j].Name
	})

	for _, isvc := range sortedDetectorItems {
		names = append(names, isvc.Name)

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
		ports = append(ports, httpPort)
		if isvc.Status.URL != nil {
			split := strings.Split(isvc.Status.URL.String(), "//")[1]

			var detectorHost string
			if strings.Contains(split, ":") {
				host_and_port := strings.Split(split, ":")
				detectorHost = host_and_port[0]
			} else {
				detectorHost = split
			}
			hosts = append(hosts, detectorHost)

			// for now, ignore the listed port in the inference service
			// real_port = append(ports, host_and_port[1])
		}
	}

	log.Info("Detector services resolved", "names", names, "hosts", hosts, "ports", ports)

	// Build detectors YAML string
	var configYaml strings.Builder
	configYaml.WriteString("chat_generation:\n")
	configYaml.WriteString("  service:\n")
	configYaml.WriteString(fmt.Sprintf("    hostname: %s\n", genHost))
	configYaml.WriteString(fmt.Sprintf("    port: %s\n", genPort))

	configYaml.WriteString("detectors:\n")
	for i := range names {
		configYaml.WriteString(fmt.Sprintf("  %s:\n", names[i]))
		configYaml.WriteString("    type: text_contents\n")
		configYaml.WriteString("    service:\n")
		configYaml.WriteString(fmt.Sprintf("      hostname: %q\n", hosts[i]))
		configYaml.WriteString(fmt.Sprintf("      port: %s\n", ports[i]))
		configYaml.WriteString("    chunker_id: whole_doc_chunker\n")
		configYaml.WriteString("    default_threshold: 0.5\n")
	}

	if useBuiltInDetectors {
		configYaml.WriteString("  regex:\n")
		configYaml.WriteString("    type: text_contents\n")
		configYaml.WriteString("    service:\n")
		configYaml.WriteString("      hostname: 127.0.0.1\n")
		configYaml.WriteString("      port: 8080\n")
		configYaml.WriteString("    chunker_id: whole_doc_chunker\n")
		configYaml.WriteString("    default_threshold: 0.5\n")
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
		return nil, nil, fmt.Errorf("failed to set owner reference: %w", err)
	}
	return cm, names, nil
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

	// Set orchestrator.Spec.OrchestratorConfig to use the automatically generated config
	latestOrchestrator := &gorchv1alpha1.GuardrailsOrchestrator{}
	if err := r.Get(ctx, types.NamespacedName{Name: orchestrator.Name, Namespace: orchestrator.Namespace}, latestOrchestrator); err != nil {
		log.Error(err, "Failed to re-fetch Orchestrator before patching")
		return err
	}
	// Patch only the OrchestratorConfig field in the spec
	patch := client.MergeFrom(latestOrchestrator.DeepCopy())
	latestOrchestrator.Spec.OrchestratorConfig = &cm.Name
	if err := r.Patch(ctx, latestOrchestrator, patch); err != nil {
		log.Error(err, "Failed to patch OrchestratorConfig in CR")
		return err
	}

	orchestrator.Spec.OrchestratorConfig = &cm.Name
	return nil
}

// === GATEWAY CONFIGURATION ===========================================================================================
// defineGatewayConfigMap defines default configmap *data* for the guardrails gateway
func (r *GuardrailsOrchestratorReconciler) defineGatewayConfigMap(
	orchestrator *gorchv1alpha1.GuardrailsOrchestrator, names []string) (*corev1.ConfigMap, error) {
	log := ctrl.Log.WithName("AutoConfigurator | Gateway Config Definer |")

	var configYaml strings.Builder
	configYaml.WriteString("orchestrator:\n")
	configYaml.WriteString("  host: \"localhost\"\n")
	configYaml.WriteString("  port: 8032\n")
	configYaml.WriteString("detectors:\n")

	for i := range names {
		configYaml.WriteString(fmt.Sprintf("  - name: %s\n", names[i]))
		configYaml.WriteString("    input: true\n")
		configYaml.WriteString("    output: true\n")
		configYaml.WriteString("    detector_params: {}\n")
	}
	if orchestrator.Spec.EnableBuiltInDetectors {
		configYaml.WriteString("  - name: regex\n")
		configYaml.WriteString("    input: true\n")
		configYaml.WriteString("    output: true\n")
		configYaml.WriteString("    detector_params:\n")
		configYaml.WriteString("      regex:\n")
		configYaml.WriteString("        - $^\n")
	}

	configYaml.WriteString("routes:\n")
	configYaml.WriteString("  - name: all\n")
	configYaml.WriteString("    detectors:\n")
	for i := range names {
		configYaml.WriteString(fmt.Sprintf("      - %s\n", names[i]))
	}
	if orchestrator.Spec.EnableBuiltInDetectors {
		configYaml.WriteString("      - regex\n")
	}
	configYaml.WriteString("  - name: passthrough\n")
	configYaml.WriteString("    detectors:\n")

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

	// Set orchestrator.Spec.OrchestratorConfig to use the automatically generated config
	latestOrchestrator := &gorchv1alpha1.GuardrailsOrchestrator{}
	if err := r.Get(ctx, types.NamespacedName{Name: orchestrator.Name, Namespace: orchestrator.Namespace}, latestOrchestrator); err != nil {
		log.Error(err, "Failed to re-fetch Orchestrator before patching")
		return err
	}

	// Patch only the OrchestratorConfig field in the spec
	patch := client.MergeFrom(latestOrchestrator.DeepCopy())
	latestOrchestrator.Spec.SidecarGatewayConfig = &cm.Name
	if err := r.Patch(ctx, latestOrchestrator, patch); err != nil {
		log.Error(err, "Failed to patch GatewayConfig in CR")
		return err
	}
	orchestrator.Spec.SidecarGatewayConfig = &cm.Name
	return nil
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
	if orchestrator.Spec.OrchestratorConfig != nil {
		cm := &corev1.ConfigMap{}
		err := r.Get(ctx, types.NamespacedName{Name: *orchestrator.Spec.OrchestratorConfig, Namespace: namespace}, cm)
		if err != nil {
			if errors.IsNotFound(err) {
				return true, nil // ConfigMap deleted, trigger regeneration
			}
			return false, err
		}
	}

	// Check if gateway configmap exists
	if orchestrator.Spec.EnableGuardrailsGateway && orchestrator.Spec.SidecarGatewayConfig != nil {
		cm := &corev1.ConfigMap{}
		err := r.Get(ctx, types.NamespacedName{Name: *orchestrator.Spec.SidecarGatewayConfig, Namespace: namespace}, cm)
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

	cm, isvcNames, err := r.defineOrchestratorConfigMap(ctx, orchestrator)
	if err != nil {
		log.Error(err, "Failed to automatically generate orchestrator configmap")
		return err
	}

	if err := r.applyOrchestratorConfigMap(ctx, orchestrator, cm); err != nil {
		log.Error(err, "Failed to automatically create/update orchestrator configmap")
		return err
	}

	if orchestrator.Spec.EnableGuardrailsGateway {
		gatewayCM, err := r.defineGatewayConfigMap(orchestrator, isvcNames)
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
	if err := r.Get(ctx, types.NamespacedName{Name: *orchestrator.Spec.OrchestratorConfig, Namespace: orchestrator.Namespace}, existingConfigMap); err != nil {
		log.Error(err, "Failed to get ConfigMap", "ConfigMap.Name", configMapName, "ConfigMap.Namespace", orchestrator.Namespace)
		return ctrl.Result{}, err
	}

	var existingGatewayConfigMap *corev1.ConfigMap
	if orchestrator.Spec.SidecarGatewayConfig != nil {
		existingGatewayConfigMap = &corev1.ConfigMap{}
		err := r.Get(ctx, types.NamespacedName{Name: *orchestrator.Spec.SidecarGatewayConfig, Namespace: orchestrator.Namespace}, existingGatewayConfigMap)
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
	if orchestrator.Spec.OrchestratorConfig != nil {
		cm := &corev1.ConfigMap{}
		if err := r.Get(ctx, types.NamespacedName{Name: *orchestrator.Spec.OrchestratorConfig, Namespace: orchestrator.Namespace}, cm); err == nil {
			configData := cm.Data["config.yaml"]
			hash := fmt.Sprintf("%x", sha256.Sum256([]byte(configData)))
			if annotations["trustyai.opendatahub.io/orchestrator-config-hash"] != hash {
				annotations["trustyai.opendatahub.io/orchestrator-config-hash"] = hash
				changedConfigs = append(changedConfigs, "Orchestrator ConfigMap")
			}
		}
	}

	// Gateway config hash
	if orchestrator.Spec.EnableGuardrailsGateway && orchestrator.Spec.SidecarGatewayConfig != nil {
		cm := &corev1.ConfigMap{}
		if err := r.Get(ctx, types.NamespacedName{Name: *orchestrator.Spec.SidecarGatewayConfig, Namespace: orchestrator.Namespace}, cm); err == nil {
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

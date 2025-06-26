package gorch

import (
	"context"
	"fmt"
	"github.com/kserve/kserve/pkg/apis/serving/v1alpha1"
	kservev1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"strconv"
	"strings"
)

// GenerateOrchestratorConfigMap creates a new ConfigMap for the orchestrator with default or generated data.
func (r *GuardrailsOrchestratorReconciler) GenerateOrchestratorConfigMap(
	ctx context.Context, orchestratorName, namespace string, owner client.Object, generationService string, useBuiltInDetectors bool) (*corev1.ConfigMap, error) {
	log := ctrl.Log.WithName("GenerateOrchestratorConfigMap")
	configMapName := orchestratorName + "-auto-config"

	log.Info("Starting automatic orchestratorConfig generation",
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
		return nil, fmt.Errorf("Could not list all InferenceServices: %w", err)
	}
	var genHost, genPort string
	for _, isvc := range allInferenceServices.Items {
		if isvc.Name == generationService && isvc.Status.URL != nil {
			split := strings.Split(isvc.Status.URL.String(), "//")[1]
			host_and_port := strings.Split(split, ":")
			genHost = host_and_port[0]
			genPort = "8080"
			break
		}
	}
	log.Info("Generation service resolved", "genHost", genHost, "genPort", genPort)
	if genHost == "" || genPort == "" {
		return nil, fmt.Errorf("could not find InferenceService with name %q in namespace %q", generationService, namespace)
	}

	// get detectors
	var detectorInferenceServices kservev1beta1.InferenceServiceList
	err = r.List(ctx, &detectorInferenceServices, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{"trustyai/guardrails": "true"}),
		Namespace:     namespace,
	})
	if err != nil || len(detectorInferenceServices.Items) == 0 {
		return nil, fmt.Errorf("could not automatically find detector inference services: %w", err)
	}

	// grab matching serving runtimes
	var servingRuntimes v1alpha1.ServingRuntimeList
	err = r.List(ctx, &servingRuntimes, &client.ListOptions{
		Namespace: namespace,
	})
	if err != nil || len(servingRuntimes.Items) == 0 {
		return nil, fmt.Errorf("could not automatically find detector serving runtimes: %w", err)
	}

	// Extract status.address.uri from each InferenceService
	var hosts []string
	var ports []string
	var names []string
	for _, isvc := range detectorInferenceServices.Items {
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

		httpPort := strconv.Itoa(int(matchingRuntime.Spec.Containers[0].Ports[0].ContainerPort))
		ports = append(ports, httpPort)
		if isvc.Status.URL != nil {
			split := strings.Split(isvc.Status.URL.String(), "//")[1]

			host_and_port := strings.Split(split, ":")
			hosts = append(hosts, host_and_port[0])

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

	log.Info("Generated config.yaml", "configYaml", configYaml.String())

	//
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
	if err := controllerutil.SetControllerReference(owner, cm, r.Scheme); err != nil {
		return nil, fmt.Errorf("failed to set owner reference: %w", err)
	}
	log.Info("Successfully created ConfigMap object", "configMapName", configMapName)
	return cm, nil
}

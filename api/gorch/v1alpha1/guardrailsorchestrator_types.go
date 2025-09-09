/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type AutoConfig struct {
	// The name of the inference service that provides the vLLM generation model to-be-guardrailed
	InferenceServiceToGuardrail string `json:"inferenceServiceToGuardrail"`

	/* Label key to use when automatically identifying guardrail detector inference services.
	If provided, all inference services with the label `$detectorServiceLabelToMatch: true` will be used as a guardrails detector.
	If not provided, the default match label is `trustyai/guardrails`, and the autoconfig will use all inference services with the label `trustyai/guardrails: true` as detectors.
	*/
	// +optional
	DetectorServiceLabelToMatch string `json:"detectorServiceLabelToMatch,omitempty"`
}

// GuardrailsOrchestratorSpec defines the desired state of GuardrailsOrchestrator.
type GuardrailsOrchestratorSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// Number of replicas
	Replicas int32 `json:"replicas"`
	// Name of configmap containing generator,detector,and chunker arguments
	// +optional
	OrchestratorConfig *string `json:"orchestratorConfig,omitempty"`
	// Settings governing the automatic configuration of the orchestrator. Replaces `OrchestratorConfig`.
	// +optional
	AutoConfig *AutoConfig `json:"autoConfig,omitempty"`
	// Boolean flag to enable/disable built-in detectors
	// +optional
	EnableBuiltInDetectors bool `json:"enableBuiltInDetectors,omitempty"`
	// Boolean flag to enable/disable the guardrails sidecar gateway
	// +optional
	EnableGuardrailsGateway bool `json:"enableGuardrailsGateway,omitempty"`
	//  Name of the configmap containing guadrails sidecar gateway arguments
	// +optional
	SidecarGatewayConfig *string `json:"guardrailsGatewayConfig,omitempty"`
	// List of orchestrator enviroment variables for configuring the OTLP exporter
	// +optional
	OtelExporter OtelExporter `json:"otelExporter,omitempty"`
	// Set log level in the orchestrator deployment
	// +optional
	LogLevel *string `json:"logLevel,omitempty"`
}

// OtelExporter defines the environment variables for configuring the OTLP exporter.
type OtelExporter struct {
	// Sets the protocol for all the OTLP endpoints
	// +optional
	Protocol string `json:"protocol,omitempty"`
	// Overrides the protocol for traces
	// +optional
	TracesProtocol string `json:"tracesProtocol,omitempty"`
	// Overrides the protocol for traces
	// +optional
	MetricsProtocol string `json:"metricsProtocol,omitempty"`
	// Sets the OTLP endpoint
	// +optional
	OTLPEndpoint string `json:"otlpEndpoint,omitempty"`
	// Overrides the OTLP endpoint for metrics
	// +optional
	MetricsEndpoint string `json:"metricsEndpoint,omitempty"`
	// Overrides the OTLP endpoint for traces
	// +optional
	TracesEndpoint string `json:"tracesEndpoint,omitempty"`
	// Specifies which data types to export
	// +optional
	OTLPExport string `json:"otlpExport,omitempty"`
}

type ConditionType string

type Condition struct {
	Type ConditionType `json:"type" description:"type of condition ie. Available|Progressing|Degraded."`

	Status corev1.ConditionStatus `json:"status" description:"status of the condition, one of True, False, Unknown"`

	// +optional
	Reason string `json:"reason,omitempty" description:"one-word CamelCase reason for the condition's last transition"`

	// +optional
	Message string `json:"message,omitempty" description:"human-readable message indicating details about last transition"`

	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime" description:"last time the condition transit from one status to another"`
}

type DetectedService struct {
	Name      string `json:"name,omitempty"`
	Type      string `json:"type,omitempty"`   // e.g. "generator" or "detector"
	Scheme    string `json:"scheme,omitempty"` //e.g., "http" or "https"
	Hostname  string `json:"hostname,omitempty"`
	Port      string `json:"port,omitempty"`
	TLSSecret string `json:"tlsSecret,omitempty"`
}

type AutoConfigState struct {
	GeneratedConfigMap        *string           `json:"generatedConfigMap,omitempty"`
	GeneratedGatewayConfigMap *string           `json:"generatedGatewayConfigMap,omitempty"`
	LastGenerated             string            `json:"lastGenerated,omitempty"`
	GenerationService         DetectedService   `json:"generationService,omitempty"`
	DetectorServices          []DetectedService `json:"detectorServices,omitempty"`
	ConfigurationHash         string            `json:"configurationHash,omitempty"`
	Status                    string            `json:"status,omitempty"`
	Message                   string            `json:"message,omitempty"`
}

type GuardrailsOrchestratorStatus struct {
	Phase string `json:"phase,omitempty"`
	// Conditions describes the state of the GuardrailsOrchestrator resource.
	// +optional
	Conditions []Condition `json:"conditions,omitempty"`
	// AutoConfigState describes information about the generated autoconfiguration
	// +optional
	AutoConfigState *AutoConfigState `json:"autoConfigState,omitempty"`
}

// +kubebuilder:object:root=true

// GuardrailsOrchestrator is the Schema for the guardrailsorchestrators API.
// +kubebuilder:subresource:status
type GuardrailsOrchestrator struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GuardrailsOrchestratorSpec   `json:"spec,omitempty"`
	Status GuardrailsOrchestratorStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GuardrailsOrchestratorList contains a list of GuardrailsOrchestrator.
type GuardrailsOrchestratorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GuardrailsOrchestrator `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GuardrailsOrchestrator{}, &GuardrailsOrchestratorList{})
}

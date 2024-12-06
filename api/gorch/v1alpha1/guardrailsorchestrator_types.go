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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type TLSSpec struct {
	Type       string `json:"type"`
	CertPath   string `json:"cert_path"`
	KeyPath    string `json:"key_path"`
	CACertPath string `json:"ca_cert_path"`
}

type ServiceSpec struct {
	Hostname string  `json:"hostname"`
	Port     int     `json:"port"`
	TLS      TLSSpec `json:"tls,omitempty"`
}

type GeneratorSpec struct {
	Provider string      `json:"provider"`
	Service  ServiceSpec `json:"service"`
}

type ChunkerSpec struct {
	Provider string      `json:"provider"`
	Service  ServiceSpec `json:"service"`
}

type DetectorSpec struct {
	Type             string      `json:"type"`
	Service          ServiceSpec `json:"service"`
	ChunkerName      string      `json:"chunkerName"`
	DefaultThreshold string      `json:"defaultThreshold"`
}

// GuardrailsOrchestratorSpec defines the desired state of GuardrailsOrchestrator.
type GuardrailsOrchestratorSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Number of replicas
	Replicas int32 `json:"replicas"`
	// Generator configuration
	Generator GeneratorSpec `json:"generator"`
	// Chunker configuration
	Chunkers []ChunkerSpec `json:"chunkers,omitempty"`
	// Detector configuration
	Detectors []DetectorSpec `json:"detectors"`
	// TLS configuration
	TLS string `json:"tls,omitempty"`
}

// const (
// 	OrchestratorConditionReconciled ConditionType = "Reconciled"
// 	// OrchestratorConditionReady represents the fact that the orchestrator's components are ready
// 	OrchestratorConditionReady           ConditionType   = "Ready"
// 	OrchestratorDeploymentNotReady       ConditionReason = "DeploymentNotReady"
// 	OrchestratorInferenceServiceNotReady ConditionReason = "InferenceServiceNotReady"
// 	OrchestratorRouteNotAdmitted         ConditionReason = "RouteNotAdmitted"
// 	OrchestratorHealthy                  ConditionReason = "Healthy"
// 	OrchestratorReadinessCheckFailed     ConditionReason = "ReadinessCheckFailed"
// )

type GuardrailsOrchestratorStatus struct {
	Conditions []metav1.Condition `json:"conditions"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// GuardrailsOrchestrator is the Schema for the guardrailsorchestrators API.
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

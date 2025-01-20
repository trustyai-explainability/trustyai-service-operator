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
	ChunkerName string      `json:"chunkerName"`
	Provider    string      `json:"provider"`
	Service     ServiceSpec `json:"service"`
}

type DetectorSpec struct {
	Name             string      `json:"name"`
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
	// CongigMapName string `json:"configMapName"`
	// Generator configuration
	Generator GeneratorSpec `json:"generator"`
	// Chunker configuration
	Chunkers []ChunkerSpec `json:"chunkers,omitempty"`
	// Detector configuration
	Detectors []DetectorSpec `json:"detectors"`
	// TLS configuration
	TLS string `json:"tls,omitempty"`
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

type GuardrailsOrchestratorStatus struct {
	Phase string `json:"phase,omitempty"`

	// Conditions describes the state of the GuardrailsOrchestrator resource.
	// +optional
	Conditions []Condition `json:"conditions,omitempty"`
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
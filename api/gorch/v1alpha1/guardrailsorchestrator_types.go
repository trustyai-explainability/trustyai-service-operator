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
	"time"

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
	// Generator configuration
	Generator GeneratorSpec `json:"generator"`
	// Chunker configuration
	Chunkers []ChunkerSpec `json:"chunkers,omitempty"`
	// Detector configuration
	Detectors []DetectorSpec `json:"detectors"`
	// TLS configuration
	TLS string `json:"tls,omitempty"`
}

type GuardrailsOrchestratorStatus struct {
	Conditions []GuardrailsOrchestratorCondition `json:"conditions,omitempty"`
}

var testTime *time.Time

func (in *GuardrailsOrchestratorStatus) SetConditions(condition GuardrailsOrchestratorCondition) {
	var now time.Time
	if testTime == nil {
		now = time.Now()
	} else {
		now = *testTime
	}

	lastTransitionTime := metav1.NewTime(now.Truncate(time.Second))

	for i, prevCondition := range in.Conditions {
		if prevCondition.Type == condition.Type {
			if prevCondition.Status != condition.Status {
				condition.LastTransitionTime = lastTransitionTime
			} else {
				condition.LastTransitionTime = prevCondition.LastTransitionTime
			}
			in.Conditions[i] = condition
			return
		}
	}
	condition.LastTransitionTime = lastTransitionTime
	in.Conditions = append(in.Conditions, condition)
}

type GuardrailsOrchestratorCondition struct {
	Type   string                 `json:"type"`
	Status metav1.ConditionStatus `json:"status"`
	// +optional
	Reason string `json:"reason,omitempty"`
	// +optional
	Message string `json:"message,omitempty"`
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

// +kubebuilder:object:root=true

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

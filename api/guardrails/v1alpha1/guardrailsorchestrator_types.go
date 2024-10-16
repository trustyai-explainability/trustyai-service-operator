/*
Copyright 2024.

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

// GuardrailsOrchestrator is the Schema for the GuardrailsOrchestrator API
type GuardrailsOrchestrator struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GuardrailsOrchestratorSpec   `json:"spec,omitempty"`
	Status GuardrailsOrchestratorStatus `json:"status,omitempty"`
}

type ServiceSpec struct {
	Hostname string `json:"hostname"`
	Port     string `json:"port"`
}

type GenerationSpec struct {
	Provider string `json:"provider,omitempty"`
	Service  ServiceSpec
}

type ChunkersSpec struct {
	Type    string      `json:"type"`
	Service ServiceSpec `json:"service"`
}

type DetectorsSpec struct {
	Type             string      `json:"type"`
	Service          ServiceSpec `json:"service"`
	ChunkerID        string      `json:"chunker_id"`
	DefaultThreshold float32     `json:"default_threshold"`
}

type EnvSecret struct {
	// Environment's name
	Env string `json:"env"`
	// The secret is from a secret object
	// +optional
	SecretRef *corev1.SecretKeySelector `json:"secretRef,omitempty"`
	// The secret is from a plain text
	// +optional
	Secret *string `json:"secret,omitempty"`
}

type FileSecret struct {
	// The secret object
	SecretRef corev1.SecretVolumeSource `json:"secretRef,omitempty"`
	// The path to mount the secret
	MountPath string `json:"mountPath"`
}

// GuardrailsOrchestratorSpec defines the desired state of GuardrailsOrchestrator
type GuardrailsOrchestratorSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Generator name
	// + Optional
	Generator string `json:"generation"`
	// Chunker name
	// + Optional
	Chunker string `json:"chunkers"`
	// Detector name
	Detectors string `json:"detector"`
	// Number of replicas
	Replicas   int32       `json:"replicas"`
	EnvSecrets []EnvSecret `json:"envSecrets,omitempty"`
	// Use secrets as files
	FileSecrets []FileSecret `json:"fileSecrets,omitempty"`
}

// GuardrailsOrchestratorStatus defines the observed state of GuardrailsOrchestrator
type GuardrailsOrchestratorStatus struct {
	// Define your status fields here
	Phase      string                 `json:"phase"`
	Replicas   int32                  `json:"replicas"`
	Conditions []Condition            `json:"conditions"`
	Ready      corev1.ConditionStatus `json:"ready,omitempty"`
}

// Condition represents possible conditions of a GuardrailsOrchestratorStatus
type Condition struct {
	Type               string                 `json:"type"`
	Status             corev1.ConditionStatus `json:"status"`
	LastTransitionTime metav1.Time            `json:"lastTransitionTime"`
	Reason             string                 `json:"reason"`
	Message            string                 `json:"message"`
}

// +kubebuilder:object:root=true

// GuardrailsOrchestratorList contains a list of GuardrailsOrchestrator
type GuardrailsOrchestratorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GuardrailsOrchestrator `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GuardrailsOrchestrator{}, &GuardrailsOrchestratorList{})
}

func (g *GuardrailsOrchestrator) SetStatus(condType, reason, message string, status corev1.ConditionStatus) {
	now := metav1.Now()
	condition := Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
	}
	// Replace or append condition
	found := false
	for i, cond := range g.Status.Conditions {
		if cond.Type == condType {
			g.Status.Conditions[i] = condition
			found = true
			break
		}
	}
	if !found {
		g.Status.Conditions = append(g.Status.Conditions, condition)
	}
}

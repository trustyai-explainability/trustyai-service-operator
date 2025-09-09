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
	"github.com/trustyai-explainability/trustyai-service-operator/api/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// NemoGuardrailsSpec defines the desired state of NemoGuardrails
type NemoGuardrailsSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// NemoConfig should be the name of the configmap containing the NeMO server configuration
	NemoConfig string `json:"nemoConfig,omitempty"`
}

// NemoGuardrailStatus defines the observed state of NemoGuardrails
type NemoGuardrailStatus struct {
	Phase string `json:"phase,omitempty"`

	// Conditions describes the state of the NemoGuardrails resource.
	// +optional
	Conditions []common.Condition `json:"conditions,omitempty"`
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// NemoGuardrails is the Schema for the nemoguardrails API
type NemoGuardrails struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NemoGuardrailsSpec  `json:"spec,omitempty"`
	Status NemoGuardrailStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// NemoGuardrailsList contains a list of NemoGuardrails
type NemoGuardrailsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NemoGuardrails `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NemoGuardrails{}, &NemoGuardrailsList{})
}

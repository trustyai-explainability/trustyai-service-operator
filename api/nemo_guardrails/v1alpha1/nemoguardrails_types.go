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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// NemoConfig holds information related to a single configuration of the NeMo Guardrails server
type NemoConfig struct {
	//Name sets the id of this particular config within the NeMo Guardrails server. This will create a directory called /app/config/$Name. Since it $Name will be used a directory, it must only contain alphanumeric characters, dashes, and underscores.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9_-]+$`
	Name string `json:"name,omitempty"`
	//ConfigMaps is a list of configmaps that comprise the configuration. All files from these configmaps will be mounted within /app/config/$Name
	ConfigMaps []string `json:"configMaps,omitempty"`
	//The Default flag determines whether config is treated as the default config for the nemo-server. If no config is set to default, the first entry in NemoConfigs will be used as the default
	Default bool `json:"default,omitempty"`
}

// NemoGuardrailsSpec defines the desired state of NemoGuardrails
type NemoGuardrailsSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// NemoConfig should be the names of the configmaps containing NeMO server configuration files. All files in NemoConfigs will be mounted to /app/config/$Name
	NemoConfigs    []NemoConfig           `json:"nemoConfigs"`
	CABundleConfig *common.CABundleConfig `json:"caBundleConfig,omitempty"`
	// Define Env information for the main container
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`
}

type CAStatus struct {
	ODHTrustedCAFound       bool   `json:"odhTrustedCAFound"`
	ODHTrustedCAError       string `json:"odhTrustedCAError,omitempty"`
	OpenshiftServingCAFound bool   `json:"openshiftServingCAFound"`
	OpenshiftServingCAError string `json:"openshiftServingCAError,omitempty"`
	UserCAFound             bool   `json:"userCAFound,omitempty"`
	UserCAError             string `json:"userCAError,omitempty"`
}

// NemoGuardrailStatus defines the observed state of NemoGuardrails
type NemoGuardrailStatus struct {
	Phase string `json:"phase,omitempty"`

	// Conditions describes the state of the NemoGuardrails resource.
	// +optional
	Conditions []common.Condition `json:"conditions,omitempty"`
	// CA describes the status of the CA configmaps
	// +optional
	CA *CAStatus `json:"ca,omitempty"`
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

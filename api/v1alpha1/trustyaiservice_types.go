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

// TrustyAIService is the Schema for the trustyaiservices API
// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
type TrustyAIService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TrustyAIServiceSpec   `json:"spec,omitempty"`
	Status TrustyAIServiceStatus `json:"status,omitempty"`
}

type StorageSpec struct {
	Format string `json:"format"`
	Folder string `json:"folder"`
	Size   string `json:"size"`
}

type DataSpec struct {
	Filename string `json:"filename"`
	Format   string `json:"format"`
}

type MetricsSpec struct {
	Schedule  string `json:"schedule"`
	BatchSize *int   `json:"batchSize,omitempty"`
}

// TrustyAIServiceSpec defines the desired state of TrustyAIService
type TrustyAIServiceSpec struct {
	// Number of replicas
	// +optional
	Replicas *int32      `json:"replicas"`
	Storage  StorageSpec `json:"storage"`
	Data     DataSpec    `json:"data"`
	Metrics  MetricsSpec `json:"metrics"`
}

// TrustyAIServiceStatus defines the observed state of TrustyAIService
type TrustyAIServiceStatus struct {
	// Define your status fields here
	Phase      string                 `json:"phase"`
	Replicas   int32                  `json:"replicas"`
	Conditions []Condition            `json:"conditions"`
	Ready      corev1.ConditionStatus `json:"ready,omitempty"`
}

// Condition represents possible conditions of a TrustyAIServiceStatus
type Condition struct {
	Type               string                 `json:"type"`
	Status             corev1.ConditionStatus `json:"status"`
	LastTransitionTime metav1.Time            `json:"lastTransitionTime"`
	Reason             string                 `json:"reason"`
	Message            string                 `json:"message"`
}

//+kubebuilder:object:root=true

// TrustyAIServiceList contains a list of TrustyAIService
type TrustyAIServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TrustyAIService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TrustyAIService{}, &TrustyAIServiceList{})
}

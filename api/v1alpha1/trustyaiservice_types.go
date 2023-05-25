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

type StorageSpec struct {
	Format string `json:"format"`
	Folder string `json:"folder"`
	PV     string `json:"pv"`
	Size   string `json:"size"`
}

type DataSpec struct {
	Filename string `json:"filename"`
	Format   string `json:"format"`
}

type MetricsSpec struct {
	Schedule string `json:"schedule"`
}

// TrustyAIServiceSpec defines the desired state of TrustyAIService
type TrustyAIServiceSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// The namespace in which to deploy the image
	Namespace string `json:"namespace"`
	// The image to deploy
	// +optional
	Image string `json:"image,omitempty"`
	// The tag to deploy
	// +optional
	Tag string `json:"tag,omitempty"`
	// Number of replicas
	// +optional
	Replicas *int32      `json:"replicas"`
	Storage  StorageSpec `json:"storage"`
	Data     DataSpec    `json:"data"`
	Metrics  MetricsSpec `json:"metrics"`
}

// TrustyAIServiceStatus defines the observed state of TrustyAIService
type TrustyAIServiceStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// TrustyAIService is the Schema for the trustyaiservices API
type TrustyAIService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TrustyAIServiceSpec   `json:"spec,omitempty"`
	Status TrustyAIServiceStatus `json:"status,omitempty"`
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

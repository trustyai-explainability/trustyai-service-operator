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

// Represent a service's status
// +kubebuilder:validation:Enum=New;Scheduled;Running;Complete;Cancelled
type ServiceState string

const (
	// The service is just created
	NewServiceState ServiceState = "New"
	// The service is scheduled and waiting for available resources to run it
	ScheduledServiceState ServiceState = "Scheduled"
	// The service is running
	RunningServiceState ServiceState = "Running"
	// The service is complete
	CompleteServiceState ServiceState = "Complete"
	// The service is cancelled
	CancelledServiceState ServiceState = "Cancelled"
)

// +kubebuilder:validation:Enum=NoReason;Succeeded;Failed;Cancelled
type Reason string

const (
	// Service is still running and no final result yet
	NoReason Reason = "NoReason"
	// Service finished successfully
	SucceedReason Reason = "Succeeded"
	// Service failed
	FailedReason Reason = "Failed"
	// Service is cancelled
	CancelledReason Reason = "Cancelled"
)

type Arg struct {
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
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

// GuardrailsServiceSpec defines the desired state of GuardrailsService
type GuardrailsServiceSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Generator name
	Generator string `json:"generator"`
	// Chunker name
	// + Optional
	Chunker string `json:"chunker"`
	// Args for the chunker
	// + optional
	ChunkerArgs []Arg `json:"chunkerArgs,omitempty"`
	// Detector name
	Detector string `json:"detector"`
	// Args for the detector
	// +optional
	DetectorArgs []Arg `json:"detectorArgs,omitempty"`
	// Evaluation tasks
	EnvSecrets []EnvSecret `json:"envSecrets,omitempty"`
	// Use secrets as files
	FileSecrets []FileSecret `json:"fileSecrets,omitempty"`
}

// GuardrailsServiceStatus defines the observed state of GuardrailsService
type GuardrailsServiceStatus struct {
	// Important: Run "make" to regenerate code after modifying this file

	// The name of the Pod that runs the evaluation Service
	// +optional
	PodName string `json:"podName,omitempty"`
	// State of the Service
	// +optional
	State ServiceState `json:"state,omitempty"`
	// Final result of the Service
	// +optional
	Reason Reason `json:"reason,omitempty"`
	// Message about the current/final status
	// +optional
	Message string `json:"message,omitempty"`
	// Information when was the last time the Service was successfully scheduled.
	// +optional
	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty"`
	// Information when the Service's state changes to Complete.
	// +optional
	CompleteTime *metav1.Time `json:"completeTime,omitempty"`
	// Evaluation results
	// +optional
	Results string `json:"results,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// GuardrailsService is the Schema for the GuardrailsService API
type GuardrailsService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GuardrailsServiceSpec   `json:"spec,omitempty"`
	Status GuardrailsServiceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GuardrailsServiceList contains a list of GuardrailsService
type GuardrailsServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GuardrailsService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GuardrailsService{}, &GuardrailsServiceList{})
}

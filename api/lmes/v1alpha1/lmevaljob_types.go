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

// Represent a job's status
// +kubebuilder:validation:Enum=New;Scheduled;Running;Complete;Cancelled
type JobState string

const (
	// The job is just created
	NewJobState JobState = "New"
	// The job is scheduled and waiting for available resources to run it
	ScheduledJobState JobState = "Scheduled"
	// The job is running
	RunningJobState JobState = "Running"
	// The job is complete
	CompleteJobState JobState = "Complete"
	// The job is cancelled
	CancelledJobState JobState = "Cancelled"
)

// +kubebuilder:validation:Enum=NoReason;Succeeded;Failed;Cancelled
type Reason string

const (
	// Job is still running and no final result yet
	NoReason Reason = "NoReason"
	// Job finished successfully
	SucceedReason Reason = "Succeeded"
	// Job failed
	FailedReason Reason = "Failed"
	// Job is cancelled
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

// LMEvalJobSpec defines the desired state of LMEvalJob
type LMEvalJobSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Model name
	Model string `json:"model"`
	// Args for the model
	// +optional
	ModelArgs []Arg `json:"modelArgs,omitempty"`
	// Evaluation tasks
	Tasks []string `json:"tasks"`
	// Sets the number of few-shot examples to place in context
	// +optional
	NumFewShot *int `json:"numFewShot,omitempty"`
	// Accepts an integer, or a float between 0.0 and 1.0 . If passed, will limit
	// the number of documents to evaluate to the first X documents (if an integer)
	// per task or first X% of documents per task
	// +optional
	Limit string `json:"limit,omitempty"`
	// Map to `--gen_kwargs` parameter for the underlying library.
	// +optional
	GenArgs []Arg `json:"genArgs,omitempty"`
	// If this flag is passed, then the model's outputs, and the text fed into the
	// model, will be saved at per-document granularity
	// +optional
	LogSamples *bool `json:"logSamples,omitempty"`
	// Assign secrets to the environment variables
	// +optional
	EnvSecrets []EnvSecret `json:"envSecrets,omitempty"`
	// Use secrets as files
	FileSecrets []FileSecret `json:"fileSecrets,omitempty"`
	// Batch size for the evaluation. This is used by the models that run and are loaded
	// locally and not apply for the commercial APIs.
	BatchSize *int `json:"batchSize,omitempty"`
}

// LMEvalJobStatus defines the observed state of LMEvalJob
type LMEvalJobStatus struct {
	// Important: Run "make" to regenerate code after modifying this file

	// The name of the Pod that runs the evaluation job
	// +optional
	PodName string `json:"podName,omitempty"`
	// State of the job
	// +optional
	State JobState `json:"state,omitempty"`
	// Final result of the job
	// +optional
	Reason Reason `json:"reason,omitempty"`
	// Message about the current/final status
	// +optional
	Message string `json:"message,omitempty"`
	// Information when was the last time the job was successfully scheduled.
	// +optional
	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty"`
	// Information when the job's state changes to Complete.
	// +optional
	CompleteTime *metav1.Time `json:"completeTime,omitempty"`
	// Evaluation results
	// +optional
	Results string `json:"results,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// LMEvalJob is the Schema for the lmevaljobs API
type LMEvalJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LMEvalJobSpec   `json:"spec,omitempty"`
	Status LMEvalJobStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// LMEvalJobList contains a list of LMEvalJob
type LMEvalJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LMEvalJob `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LMEvalJob{}, &LMEvalJobList{})
}

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
	"fmt"
	"strings"

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

type Card struct {
	// Unitxt card's ID
	// +optional
	Name string `json:"name,omitempty"`
	// A JSON string for a custom unitxt card which contains the custom dataset.
	// Use the documentation here: https://www.unitxt.ai/en/latest/docs/adding_dataset.html#adding-to-the-catalog
	// to compose a custom card, store it as a JSON file, and use the JSON content as the value here.
	// +optional
	Custom string `json:"custom,omitempty"`
}

// Use a task recipe to form a custom task. It maps to the Unitxt Recipe
// Find details of the Unitxt Recipe here:
// https://www.unitxt.ai/en/latest/unitxt.standard.html#unitxt.standard.StandardRecipe
type TaskRecipe struct {
	// The Unitxt dataset card
	Card Card `json:"card"`
	// The Unitxt template
	Template string `json:"template"`
	// The Unitxt Task
	// +optional
	Task *string `json:"task,omitempty"`
	// Metrics
	// +optional
	Metrics []string `json:"metrics,omitempty"`
	// The Unitxt format
	// +optional
	Format *string `json:"format,omitempty"`
	// A limit number of records to load
	// +optional
	LoaderLimit *int `json:"loaderLimit,omitempty"`
	// Number of fewshot
	// +optional
	NumDemos *int `json:"numDemos,omitempty"`
	// The pool size for the fewshot
	// +optional
	DemosPoolSize *int `json:"demosPoolSize,omitempty"`
}

type TaskList struct {
	// TaskNames from lm-eval's task list
	TaskNames []string `json:"taskNames,omitempty"`
	// Task Recipes specifically for Unitxt
	TaskRecipes []TaskRecipe `json:"taskRecipes,omitempty"`
}

func (t *TaskRecipe) String() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("card=%s,template=%s", t.Card.Name, t.Template))
	if t.Task != nil {
		b.WriteString(fmt.Sprintf(",task=%s", *t.Task))
	}
	if len(t.Metrics) > 0 {
		b.WriteString(fmt.Sprintf(",metrics=[%s]", strings.Join(t.Metrics, ",")))
	}
	if t.Format != nil {
		b.WriteString(fmt.Sprintf(",format=%s", *t.Format))
	}
	if t.LoaderLimit != nil {
		b.WriteString(fmt.Sprintf(",loader_limit=%d", *t.LoaderLimit))
	}
	if t.NumDemos != nil {
		b.WriteString(fmt.Sprintf(",num_demos=%d", *t.NumDemos))
	}
	if t.DemosPoolSize != nil {
		b.WriteString(fmt.Sprintf(",demos_pool_size=%d", *t.DemosPoolSize))
	}
	return b.String()
}

type LMEvalContainer struct {
	// Define Env information for the main container
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`
	// Define the volume mount information
	// +optional
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`
	// Compute Resources required by this container.
	// More info: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// The following Getter-ish functions avoid nil pointer panic
func (c *LMEvalContainer) GetEnv() []corev1.EnvVar {
	if c == nil {
		return nil
	}
	return c.Env
}

func (c *LMEvalContainer) GetVolumMounts() []corev1.VolumeMount {
	if c == nil {
		return nil
	}
	return c.VolumeMounts
}

func (c *LMEvalContainer) GetResources() *corev1.ResourceRequirements {
	if c == nil {
		return nil
	}
	return c.Resources
}

type LMEvalPodSpec struct {
	// Extra container data for the lm-eval container
	// +optional
	Container *LMEvalContainer `json:"container,omitempty"`
	// Specify the volumes information for the lm-eval and sidecar containers
	// +optional
	Volumes []corev1.Volume `json:"volumes,omitempty"`
	// Specify extra containers for the lm-eval job
	// FIXME: aggregate the sidecar containers into the pod
	// +optional
	SideCars []corev1.Container `json:"sideCars,omitempty"`
}

// The following Getter-ish functions avoid nil pointer panic
func (p *LMEvalPodSpec) GetContainer() *LMEvalContainer {
	if p == nil {
		return nil
	}
	return p.Container
}

func (p *LMEvalPodSpec) GetVolumes() []corev1.Volume {
	if p == nil {
		return nil
	}
	return p.Volumes
}

func (p *LMEvalPodSpec) GetSideCards() []corev1.Container {
	if p == nil {
		return nil
	}
	return p.SideCars
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
	// Evaluation task list
	TaskList TaskList `json:"taskList"`
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
	// Batch size for the evaluation. This is used by the models that run and are loaded
	// locally and not apply for the commercial APIs.
	BatchSize *int `json:"batchSize,omitempty"`
	// Specify extra information for the lm-eval job's pod
	// +optional
	Pod *LMEvalPodSpec `json:"pod,omitempty"`
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
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`
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

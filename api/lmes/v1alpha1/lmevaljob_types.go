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
// +kubebuilder:validation:Enum=New;Scheduled;Running;Complete;Cancelled;Suspended
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
	// The job is suspended
	SuspendedJobState JobState = "Suspended"
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
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9._-]+$`
	Name string `json:"name"`
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9._/:\- ]*$`
	Value string `json:"value,omitempty"`
}

type Card struct {
	// Unitxt card's ID
	// +optional
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9._-]+$`
	Name string `json:"name,omitempty"`
	// A JSON string for a custom unitxt card which contains the custom dataset.
	// Use the documentation here: https://www.unitxt.ai/en/latest/docs/adding_dataset.html#adding-to-the-catalog
	// to compose a custom card, store it as a JSON file, and use the JSON content as the value here.
	// +optional
	Custom string `json:"custom,omitempty"`
}

type Template struct {
	// Unitxt template ID
	// +optional
	Name string `json:"name,omitempty"`
	// The name of the custom template in the custom field. Its value is a JSON string
	// for a custom Unitxt template. Use the documentation here: https://www.unitxt.ai/en/latest/docs/adding_template.html
	// to compose a custom template, store it as a JSON file by calling the
	// add_to_catalog API: https://www.unitxt.ai/en/latest/docs/saving_and_loading_from_catalog.html#adding-assets-to-the-catalog,
	// and use the JSON content as the value here.
	// +optional
	Ref string `json:"ref,omitempty"`
}

type SystemPrompt struct {
	// Unitxt System Prompt id
	Name string `json:"name,omitempty"`
	// The name of the custom systemPrompt in the custom field. Its value is a custom system prompt string
	Ref string `json:"ref,omitempty"`
}

type Metric struct {
	// Unitxt metric id
	// +optional
	Name string `json:"name,omitempty"`
	// The name of the custom metric in the custom field. Its value is a JSON string
	// for a custom Unitxt metric. Use the documentation here: https://www.unitxt.ai/en/latest/docs/adding_metric.html#adding-a-new-instance-metric
	// to compose a custom metric, store it as a JSON file by calling the
	// add_to_catalog API: https://www.unitxt.ai/en/latest/docs/saving_and_loading_from_catalog.html#adding-assets-to-the-catalog,
	// and use the JSON content as the value here.
	// +optional
	Ref string `json:"ref,omitempty"`
}

type Task struct {
	// Unitxt task id
	// +optional
	Name string `json:"name,omitempty"`
	// The name of the custom task in the custom field. Its value is a JSON string
	// for a custom Unitxt task. Use the documentation here: https://www.unitxt.ai/en/latest/docs/adding_task.html
	// to compose a custom task, store it as a JSON file by calling the
	// add_to_catalog API: https://www.unitxt.ai/en/latest/docs/saving_and_loading_from_catalog.html#adding-assets-to-the-catalog,
	// and use the JSON content as the value here.
	// +optional
	Ref string `json:"ref,omitempty"`
}

type CustomArtifact struct {
	// Name of the custom artifact
	Name string `json:"name"`
	// Value of the custom artifact. It could be a JSON string or plain text
	// depending on the artifact type
	Value string `json:"value"`
}

func (c *CustomArtifact) String() string {
	return fmt.Sprintf("%s|%s", c.Name, c.Value)
}

type CustomArtifacts struct {
	// The Unitxt custom templates
	// +optional
	Templates []CustomArtifact `json:"templates,omitempty"`
	// The Unitxt custom System Prompts
	// +optional
	SystemPrompts []CustomArtifact `json:"systemPrompts,omitempty"`
	// The Unitxt custom metrics
	// +optional
	Metrics []CustomArtifact `json:"metrics,omitempty"`
	// The Unitxt custom tasks
	// +optional
	Tasks []CustomArtifact `json:"tasks,omitempty"`
}

func (c *CustomArtifacts) GetTemplates() []CustomArtifact {
	if c == nil {
		return nil
	}
	return c.Templates
}

func (c *CustomArtifacts) GetSystemPrompts() []CustomArtifact {
	if c == nil {
		return nil
	}
	return c.SystemPrompts
}

func (c *CustomArtifacts) GetMetrics() []CustomArtifact {
	if c == nil {
		return nil
	}
	return c.Metrics
}

func (c *CustomArtifacts) GetTasks() []CustomArtifact {
	if c == nil {
		return nil
	}
	return c.Tasks
}

// Use a task recipe to form a custom task. It maps to the Unitxt Recipe
// Find details of the Unitxt Recipe here:
// https://www.unitxt.ai/en/latest/unitxt.standard.html#unitxt.standard.StandardRecipe
type TaskRecipe struct {
	// The Unitxt dataset card
	Card Card `json:"card"`
	// The Unitxt template
	// +optional
	Template *Template `json:"template,omitempty"`
	// The Unitxt System Prompt
	// +optional
	SystemPrompt *SystemPrompt `json:"systemPrompt,omitempty"`
	// The Unitxt Task
	// +optional
	Task *Task `json:"task,omitempty"`
	// Metrics
	// +optional
	Metrics []Metric `json:"metrics,omitempty"`
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

// GitSource specifies the git location of external tasks
type GitSource struct {
	// URL specifies the git repository URL
	// +kubebuilder:validation:Pattern=`^https://[a-zA-Z0-9._/-]+$`
	URL string `json:"url,omitempty"`
	// Branch specifies the git branch to use
	// +optional
	Branch *string `json:"branch,omitempty"`
	// Commit specifies the git commit to use
	// +optional
	Commit *string `json:"commit,omitempty"`
	// Path specifies the path to the task file
	// +optional
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9._/-]*$`
	Path string `json:"path,omitempty"`
}

// CustomTaskSource specifies the source of custom tasks
type CustomTaskSource struct {
	// GitSource specifies the git location of external tasks
	GitSource GitSource `json:"git,omitempty"`
}

// CustomTasks specifies the custom (external) tasks to use
type CustomTasks struct {
	// Source specifies the source location of custom tasks
	Source CustomTaskSource `json:"source,omitempty"`
}

type TaskList struct {
	// TaskNames from lm-eval's task list and/or from custom tasks if CustomTasks is defined
	// +kubebuilder:validation:items:Pattern=`^[a-zA-Z0-9._-]+$`
	TaskNames []string `json:"taskNames,omitempty"`
	// Task Recipes specifically for Unitxt
	TaskRecipes []TaskRecipe `json:"taskRecipes,omitempty"`
	// Custom Unitxt artifacts that can be used in a TaskRecipe
	CustomArtifacts *CustomArtifacts `json:"custom,omitempty"`
	// CustomTasks is a list of external tasks
	CustomTasks *CustomTasks `json:"customTasks,omitempty"`
}

func (t *TaskList) HasCustomTasks() bool {
	return t.CustomTasks != nil && len(t.TaskNames) > 0
}

func (t *TaskList) HasCustomTasksWithGit() bool {
	return t.CustomTasks != nil && t.CustomTasks.Source.GitSource.URL != "" && len(t.TaskNames) > 0
}

func (m *Metric) String() string {
	if m == nil {
		return ""
	}
	if m.Name != "" {
		return m.Name
	}
	if m.Ref != "" {
		return fmt.Sprintf("metrics.%s", m.Ref)
	}
	return ""
}

func (t *Template) String() string {
	if t == nil {
		return ""
	}
	if t.Name != "" {
		return t.Name
	}
	if t.Ref != "" {
		return fmt.Sprintf("templates.%s", t.Ref)
	}
	return ""
}

func (s *SystemPrompt) String() string {
	if s == nil {
		return ""
	}
	if s.Name != "" {
		return s.Name
	}
	if s.Ref != "" {
		return fmt.Sprintf("system_prompts.%s", s.Ref)
	}
	return ""
}

func (t *Task) String() string {
	if t == nil {
		return ""
	}
	if t.Name != "" {
		return t.Name
	}
	if t.Ref != "" {
		return fmt.Sprintf("tasks.%s", t.Ref)
	}
	return ""
}

// Use the tp_idx and sp_idx to point to the corresponding custom template
// and custom system_prompt
func (t *TaskRecipe) String() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("card=%s", t.Card.Name))
	if t.Template != nil {
		b.WriteString(fmt.Sprintf(",template=%s", t.Template.String()))
	}
	if t.SystemPrompt != nil {
		b.WriteString(fmt.Sprintf(",system_prompt=%s", t.SystemPrompt.String()))
	}
	if t.Task != nil {
		b.WriteString(fmt.Sprintf(",task=%s", t.Task.String()))
	}
	if len(t.Metrics) > 0 {
		var metrics []string
		for _, metric := range t.Metrics {
			metrics = append(metrics, metric.String())
		}
		b.WriteString(fmt.Sprintf(",metrics=[%s]", strings.Join(metrics, ",")))
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
	// SecurityContext defines the security options the container should be run with.
	// If set, the fields of SecurityContext override the equivalent fields of PodSecurityContext.
	// More info: https://kubernetes.io/docs/tasks/configure-pod-container/security-context/
	// +optional
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`
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

type PersistentVolumeClaimManaged struct {
	Size string `json:"size,omitempty"`
}

type Outputs struct {
	// Use an existing PVC to store the outputs
	// +optional
	PersistentVolumeClaimName *string `json:"pvcName,omitempty"`
	// Create an operator managed PVC
	// +optional
	PersistentVolumeClaimManaged *PersistentVolumeClaimManaged `json:"pvcManaged,omitempty"`
}

func (c *LMEvalContainer) GetSecurityContext() *corev1.SecurityContext {
	if c == nil {
		return nil
	}
	return c.SecurityContext
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
	// If specified, the pod's scheduling constraints
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
	// SecurityContext holds pod-level security attributes and common container settings.
	// Optional: Defaults to empty.  See type description for default values of each field.
	// +optional
	SecurityContext *corev1.PodSecurityContext `json:"securityContext,omitempty"`
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

type OfflineS3Spec struct {
	AccessKeyIdRef     corev1.SecretKeySelector `json:"accessKeyId"`
	SecretAccessKeyRef corev1.SecretKeySelector `json:"secretAccessKey"`
	Bucket             corev1.SecretKeySelector `json:"bucket"`
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9._/-]*$`
	Path      string                    `json:"path"`
	Region    corev1.SecretKeySelector  `json:"region"`
	Endpoint  corev1.SecretKeySelector  `json:"endpoint"`
	VerifySSL *bool                     `json:"verifySSL,omitempty"`
	CABundle  *corev1.SecretKeySelector `json:"caBundle,omitempty"`
}

// OfflineStorageSpec defines the storage configuration for LMEvalJob's offline mode
type OfflineStorageSpec struct {
	PersistentVolumeClaimName *string        `json:"pvcName,omitempty"`
	S3Spec                    *OfflineS3Spec `json:"s3,omitempty"`
}

// OfflineSpec defined the configuration for LMEvalJob's offline mode
type OfflineSpec struct {
	StorageSpec OfflineStorageSpec `json:"storage"`
}

// ChatTemplate defines the configuration for the applied chat template during the evaluation.
type ChatTemplate struct {
	Enabled bool   `json:"enabled"`
	Name    string `json:"name,omitempty"`
}

// ProgressBar stores information about progress of various steps within an evaluation task.
type ProgressBar struct {
	// The title of the step being performed
	Message string `json:"message"`

	// The percent completion of this evaluation step
	Percent string `json:"percent"`

	// The elapsed time that this step has been running
	ElapsedTime string `json:"elapsedTime"`

	// The estimated time remaining for this evaluation step
	RemainingTimeEstimate string `json:"remainingTimeEstimate"`

	// Fractional progress through the evaluation step, presented as "$completed_items/$total_items"
	Count string `json:"count"`
}

func (p *LMEvalPodSpec) GetAffinity() *corev1.Affinity {
	if p == nil {
		return nil
	}
	return p.Affinity
}

func (p *LMEvalPodSpec) GetSecurityContext() *corev1.PodSecurityContext {
	if p == nil {
		return nil
	}
	return p.SecurityContext
}

// LMEvalJobSpec defines the desired state of LMEvalJob
type LMEvalJobSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Model name
	// +kubebuilder:validation:Enum=hf;openai-completions;openai-chat-completions;local-completions;local-chat-completions;watsonx_llm;textsynth
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
	// +kubebuilder:validation:Pattern=`^(\d+\.?\d*|\d*\.\d+)$`
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
	BatchSize *string `json:"batchSize,omitempty"`
	// Specify extra information for the lm-eval job's pod
	// +optional
	Pod *LMEvalPodSpec `json:"pod,omitempty"`
	// Suspend keeps the job but without pods. This is intended to be used by the Kueue integration
	// +optional
	Suspend bool `json:"suspend,omitempty"`
	// Outputs specifies storage for evaluation results
	// +optional
	Outputs *Outputs `json:"outputs,omitempty"`
	// Offline specifies settings for running LMEvalJobs in an offline mode
	// +optional
	Offline *OfflineSpec `json:"offline,omitempty"`
	// AllowOnly specifies whether the LMEvalJob can directly download remote code, datasets and metrics. Default is false.
	// +optional
	// +kubebuilder:default:=false
	AllowOnline *bool `json:"allowOnline,omitempty"`
	// AllowCodeExecution specifies whether the LMEvalJob can execute remote code. Default is false.
	// +optional
	// +kubebuilder:default:=false
	AllowCodeExecution *bool `json:"allowCodeExecution,omitempty"`
	// SystemInstruction will set the system instruction for all prompts passed to the evaluated model
	// +optional
	SystemInstruction string `json:"systemInstruction,omitempty"`
	// ChatTemplate defines whether to apply the default or specified chat template to prompts. This is required for chat-completions models.
	// +optional
	ChatTemplate *ChatTemplate `json:"chatTemplate,omitempty"`
}

// IsOffline returns whether this LMEvalJob is configured to run offline
func (s *LMEvalJobSpec) IsOffline() bool {
	return s.Offline != nil
}

func (s *LMEvalJobSpec) HasOfflinePVC() bool {
	return s.Offline != nil && s.Offline.StorageSpec.PersistentVolumeClaimName != nil
}

func (s *LMEvalJobSpec) HasOfflineS3() bool {
	return s.Offline != nil && s.Offline.StorageSpec.S3Spec != nil
}

func (s *OfflineS3Spec) HasCertificates() bool {
	return s.CABundle != nil
}

// HasCustomOutput returns whether an LMEvalJobSpec defines custom outputs or not
func (s *LMEvalJobSpec) HasCustomOutput() bool {
	return s.Outputs != nil
}

// HasManagedPVC returns whether the outputs define a managed PVC
func (o *Outputs) HasManagedPVC() bool {
	return o.PersistentVolumeClaimManaged != nil
}

// HasExistingPVC returns whether the outputs define an existing PVC
func (o *Outputs) HasExistingPVC() bool {
	return o.PersistentVolumeClaimName != nil
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

	// Information about the current job progress
	// +optional
	ProgressBars []ProgressBar `json:"progressBars,omitempty"`

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

// generate pod name for the job
func (j *LMEvalJob) GetPodName() string {
	return j.Name
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

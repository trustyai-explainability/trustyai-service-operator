package v1alpha1

import (
	common "github.com/opendatahub-io/odh-platform-utilities/api/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EnabledServices defines which TrustyAI services are enabled
type EnabledServices struct {
	// +optional
	TAS bool `json:"tas,omitempty"`
	// +optional
	LMES bool `json:"lmes,omitempty"`
	// +optional
	EvalHub bool `json:"evalHub,omitempty"`
	// +optional
	GORCH bool `json:"gorch,omitempty"`
	// +optional
	NemoGuardrails bool `json:"nemoGuardrails,omitempty"`
}

// LMEvalConfig defines LMEval-specific configuration
type LMEvalConfig struct {
	// PermitCodeExecution controls whether code execution is allowed during evaluations.
	// +kubebuilder:default=false
	// +optional
	PermitCodeExecution bool `json:"permitCodeExecution,omitempty"`
	// PermitOnline controls whether online access is allowed during evaluations.
	// +kubebuilder:default=false
	// +optional
	PermitOnline bool `json:"permitOnline,omitempty"`
	// MaxBatchSize is the maximum number of evaluation requests processed in a single batch.
	// +kubebuilder:default=24
	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxBatchSize int `json:"maxBatchSize,omitempty"`
	// DefaultBatchSize is the default number of evaluation requests processed in a single batch.
	// +kubebuilder:default=8
	// +kubebuilder:validation:Minimum=1
	// +optional
	DefaultBatchSize int `json:"defaultBatchSize,omitempty"`
	// DetectDevice controls whether the evaluation driver auto-detects available compute devices (CPU/GPU).
	// +kubebuilder:default=true
	// +optional
	DetectDevice bool `json:"detectDevice,omitempty"`
	// ImagePullPolicy is the image pull policy for LMES job pods.
	// +kubebuilder:default=Always
	// +kubebuilder:validation:Enum=Always;IfNotPresent;Never
	// +optional
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`
}

// EvalConfig defines evaluation-related configuration
type EvalConfig struct {
	// +optional
	LMEval LMEvalConfig `json:"lmeval,omitempty"`
}

// TrustyAICommonSpec holds the user-facing configuration shared between the
// module CR spec and the DSC stanza in the ODH operator.
type TrustyAICommonSpec struct {
	// +optional
	EnabledServices EnabledServices `json:"enabledServices,omitempty"`
	// +optional
	Eval EvalConfig `json:"eval,omitempty"`
	// KServeServerless controls whether KServe serverless mode is enabled for TrustyAI services.
	// +kubebuilder:default=true
	// +optional
	KServeServerless bool `json:"kServeServerless,omitempty"`
	// MCPGuardrailsMode deploys TrustyAI with only the NemoGuardrails service enabled,
	// using a dedicated Kustomize overlay. Mutually exclusive with other enabledServices flags.
	// +optional
	MCPGuardrailsMode bool `json:"mcpGuardrailsMode,omitempty"`
}

// TrustyAISpec defines the desired state of TrustyAI module
type TrustyAISpec struct {
	common.ManagementSpec `json:",inline"`
	TrustyAICommonSpec    `json:",inline"`
}

// DistributionInfo represents distribution information
type DistributionInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// TrustyAIStatus defines the observed state of TrustyAI module
type TrustyAIStatus struct {
	common.Status                 `json:",inline"`
	common.ComponentReleaseStatus `json:",inline"`
	// +optional
	Distribution DistributionInfo `json:"distribution,omitempty"`
}

const (
	// TrustyAIInstanceName is the singleton CR name enforced by the CEL validation rule.
	TrustyAIInstanceName = "default-trustyai"
)

// TrustyAI is the Schema for the trustyais API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=tai
// +kubebuilder:storageversion
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-trustyai'",message="TrustyAI resource must be named 'default-trustyai'"
// +kubebuilder:printcolumn:name="Management State",type=string,JSONPath=`.spec.managementState`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type TrustyAI struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TrustyAISpec   `json:"spec,omitempty"`
	Status TrustyAIStatus `json:"status,omitempty"`
}

// Compile-time assertion that TrustyAI implements common.PlatformObject.
var _ common.PlatformObject = (*TrustyAI)(nil)

func (t *TrustyAI) GetStatus() *common.Status {
	return &t.Status.Status
}

func (t *TrustyAI) GetConditions() []common.Condition {
	return t.Status.Status.GetConditions()
}

func (t *TrustyAI) SetConditions(c []common.Condition) {
	t.Status.Status.SetConditions(c)
}

func (t *TrustyAI) GetReleaseStatus() *common.ComponentReleaseStatus {
	return &t.Status.ComponentReleaseStatus
}

func (t *TrustyAI) SetReleaseStatus(s common.ComponentReleaseStatus) {
	t.Status.ComponentReleaseStatus = s
}

// +kubebuilder:object:root=true

// TrustyAIList contains a list of TrustyAI
type TrustyAIList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TrustyAI `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TrustyAI{}, &TrustyAIList{})
}

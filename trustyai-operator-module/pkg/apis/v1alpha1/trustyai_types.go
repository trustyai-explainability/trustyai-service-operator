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
	// +kubebuilder:default=false
	// +optional
	PermitCodeExecution bool `json:"permitCodeExecution,omitempty"`
	// +kubebuilder:default=false
	// +optional
	PermitOnline bool `json:"permitOnline,omitempty"`
}

// EvalConfig defines evaluation-related configuration
type EvalConfig struct {
	// +optional
	LMEval LMEvalConfig `json:"lmeval,omitempty"`
}

// TrustyAISpec defines the desired state of TrustyAI module
type TrustyAISpec struct {
	common.ManagementSpec `json:",inline"`
	// +optional
	EnabledServices EnabledServices `json:"enabledServices,omitempty"`
	// +optional
	Eval EvalConfig `json:"eval,omitempty"`
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

// TrustyAI is the Schema for the trustyais API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=tai
// +kubebuilder:storageversion
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default'",message="TrustyAI resource must be named 'default'"
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

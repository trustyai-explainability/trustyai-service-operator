package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ManagementState defines the management state of the module
// +kubebuilder:validation:Enum=Managed;Removed;Unmanaged
type ManagementState string

const (
	ManagementStateManaged   ManagementState = "Managed"
	ManagementStateRemoved   ManagementState = "Removed"
	ManagementStateUnmanaged ManagementState = "Unmanaged"
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
	// +kubebuilder:default=Managed
	// +optional
	ManagementState ManagementState `json:"managementState,omitempty"`
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

// ComponentRelease represents information about a component release
type ComponentRelease struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// TrustyAIStatus defines the observed state of TrustyAI module
type TrustyAIStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +optional
	Phase string `json:"phase,omitempty"`
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// +optional
	Distribution DistributionInfo `json:"distribution,omitempty"`
	// +optional
	Releases []ComponentRelease `json:"releases,omitempty"`
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

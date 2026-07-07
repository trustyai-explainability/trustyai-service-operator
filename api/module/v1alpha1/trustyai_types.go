package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ManagementState defines the management state of the module
// +kubebuilder:validation:Enum=Managed;Removed;Unmanaged
type ManagementState string

const (
	// Managed means the operator is actively managing the module
	ManagementStateManaged ManagementState = "Managed"
	// Removed means the operator should remove the module
	ManagementStateRemoved ManagementState = "Removed"
	// Unmanaged means the operator should not reconcile the module
	ManagementStateUnmanaged ManagementState = "Unmanaged"
)

// EnabledServices defines which TrustyAI services are enabled
type EnabledServices struct {
	// TAS enables the TrustyAI Service
	// +optional
	TAS bool `json:"tas,omitempty"`

	// LMES enables the LM Evaluation Service
	// +optional
	LMES bool `json:"lmes,omitempty"`

	// EvalHub enables the EvalHub service
	// +optional
	EvalHub bool `json:"evalHub,omitempty"`

	// GORCH enables the Guardrails Orchestrator
	// +optional
	GORCH bool `json:"gorch,omitempty"`

	// NemoGuardrails enables Nemo Guardrails
	// +optional
	NemoGuardrails bool `json:"nemoGuardrails,omitempty"`
}

// TrustyAISpec defines the desired state of TrustyAI module
type TrustyAISpec struct {
	// ManagementState indicates whether the module is managed or should be removed
	// +kubebuilder:default=Managed
	// +optional
	ManagementState ManagementState `json:"managementState,omitempty"`

	// EnabledServices defines which TrustyAI services are enabled
	// +optional
	EnabledServices EnabledServices `json:"enabledServices,omitempty"`
}

// DistributionInfo represents the distribution information
type DistributionInfo struct {
	// Name is the name of the distribution
	Name string `json:"name"`

	// Version is the version of the distribution
	Version string `json:"version"`
}

// ComponentRelease represents information about a component release
type ComponentRelease struct {
	// Name is the name of the component
	Name string `json:"name"`

	// Version is the version of the component
	Version string `json:"version"`
}

// TrustyAIStatus defines the observed state of TrustyAI module
type TrustyAIStatus struct {
	// ObservedGeneration is the last generation reconciled by the controller
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Phase represents the current phase of the module
	// +optional
	Phase string `json:"phase,omitempty"`

	// Conditions represent the latest available observations of the module's state
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Distribution contains information about the distribution
	// +optional
	Distribution DistributionInfo `json:"distribution,omitempty"`

	// Releases contains information about component releases
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

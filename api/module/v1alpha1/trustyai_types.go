package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TrustyAISpec defines the desired state of the TrustyAI module.
type TrustyAISpec struct {
	// ManagementState indicates whether the module is Managed or Removed.
	// +kubebuilder:default=Managed
	// +kubebuilder:validation:Enum=Managed;Removed
	ManagementState string `json:"managementState,omitempty"`

	// EnabledServices selects which sub-controllers to run.
	// When empty, all services are enabled.
	// +optional
	EnabledServices []string `json:"enabledServices,omitempty"`
}

// TrustyAIStatus defines the observed state of the TrustyAI module.
type TrustyAIStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Phase is a human-readable summary: Ready, Not Ready.
	// +optional
	Phase string `json:"phase,omitempty"`

	// Conditions represent the latest available observations of the module's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Releases lists the versions of components managed by this module.
	// +optional
	Releases []ComponentRelease `json:"releases,omitempty"`
}

// ComponentRelease describes a versioned component managed by the module.
type ComponentRelease struct {
	// Name of the component.
	Name string `json:"name"`
	// Version string (semver or commit SHA).
	Version string `json:"version"`
	// RepoURL is the source repository URL.
	// +optional
	RepoURL string `json:"repoUrl,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default'",message="TrustyAI is a singleton; the name must be 'default'"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// TrustyAI is the module CR created by the ODH platform orchestrator.
// It is cluster-scoped and must be a singleton named "default".
type TrustyAI struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TrustyAISpec   `json:"spec,omitempty"`
	Status TrustyAIStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TrustyAIList contains a list of TrustyAI.
type TrustyAIList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TrustyAI `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TrustyAI{}, &TrustyAIList{})
}

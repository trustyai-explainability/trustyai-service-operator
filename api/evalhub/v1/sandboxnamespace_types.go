package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// SandboxFinalizerName is the finalizer placed on a SandboxNamespace CR to
	// guarantee the operator deletes the provisioned sandbox namespace (and, via
	// cascade, everything inside it) before the CR is removed.
	SandboxFinalizerName = "trustyai.opendatahub.io/sandboxnamespace-finalizer"
)

// SandboxNamespaceSpec defines the desired state of a sandbox namespace. It is
// created by the eval-hub application for each accepted sandboxed evaluation job
// and is fully self-describing: the operator provisions resources solely from
// these fields and does not consult provider ConfigMaps.
type SandboxNamespaceSpec struct {
	// JobID is the identifier of the parent evaluation job this sandbox serves.
	// +kubebuilder:validation:MinLength=1
	JobID string `json:"jobID"`

	// EvalHubInstanceName is the name of the EvalHub CR that owns the parent job.
	// +optional
	EvalHubInstanceName string `json:"evalHubInstanceName,omitempty"`

	// EvalHubInstanceNamespace is the namespace of the EvalHub CR that owns the
	// parent job.
	// +optional
	EvalHubInstanceNamespace string `json:"evalHubInstanceNamespace,omitempty"`

	// NamespaceName optionally sets the name of the sandbox namespace to create.
	// When empty, the operator derives a stable name from the CR name and job ID.
	// Must be a valid DNS-1123 label so the API server rejects an unusable name at
	// admission rather than failing later during namespace creation.
	// +optional
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	NamespaceName string `json:"namespaceName,omitempty"`

	// ResourceEnvelope bounds the CPU, memory, and pod count enforced in the
	// sandbox namespace via a ResourceQuota.
	// +optional
	ResourceEnvelope *SandboxResourceEnvelope `json:"resourceEnvelope,omitempty"`

	// ModelEndpointURL is the model inference endpoint the sandboxed workload is
	// permitted to reach. Egress to this endpoint is allowed by NetworkPolicy;
	// all other external egress (except DNS and the eval-hub API) is denied.
	// +optional
	ModelEndpointURL string `json:"modelEndpointURL,omitempty"`

	// EvalHubAPIEndpoint is the eval-hub API endpoint the sandboxed workload is
	// permitted to reach for status callbacks. Egress to this endpoint is allowed
	// by NetworkPolicy.
	// +optional
	EvalHubAPIEndpoint string `json:"evalHubAPIEndpoint,omitempty"`
}

// SandboxNamespaceStatus defines the observed state of a sandbox namespace.
type SandboxNamespaceStatus struct {
	// Phase is the current lifecycle phase of the sandbox namespace.
	// +kubebuilder:validation:Enum=Pending;Ready;Terminating;Error
	// +optional
	Phase string `json:"phase,omitempty"`

	// NamespaceName is the name of the namespace the operator provisioned.
	// +optional
	NamespaceName string `json:"namespaceName,omitempty"`

	// Conditions represent the latest available observations of the sandbox state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// SandboxNamespace is the Schema for the sandboxnamespaces API. Each instance
// represents one isolated namespace scoped to a single evaluation job's lifecycle.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=`.status.namespaceName`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type SandboxNamespace struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SandboxNamespaceSpec   `json:"spec,omitempty"`
	Status SandboxNamespaceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// SandboxNamespaceList contains a list of SandboxNamespace
type SandboxNamespaceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SandboxNamespace `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SandboxNamespace{}, &SandboxNamespaceList{})
}

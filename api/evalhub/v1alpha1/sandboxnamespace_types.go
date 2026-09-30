package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// SandboxFinalizerName is the finalizer placed on a SandboxNamespace CR to
	// guarantee the operator deletes the provisioned sandbox namespace (and, via
	// cascade, everything inside it) before the CR is removed.
	SandboxFinalizerName = "trustyai.opendatahub.io/sandboxnamespace-finalizer"

	// SandboxNamespaceReady is the condition type reporting whether the sandbox
	// namespace and its isolation resources have been provisioned.
	SandboxNamespaceReady = "Ready"

	// Condition reasons for SandboxNamespaceReady (CamelCase per API conventions).
	SandboxReasonProvisioned        = "Provisioned"
	SandboxReasonProvisioningFailed = "ProvisioningFailed"
)

// SandboxResourceEnvelope bounds the resources available inside a sandbox
// namespace. It maps onto a ResourceQuota created in the sandbox namespace.
type SandboxResourceEnvelope struct {
	// CPU is the total CPU (requests and limits) available to the sandbox
	// namespace, expressed as a Kubernetes resource quantity (e.g. "2", "500m").
	// It must be non-negative: the value maps onto ResourceQuota hard limits, which
	// Kubernetes forbids from being negative.
	// +optional
	// +kubebuilder:validation:XValidation:rule="!string(self).startsWith('-')",message="cpu must be non-negative"
	CPU *resource.Quantity `json:"cpu,omitempty"`

	// Memory is the total memory (requests and limits) available to the sandbox
	// namespace, expressed as a Kubernetes resource quantity (e.g. "4Gi").
	// It must be non-negative: the value maps onto ResourceQuota hard limits, which
	// Kubernetes forbids from being negative.
	// +optional
	// +kubebuilder:validation:XValidation:rule="!string(self).startsWith('-')",message="memory must be non-negative"
	Memory *resource.Quantity `json:"memory,omitempty"`

	// MaxPods is the maximum number of pods allowed in the sandbox namespace.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MaxPods int32 `json:"maxPods,omitempty"`
}

// SandboxNamespaceSpec defines the desired state of a sandbox namespace. It is
// created by the eval-hub application for each accepted sandboxed evaluation job
// and is fully self-describing: the operator provisions resources solely from
// these fields and does not consult provider ConfigMaps.
type SandboxNamespaceSpec struct {
	// JobID is the identifier of the parent evaluation job this sandbox serves.
	// +kubebuilder:validation:MinLength=1
	JobID string `json:"jobID"`

	// The owning EvalHub CR is identified by a controller owner reference on this
	// resource's metadata rather than by spec fields. Because a SandboxNamespace is
	// always created in the same namespace as its EvalHub, an owner reference is
	// valid, gives cascading garbage collection of the CR, and avoids a redundant
	// namespace field.

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
	// ObservedGeneration is the .metadata.generation the operator last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// NamespaceName is the name of the namespace the operator provisioned.
	// +optional
	NamespaceName string `json:"namespaceName,omitempty"`

	// Conditions represent the latest available observations of the sandbox state.
	// The "Ready" condition reports whether the namespace and its isolation
	// resources have been provisioned.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// SandboxNamespace is the Schema for the sandboxnamespaces API. Each instance
// represents one isolated namespace scoped to a single evaluation job's lifecycle.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
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

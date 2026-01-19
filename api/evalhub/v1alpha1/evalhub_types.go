package v1alpha1

import (
	"github.com/trustyai-explainability/trustyai-service-operator/api/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EvalHub is the Schema for the evalhubs API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.ready`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type EvalHub struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   EvalHubSpec   `json:"spec,omitempty"`
	Status EvalHubStatus `json:"status,omitempty"`
}

// EvalHubSpec defines the desired state of EvalHub
type EvalHubSpec struct {
	// Number of replicas for the eval-hub deployment
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default:=1
	Replicas *int32 `json:"replicas,omitempty"`

	// Environment variables for the eval-hub container
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`
}

// EvalHubStatus defines the observed state of EvalHub
type EvalHubStatus struct {
	// Current phase of the EvalHub (Pending, Ready, Error)
	// +kubebuilder:validation:Enum=Pending;Ready;Error
	Phase string `json:"phase,omitempty"`

	// Number of desired replicas
	Replicas int32 `json:"replicas,omitempty"`

	// Number of ready replicas
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// Conditions represent the latest available observations
	Conditions []common.Condition `json:"conditions,omitempty"`

	// Ready indicates whether the EvalHub is ready to serve requests
	Ready corev1.ConditionStatus `json:"ready,omitempty"`

	// URL where the EvalHub service is accessible
	URL string `json:"url,omitempty"`

	// List of active providers
	ActiveProviders []string `json:"activeProviders,omitempty"`

	// Last time the status was updated
	LastUpdateTime *metav1.Time `json:"lastUpdateTime,omitempty"`
}

// +kubebuilder:object:root=true
// EvalHubList contains a list of EvalHub
type EvalHubList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []EvalHub `json:"items"`
}

func init() {
	SchemeBuilder.Register(&EvalHub{}, &EvalHubList{})
}

// SetStatus sets the status of the EvalHub
func (e *EvalHub) SetStatus(condType, reason, message string, status corev1.ConditionStatus) {
	now := metav1.Now()
	condition := common.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
	}
	// Replace or append condition
	found := false
	for i, cond := range e.Status.Conditions {
		if cond.Type == condType {
			e.Status.Conditions[i] = condition
			found = true
			break
		}
	}
	if !found {
		e.Status.Conditions = append(e.Status.Conditions, condition)
	}
	e.Status.LastUpdateTime = &now
}

// IsReady returns true if the EvalHub is ready
func (e *EvalHub) IsReady() bool {
	return e.Status.Ready == corev1.ConditionTrue
}

// GetReplicas returns the number of replicas, defaulting to 1
func (e *EvalHubSpec) GetReplicas() int32 {
	if e.Replicas == nil {
		return 1
	}
	return *e.Replicas
}

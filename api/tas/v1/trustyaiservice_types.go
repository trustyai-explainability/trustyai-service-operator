package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TrustyAIService is the Schema for the trustyaiservices API
// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
type TrustyAIService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TrustyAIServiceSpec   `json:"spec,omitempty"`
	Status TrustyAIServiceStatus `json:"status,omitempty"`
}

type StorageSpec struct {
	// Format only supports "PVC" or "DATABASE" values
	// +kubebuilder:validation:Enum=PVC;DATABASE
	Format                 string `json:"format"`
	Folder                 string `json:"folder,omitempty"`
	Size                   string `json:"size,omitempty"`
	DatabaseConfigurations string `json:"databaseConfigurations,omitempty"`
}

type DataSpec struct {
	Filename string `json:"filename,omitempty"`
	Format   string `json:"format,omitempty"`
}

type MetricsSpec struct {
	Schedule  string `json:"schedule"`
	BatchSize *int   `json:"batchSize,omitempty"`
}

// TrustyAIServiceSpec defines the desired state of TrustyAIService
type TrustyAIServiceSpec struct {
	// Number of replicas
	// +optional
	Replicas *int32      `json:"replicas"`
	Storage  StorageSpec `json:"storage"`
	Data     DataSpec    `json:"data,omitempty"`
	Metrics  MetricsSpec `json:"metrics"`
}

// TASCondition mirrors metav1.Condition for platform contract compliance (RHOAIENG-67659),
// but keeps lastTransitionTime, reason, and message optional to preserve backward
// compatibility with conditions stored by prior operator versions.
// +kubebuilder:object:generate=true
type TASCondition struct {
	// type is the condition type in CamelCase.
	// +kubebuilder:validation:Required
	Type string `json:"type"`

	// status is the condition status: True, False, or Unknown.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=True;False;Unknown
	Status metav1.ConditionStatus `json:"status"`

	// observedGeneration is the generation when this condition was last set.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// lastTransitionTime is when the condition last changed status.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`

	// reason is a CamelCase identifier for the condition's cause.
	// +optional
	Reason string `json:"reason,omitempty"`

	// message is a human-readable description of the condition.
	// +optional
	Message string `json:"message,omitempty"`
}

// TrustyAIServiceStatus defines the observed state of TrustyAIService
type TrustyAIServiceStatus struct {
	// ObservedGeneration is the last generation reconciled by the controller
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Phase represents the current phase of the service
	// +optional
	Phase string `json:"phase,omitempty"`

	// Replicas is the number of running replicas
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// Conditions represent the latest available observations of the service's state.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []TASCondition `json:"conditions,omitempty"`

	// Ready indicates whether the service is ready
	// +optional
	// Deprecated: Use Conditions with type "Ready" instead
	Ready corev1.ConditionStatus `json:"ready,omitempty"`
}

// +kubebuilder:object:root=true
// TrustyAIServiceList contains a list of TrustyAIService
type TrustyAIServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TrustyAIService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TrustyAIService{}, &TrustyAIServiceList{})
}

// Hub marks this version as a conversion hub.
func (*TrustyAIService) Hub() {}

// IsDatabaseConfigurationsSet returns true if the DatabaseConfigurations field is set.
func (s *StorageSpec) IsDatabaseConfigurationsSet() bool {
	return s.DatabaseConfigurations != ""
}

// IsStoragePVC returns true if the storage is set to PVC.
func (s *StorageSpec) IsStoragePVC() bool {
	return s.Format == "PVC"
}

// IsStorageDatabase returns true if the storage is set to database.
func (s *StorageSpec) IsStorageDatabase() bool {
	return s.Format == "DATABASE"
}

// IsMigration returns true if the migration fields are set.
func (t *TrustyAIService) IsMigration() bool {
	if t.Spec.Storage.Format == "DATABASE" && t.Spec.Storage.Folder != "" && t.Spec.Data.Filename != "" {
		return true
	} else {
		return false
	}
}

// SetStatus sets the status of the TrustyAIService using TASCondition
func (t *TrustyAIService) SetStatus(condType, reason, message string, status corev1.ConditionStatus) {
	metaStatus := metav1.ConditionUnknown
	switch status {
	case corev1.ConditionTrue:
		metaStatus = metav1.ConditionTrue
	case corev1.ConditionFalse:
		metaStatus = metav1.ConditionFalse
	}

	condition := TASCondition{
		Type:               condType,
		Status:             metaStatus,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: t.Generation,
	}

	found := false
	for i, cond := range t.Status.Conditions {
		if cond.Type == condType {
			if cond.Status != condition.Status {
				condition.LastTransitionTime = metav1.Now()
			} else {
				condition.LastTransitionTime = cond.LastTransitionTime
			}
			t.Status.Conditions[i] = condition
			found = true
			break
		}
	}
	if !found {
		condition.LastTransitionTime = metav1.Now()
		t.Status.Conditions = append(t.Status.Conditions, condition)
	}
}

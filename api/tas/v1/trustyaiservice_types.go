package v1

import (
	"github.com/trustyai-explainability/trustyai-service-operator/api/common"
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

// TrustyAIServiceStatus defines the observed state of TrustyAIService
type TrustyAIServiceStatus struct {
	// Define your status fields here
	Phase      string                 `json:"phase"`
	Replicas   int32                  `json:"replicas"`
	Conditions []common.Condition     `json:"conditions"`
	Ready      corev1.ConditionStatus `json:"ready,omitempty"`
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

// SetStatus sets the status of the TrustyAIService
func (t *TrustyAIService) SetStatus(condType, reason, message string, status corev1.ConditionStatus) {
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
	for i, cond := range t.Status.Conditions {
		if cond.Type == condType {
			t.Status.Conditions[i] = condition
			found = true
			break
		}
	}
	if !found {
		t.Status.Conditions = append(t.Status.Conditions, condition)
	}
}

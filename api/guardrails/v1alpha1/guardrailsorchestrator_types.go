package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (g *GuardrailsOrchestrator) SetStatus(condType, reason, message string, status corev1.ConditionStatus) {
	now := metav1.Now()
	condition := Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
	}
	// Replace or append condition
	found := false
	for i, cond := range g.Status.Conditions {
		if cond.Type == condType {
			g.Status.Conditions[i] = condition
			found = true
			break
		}
	}
	if !found {
		g.Status.Conditions = append(g.Status.Conditions, condition)
	}
}

type ServiceSpec struct {
	Hostname string `json:"hostname"`
	Port     int    `json:"port"`
	// TLS credentials path
	TLS string `json:"tls"`
}

type GeneratorSpec struct {
	// Provider name
	Provider string `json:"provider"`
	// Service
	Service ServiceSpec `json:"service"`
}

type ChunkerSpec struct {
	Type    string      `json:"type"`
	Service ServiceSpec `json:"service"`
}

type DetectorSpec struct {
	// Detector name
	Type string `json:"type"`
	// Service
	Service ServiceSpec `json:"service"`
	// Chunker ID
	ChunkerID        string `json:"chunker_id"`
	DefaultThreshold string `json:"default_threshold"`
}

type TLSSpec struct {
	Type       string `json:"type"`
	CertPath   string `json:"cert_path"`
	KeyPath    string `json:"key_path"`
	CACertPath string `json:"ca_cert_path"`
}

// GuardrailsOrchestratorStatus defines the desired state of GuardrailsOrchestrator
type GuardrailsOrchestratorSpec struct {
	// Number of replicas
	Replicas int32 `json:"replicas"`
	// Generator name
	Generator GeneratorSpec `json:"generator"`
	// Chunker name
	Chunker ChunkerSpec `json:"chunker"`
	// Detector name(s)
	Detectors []DetectorSpec `json:"detectors"`
	// TLS
	TLS TLSSpec `json:"tls"`
}

type GuardrailsOrchestratorPod struct {
	Container *GuardrailsOrchestratorContainer `json:"container"`
	Volumes   []corev1.Volume                  `json:"volumes, omitempty"`
}

// The following Getter-ish functions avoid nil pointer panic
func (p *GuardrailsOrchestratorPod) GetContainer() *GuardrailsOrchestratorContainer {
	if p == nil {
		return nil
	}
	return p.Container
}

func (p *GuardrailsOrchestratorPod) GetVolumes() []corev1.Volume {
	if p == nil {
		return nil
	}
	return p.Volumes
}

type GuardrailsOrchestratorContainer struct {
	// Define Env information for the main container
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`
	// Define the volume mount information
	// +optional
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`
	// Compute Resources required by this container.
	// More info: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// The following Getter-ish functions avoid nil pointer panic
func (c *GuardrailsOrchestratorContainer) GetEnv() []corev1.EnvVar {
	if c == nil {
		return nil
	}
	return c.Env
}

func (c *GuardrailsOrchestratorContainer) GetVolumeMounts() []corev1.VolumeMount {
	if c == nil {
		return nil
	}
	return c.VolumeMounts
}

func (c *GuardrailsOrchestratorContainer) GetResources() *corev1.ResourceRequirements {
	if c == nil {
		return nil
	}
	return c.Resources
}

// Condition defines the condition of the GuardrailsOrchestrator
type Condition struct {
	Type               string                 `json:"type"`
	Status             corev1.ConditionStatus `json:"status"`
	LastTransitionTime metav1.Time            `json:"lastTransitionTime"`
	Reason             string                 `json:"reason"`
	Message            string                 `json:"message"`
}

// GuardrailsOrchestratorStatus defines the observed state of GuardrailsOrchestrator
type GuardrailsOrchestratorStatus struct {
	Phase      string                 `json:"phase"`
	Replicas   int32                  `json:"replicas"`
	Conditions []Condition            `json:"conditions"`
	Ready      corev1.ConditionStatus `json:"ready,omitempty"`
}

// GuardrailsOrchestrator is the Schema for the GuardrailsOrchestrator API
type GuardrailsOrchestrator struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GuardrailsOrchestratorSpec   `json:"spec,omitempty"`
	Status GuardrailsOrchestratorStatus `json:"status,omitempty"`
}

// GuardrailsOrchestratorList contains a list of GuardrailsOrchestrator
type GuardrailsOrchestratorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GuardrailsOrchestrator `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GuardrailsOrchestrator{}, &GuardrailsOrchestratorList{})
}

/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"github.com/trustyai-explainability/trustyai-service-operator/api/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TargetRef identifies the target Gateway resource.
type TargetRef struct {
	// Group is the API group of the target resource.
	// +kubebuilder:default="gateway.networking.k8s.io"
	Group string `json:"group"`
	// Kind is the kind of the target resource.
	// +kubebuilder:default="Gateway"
	Kind string `json:"kind"`
	// Name is the name of the target resource.
	Name string `json:"name"`
}

// NemoConfigRef references a ConfigMap containing NeMo configuration.
type NemoConfigRef struct {
	// Name of this config set (used as directory name in /app/config/).
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9_-]+$`
	Name string `json:"name"`
	// ConfigMaps lists ConfigMap names containing config files.
	ConfigMaps []string `json:"configMaps"`
	// Default marks this config as the default for the NeMo server.
	// +optional
	Default bool `json:"default,omitempty"`
}

// NemoGuardrailsConfig defines the NeMo Guardrails server deployment.
type NemoGuardrailsConfig struct {
	// NemoConfigs lists the guardrail configuration sets to deploy.
	NemoConfigs []NemoConfigRef `json:"nemoConfigs"`
	// Replicas for the NeMo Guardrails deployment.
	// +optional
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	Replicas *int32 `json:"replicas,omitempty"`
	// Env defines additional environment variables for the NeMo container.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`
}

// GuardPluginConfig configures an IPP guardrail plugin.
type GuardPluginConfig struct {
	// Enabled controls whether this guard plugin is active.
	// +kubebuilder:default=true
	Enabled *bool `json:"enabled,omitempty"`
	// NemoURL overrides the auto-discovered NeMo service URL.
	// +optional
	NemoURL string `json:"nemoURL,omitempty"`
	// TimeoutSeconds for the NeMo guardrail check call.
	// +optional
	// +kubebuilder:default=360
	// +kubebuilder:validation:Minimum=1
	TimeoutSeconds *int `json:"timeoutSeconds,omitempty"`
}

// MergeStrategy defines how policies are merged.
// +kubebuilder:validation:Enum=atomic;merge
type MergeStrategy string

const (
	MergeStrategyAtomic MergeStrategy = "atomic"
	MergeStrategyMerge  MergeStrategy = "merge"
)

// PolicyDefaults defines the default guardrail rules applied when no namespace-level policy overrides them.
type PolicyDefaults struct {
	// Guardrails defines the default guardrail rules.
	Guardrails *GuardrailRules `json:"guardrails,omitempty"`
}

// PolicyOverrides defines guardrail rules that cannot be overridden by namespace-level policies.
type PolicyOverrides struct {
	// Strategy controls how overrides are applied. "atomic" replaces the entire config; "merge" merges individual fields.
	// +kubebuilder:default="atomic"
	// +optional
	Strategy MergeStrategy `json:"strategy,omitempty"`
	// Guardrails defines the override guardrail rules.
	Guardrails *GuardrailRules `json:"guardrails,omitempty"`
}

// GuardrailRules defines the guardrail configurations.
type GuardrailRules struct {
	// NemoGuardrails configures the NeMo Guardrails server deployment.
	// +optional
	NemoGuardrails *NemoGuardrailsConfig `json:"nemoGuardrails,omitempty"`
	// InputGuard configures the input (request) guardrail plugin.
	// +optional
	InputGuard *GuardPluginConfig `json:"inputGuard,omitempty"`
	// OutputGuard configures the output (response) guardrail plugin.
	// +optional
	OutputGuard *GuardPluginConfig `json:"outputGuard,omitempty"`
}

// ManagedResourceRef references a managed sub-resource.
type ManagedResourceRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Ready     bool   `json:"ready"`
}

// ClusterGuardrailPolicySpec defines the desired state of ClusterGuardrailPolicy.
// +kubebuilder:validation:XValidation:rule="self.targetNamespace == oldSelf.targetNamespace",message="targetNamespace is immutable"
type ClusterGuardrailPolicySpec struct {
	// TargetRef identifies the Gateway to which this policy applies.
	TargetRef TargetRef `json:"targetRef"`
	// TargetNamespace is the namespace of the target Gateway.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="targetNamespace is immutable"
	TargetNamespace string `json:"targetNamespace"`
	// Guardrails defines the guardrail rules for this policy (shorthand for atomic defaults).
	// +optional
	Guardrails *GuardrailRules `json:"guardrails,omitempty"`
	// Defaults defines guardrail rules that can be overridden by namespace-level policies.
	// +optional
	Defaults *PolicyDefaults `json:"defaults,omitempty"`
	// Overrides defines guardrail rules that cannot be overridden by namespace-level policies.
	// +optional
	Overrides *PolicyOverrides `json:"overrides,omitempty"`
}

// ClusterGuardrailPolicyStatus defines the observed state of ClusterGuardrailPolicy.
type ClusterGuardrailPolicyStatus struct {
	// Phase reflects the overall lifecycle state.
	Phase string `json:"phase,omitempty"`
	// Conditions describes the state of the policy.
	// +optional
	Conditions []common.Condition `json:"conditions,omitempty"`
	// NemoGuardrailsRef references the managed NemoGuardrails CR.
	// +optional
	NemoGuardrailsRef *ManagedResourceRef `json:"nemoGuardrailsRef,omitempty"`
	// IPPConfigRefs references the managed IPP plugin ConfigMaps.
	// +optional
	IPPConfigRefs []ManagedResourceRef `json:"ippConfigRefs,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
//+kubebuilder:printcolumn:name="TargetKind",type=string,JSONPath=`.spec.targetRef.kind`
//+kubebuilder:printcolumn:name="TargetName",type=string,JSONPath=`.spec.targetRef.name`
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// ClusterGuardrailPolicy is the Schema for cluster-wide guardrail policy management.
type ClusterGuardrailPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterGuardrailPolicySpec   `json:"spec,omitempty"`
	Status ClusterGuardrailPolicyStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ClusterGuardrailPolicyList contains a list of ClusterGuardrailPolicy.
type ClusterGuardrailPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterGuardrailPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterGuardrailPolicy{}, &ClusterGuardrailPolicyList{})
}

/*
Copyright 2023.

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
	"maps"
	"slices"

	"github.com/trustyai-explainability/trustyai-service-operator/api/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// NemoConfig holds information related to a single configuration of the NeMo Guardrails server
type NemoConfig struct {
	// Name sets the id of this particular config within the NeMo Guardrails server. This will create a
	// directory called /app/config/$Name. Since it $Name will be used a directory, it must only
	// contain alphanumeric characters, dashes, and underscores. Consumers such as AIGuardrail reference
	// this value via checks[].configId, so it must always be set.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9_-]+$`
	Name string `json:"name"`
	//ConfigMaps is a list of configmaps that comprise the configuration. All files from these configmaps will be mounted within /app/config/$Name
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	ConfigMaps []string `json:"configMaps"`
	//The Default flag determines whether config is treated as the default config for the nemo-server. If no config is set to default, the first entry in NemoConfigs will be used as the default
	Default bool `json:"default,omitempty"`
}

// NemoGuardrailsSpec defines the desired state of NemoGuardrails
type NemoGuardrailsSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Names must be unique: they are the id consumers reference (AIGuardrail checks[].configId)
	// and they determine the /app/config/$Name mount path, so a duplicate is both an ambiguous
	// reference and a colliding mount.
	// MaxItems bounds the uniqueness rule below, which is quadratic and is rejected by the apiserver's
	// CEL cost estimator on an unbounded list.
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:XValidation:rule="self.all(c, self.exists_one(o, o.name == c.name))",message="nemoConfigs names must be unique"
	NemoConfigs    []NemoConfig           `json:"nemoConfigs"`
	CABundleConfig *common.CABundleConfig `json:"caBundleConfig,omitempty"`
	// Number of replicas for the NeMo Guardrails deployment
	// +optional
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	Replicas *int32 `json:"replicas,omitempty"`
	// Define Env information for the main container
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`
	// Resources defines CPU/memory requests and limits and DRA claim references
	// for the main NeMo Guardrails container.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
	// Template describes the pod template used by the deployment
	// +optional
	Template *NemoGuardrailsTemplate `json:"template,omitempty"`
	// ExposeRoute controls whether an external route to the NeMo Guardrails server is created. Defaults to true.
	// +optional
	// +kubebuilder:default=true
	ExposeRoute *bool `json:"exposeRoute,omitempty"`
	// AllowedConsumers authorizes which namespaces may attach a consumer to this server.
	// The only valid consumer type is a MaaS AIGuardrail resource.
	// When omitted, only a MaaS AIGuardrail in the same namespace as this CR may attach to it.
	// +optional
	AllowedConsumers *AllowedConsumers `json:"allowedConsumers,omitempty"`
	// NetworkPolicy configures additional callers. The operand NetworkPolicy is
	// always reconciled; it cannot be disabled.
	// +optional
	NetworkPolicy *NemoNetworkPolicy `json:"networkPolicy,omitempty"`
}

// NemoNetworkPolicy extends the built-in MaaS payload-processing, gateway and
// Route ingress rules of the operand NetworkPolicy.
type NemoNetworkPolicy struct {
	// AdditionalIngress permits callers on the public Service listener only.
	// Namespace and Pod selectors are ANDed, and both must positively identify
	// callers: empty and exclusion-only selectors are rejected.
	// This does not authorize consumer attachments or bypass authentication,
	// and it never grants the authenticated admin listener.
	// +optional
	// +kubebuilder:validation:MaxItems=32
	AdditionalIngress []NemoIngressPeer `json:"additionalIngress,omitempty"`
}

// NemoIngressPeer identifies callers across namespaces, never relative to the CR.
// Both selectors must positively identify callers: a nonempty matchLabels value,
// an Exists expression, or an In expression with a nonempty value. The CRD rejects
// empty and exclusion-only selectors at admission, and a namespaceSelector may not
// rely on the universal Exists form of kubernetes.io/metadata.name. The controller
// revalidates the same constraints and additionally rejects empty-peer and
// universal-CIDR forms, failing closed without writing a policy; the CR reports
// that rejection through its NetworkPolicy condition.
type NemoIngressPeer struct {
	// NamespaceSelector selects the caller namespaces and is ANDed with
	// PodSelector. An Exists constraint on kubernetes.io/metadata.name matches every
	// namespace, so it is rejected unless a nonempty matchLabels value or an In
	// expression with a nonempty value also narrows the selector.
	// +kubebuilder:validation:XValidation:rule="(has(self.matchLabels) && self.matchLabels.exists(k, size(self.matchLabels[k]) > 0)) || (has(self.matchExpressions) && self.matchExpressions.exists(e, e.operator == 'In' && has(e.values) && e.values.exists(v, size(v) > 0))) || !(has(self.matchExpressions) && self.matchExpressions.exists(e, e.key == 'kubernetes.io/metadata.name' && e.operator == 'Exists'))",message="namespaceSelector may not rely on Exists kubernetes.io/metadata.name alone; add a nonempty matchLabels value or an In expression with a nonempty value"
	NamespaceSelector NemoLabelSelector `json:"namespaceSelector"`
	// PodSelector selects the caller Pods inside the selected namespaces.
	PodSelector NemoLabelSelector `json:"podSelector"`
}

// NemoLabelSelector is metav1.LabelSelector with bounded collections. These
// selectors need admission rules that inspect matchLabels values and
// matchExpressions, and the API server rejects a comprehension over an unbounded
// collection as over its CEL cost budget: every rule under additionalIngress is
// cost-multiplied by that list's maxItems. The wire shape is identical to
// metav1.LabelSelector. Use LabelSelector to build the canonical selector.
// +structType=atomic
// +kubebuilder:validation:XValidation:rule="(has(self.matchLabels) && self.matchLabels.exists(k, size(self.matchLabels[k]) > 0)) || (has(self.matchExpressions) && self.matchExpressions.exists(e, e.operator == 'Exists' || (e.operator == 'In' && has(e.values) && e.values.exists(v, size(v) > 0))))",message="selector requires a positive identity constraint: a nonempty matchLabels value, an Exists expression, or an In expression with a nonempty value"
type NemoLabelSelector struct {
	// MatchLabels is a map of {key,value} pairs. A single {key,value} in the
	// matchLabels map is equivalent to an element of matchExpressions, whose key
	// field is "key", the operator is "In", and the values array contains only
	// "value". The requirements are ANDed.
	// +optional
	// +kubebuilder:validation:MaxProperties=32
	MatchLabels map[string]string `json:"matchLabels,omitempty"`
	// MatchExpressions is a list of label selector requirements. The requirements
	// are ANDed.
	// +optional
	// +kubebuilder:validation:MaxItems=32
	// +listType=atomic
	MatchExpressions []NemoLabelSelectorRequirement `json:"matchExpressions,omitempty"`
}

// NemoLabelSelectorRequirement is metav1.LabelSelectorRequirement with a bounded
// values list, bounded for the same admission-rule cost reason as NemoLabelSelector.
type NemoLabelSelectorRequirement struct {
	// Key is the label key that the selector applies to.
	Key string `json:"key"`
	// Operator represents a key's relationship to a set of values. Valid operators
	// are In, NotIn, Exists and DoesNotExist.
	Operator metav1.LabelSelectorOperator `json:"operator"`
	// Values is an array of string values. If the operator is In or NotIn, the
	// values array must be non-empty. If the operator is Exists or DoesNotExist, the
	// values array must be empty.
	// +optional
	// +kubebuilder:validation:MaxItems=32
	// +listType=atomic
	Values []string `json:"values,omitempty"`
}

// LabelSelector returns a deep copy of the canonical Kubernetes selector used to
// build the operand NetworkPolicy and to revalidate the peer in the controller.
func (s NemoLabelSelector) LabelSelector() metav1.LabelSelector {
	selector := metav1.LabelSelector{MatchLabels: maps.Clone(s.MatchLabels)}
	for _, expression := range s.MatchExpressions {
		selector.MatchExpressions = append(selector.MatchExpressions, metav1.LabelSelectorRequirement{
			Key:      expression.Key,
			Operator: expression.Operator,
			Values:   slices.Clone(expression.Values),
		})
	}
	return selector
}

// NemoGuardrailsTemplate defines the template for the NemoGuardrails deployment
type NemoGuardrailsTemplate struct {
	// Pod defines pod-level scheduling constraints
	// +optional
	Pod *NemoGuardrailsPodTemplate `json:"pod,omitempty"`
}

// NemoGuardrailsPodTemplate defines pod-level overrides
type NemoGuardrailsPodTemplate struct {
	// ResourceClaims declares the DRA claims available to this Pod. Each entry
	// references a ResourceClaim or ResourceClaimTemplate in the instance's namespace.
	// The main container opts in by referencing the entry's name in spec.resources.claims.
	// Changes update the Deployment's Pod template and trigger a rollout.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:items:XValidation:rule="has(self.resourceClaimName) != has(self.resourceClaimTemplateName)",message="exactly one of resourceClaimName and resourceClaimTemplateName must be set"
	// +kubebuilder:validation:items:XValidation:rule="!has(self.resourceClaimName) || size(self.resourceClaimName) > 0",message="resourceClaimName must not be empty"
	// +kubebuilder:validation:items:XValidation:rule="!has(self.resourceClaimTemplateName) || size(self.resourceClaimTemplateName) > 0",message="resourceClaimTemplateName must not be empty"
	ResourceClaims []corev1.PodResourceClaim `json:"resourceClaims,omitempty"`
	// If specified, the pod's scheduling constraints
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
	// If specified, the pod's tolerations
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// NodeSelector is a map of key-value pairs for node scheduling
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
}

// AllowedConsumers is the NeMo Guardrails server-owned permission for consumer attachments.
type AllowedConsumers struct {
	// Namespaces selects namespaces whose consumer resources may attach to this server.
	// When omitted, only the NemoGuardrails CR's namespace is allowed.
	// +optional
	Namespaces *ConsumerNamespaces `json:"namespaces,omitempty"`
}

// ConsumerNamespaceFrom selects how consumer namespaces are authorized.
type ConsumerNamespaceFrom string

const (
	// ConsumerNamespaceFromSame allows only the NemoGuardrails CR's namespace.
	ConsumerNamespaceFromSame ConsumerNamespaceFrom = "Same"
	// ConsumerNamespaceFromSelector allows namespaces whose labels match Selector.
	// The NemoGuardrails CR namespace must match the selector too. The selector is ver
	ConsumerNamespaceFromSelector ConsumerNamespaceFrom = "Selector"
	// ConsumerNamespaceFromAll allows any namespace.
	ConsumerNamespaceFromAll ConsumerNamespaceFrom = "All"
)

// ConsumerNamespaces is the namespace scope of allowed consumers.
// +kubebuilder:validation:XValidation:rule="(has(self.from) && self.from == 'Selector') ? has(self.selector) : !has(self.selector)",message="selector is required when from is Selector and must be omitted when from is Same or All"
// +kubebuilder:validation:XValidation:rule="!has(self.from) || self.from != 'Selector' || (has(self.selector.matchLabels) && size(self.selector.matchLabels) > 0) || (has(self.selector.matchExpressions) && size(self.selector.matchExpressions) > 0)",message="from Selector requires a nonempty selector with at least one matchLabels entry or matchExpressions requirement"
// +kubebuilder:validation:XValidation:rule="!has(self.selector) || !has(self.selector.matchExpressions) || self.selector.matchExpressions.all(e, size(e.key) > 0 && e.operator in ['In', 'NotIn', 'Exists', 'DoesNotExist'] && ((e.operator in ['In', 'NotIn']) ? (has(e.values) && size(e.values) > 0) : (!has(e.values) || size(e.values) == 0)))",message="matchExpressions operator must be In, NotIn, Exists, or DoesNotExist; In and NotIn require nonempty values, and Exists and DoesNotExist require values to be omitted or empty"
type ConsumerNamespaces struct {
	// From selects how consumer namespaces are authorized.
	// Same allows only the NemoGuardrails CR namespace. Selector matches Namespace labels.
	// All allows any namespace. Defaults to Same.
	// +optional
	// +kubebuilder:default=Same
	// +kubebuilder:validation:Enum=Same;Selector;All
	From ConsumerNamespaceFrom `json:"from,omitempty"`
	// Selector is matched against the consumer Namespace's labels.
	// Required when From is Selector. Forbidden when From is Same or All.
	// An empty selector is rejected; use All to allow every namespace.
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

type CAStatus struct {
	ODHTrustedCAFound       bool   `json:"odhTrustedCAFound"`
	ODHTrustedCAError       string `json:"odhTrustedCAError,omitempty"`
	OpenshiftServingCAFound bool   `json:"openshiftServingCAFound"`
	OpenshiftServingCAError string `json:"openshiftServingCAError,omitempty"`
	UserCAFound             bool   `json:"userCAFound,omitempty"`
	UserCAError             string `json:"userCAError,omitempty"`
}

// NemoGuardrailStatus defines the observed state of NemoGuardrails
type NemoGuardrailStatus struct {
	Phase string `json:"phase,omitempty"`

	// Conditions describes the state of the NemoGuardrails resource.
	// +optional
	Conditions []common.Condition `json:"conditions,omitempty"`
	// CA describes the status of the CA configmaps
	// +optional
	CA *CAStatus `json:"ca,omitempty"`
	// Endpoint is the internal Service URL for the NeMo Guardrails server.
	// Only set when the server is protected by authentication (security.opendatahub.io/enable-auth: "true").
	// +optional
	Endpoint string `json:"endpoint,omitempty"`
	// WorkloadNamespace is the namespace where the Deployment was last reconciled.
	// +optional
	WorkloadNamespace string `json:"workloadNamespace,omitempty"`
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// NemoGuardrails is the Schema for the nemoguardrails API
type NemoGuardrails struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NemoGuardrailsSpec  `json:"spec,omitempty"`
	Status NemoGuardrailStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// NemoGuardrailsList contains a list of NemoGuardrails
type NemoGuardrailsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NemoGuardrails `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NemoGuardrails{}, &NemoGuardrailsList{})
}

package utils

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation"
)

// NetworkPolicyEgressMode has no implicit default.
type NetworkPolicyEgressMode string

const (
	NetworkPolicyDenyAll    NetworkPolicyEgressMode = "DenyAll"
	NetworkPolicyRestricted NetworkPolicyEgressMode = "Restricted"
	NetworkPolicyAllowAll   NetworkPolicyEgressMode = "AllowAll"
)

// NetworkPolicyEgressIntent comes from trusted controller configuration, never
// from annotations read off an existing policy. AllowAll is not exfiltration protection.
type NetworkPolicyEgressIntent struct {
	Mode         NetworkPolicyEgressMode
	Rationale    string
	ResidualRisk string
}

// NetworkPolicyNamespaceOnlyPeerIntent is an explicit review record for a
// namespace-wide ingress peer. It does not authorize the peer by itself: the
// selector must exactly match a namespace-only peer in the desired policy.
type NetworkPolicyNamespaceOnlyPeerIntent struct {
	NamespaceSelector metav1.LabelSelector `json:"namespaceSelector"`
	Rationale         string               `json:"rationale"`
	ResidualRisk      string               `json:"residualRisk"`
}

// NetworkPolicyIngressIntent carries trusted exceptions to the default rule
// that ingress peers require a Pod selector. Namespace-only peers are broad and
// must be explicitly listed with rationale and residual risk.
type NetworkPolicyIngressIntent struct {
	NamespaceOnlyPeers []NetworkPolicyNamespaceOnlyPeerIntent
}

func positiveSelector(selector *metav1.LabelSelector) error {
	if selector == nil {
		return fmt.Errorf("selector is required")
	}
	if _, err := metav1.LabelSelectorAsSelector(selector); err != nil {
		return err
	}
	// LabelSelectorAsSelector does not fully validate label keys/values.
	if errs := metav1validation.ValidateLabelSelector(selector, metav1validation.LabelSelectorValidationOptions{}, nil); len(errs) > 0 {
		return fmt.Errorf("invalid selector: %v", errs)
	}
	for _, value := range selector.MatchLabels {
		if value != "" {
			return nil
		}
	}
	for _, expr := range selector.MatchExpressions {
		if expr.Operator == metav1.LabelSelectorOpExists {
			return nil
		}
		if expr.Operator == metav1.LabelSelectorOpIn && len(expr.Values) > 0 {
			for _, value := range expr.Values {
				if value != "" {
					return nil
				}
			}
		}
	}
	return fmt.Errorf("selector requires a positive nonempty identity constraint")
}

const namespaceNameLabel = "kubernetes.io/metadata.name"

// positiveNamespaceSelector rejects the Kubernetes-provided namespace name
// label's universal Exists form while preserving useful Exists selectors on
// workload/tenant labels.
func positiveNamespaceSelector(selector *metav1.LabelSelector) error {
	if err := positiveSelector(selector); err != nil {
		return err
	}

	// A non-empty equality or finite In requirement narrows the selector. An
	// Exists requirement on kubernetes.io/metadata.name alone does not: that
	// label is present on every namespace in supported Kubernetes versions.
	for _, value := range selector.MatchLabels {
		if value != "" {
			return nil
		}
	}
	for _, expr := range selector.MatchExpressions {
		if expr.Operator == metav1.LabelSelectorOpIn {
			for _, value := range expr.Values {
				if value != "" {
					return nil
				}
			}
		}
	}
	for _, expr := range selector.MatchExpressions {
		if expr.Key == namespaceNameLabel && expr.Operator == metav1.LabelSelectorOpExists {
			return fmt.Errorf("namespace selector uses universal Exists constraint on %q", namespaceNameLabel)
		}
	}
	return nil
}

func namespaceSelectorIntentKey(selector metav1.LabelSelector) (string, error) {
	data, err := json.Marshal(selector)
	return string(data), err
}

func validateNetworkPolicyIngressIntent(intent *NetworkPolicyIngressIntent) (map[string]NetworkPolicyNamespaceOnlyPeerIntent, error) {
	allowed := make(map[string]NetworkPolicyNamespaceOnlyPeerIntent)
	if intent == nil {
		return allowed, nil
	}
	if len(intent.NamespaceOnlyPeers) == 0 {
		return nil, fmt.Errorf("ingress intent must contain at least one explicit namespace-only peer")
	}
	for _, exception := range intent.NamespaceOnlyPeers {
		if err := positiveNamespaceSelector(&exception.NamespaceSelector); err != nil {
			return nil, fmt.Errorf("namespace-only peer exception selector: %w", err)
		}
		if strings.TrimSpace(exception.Rationale) == "" || strings.TrimSpace(exception.ResidualRisk) == "" {
			return nil, fmt.Errorf("namespace-only peer exception requires rationale and residual risk")
		}
		key, err := namespaceSelectorIntentKey(exception.NamespaceSelector)
		if err != nil {
			return nil, fmt.Errorf("encode namespace-only peer selector: %w", err)
		}
		if _, exists := allowed[key]; exists {
			return nil, fmt.Errorf("duplicate namespace-only peer exception")
		}
		allowed[key] = exception
	}
	return allowed, nil
}

func validatePolicyPeerWithIngressIntent(peer networkingv1.NetworkPolicyPeer, allowedNamespaceOnlyPeers map[string]NetworkPolicyNamespaceOnlyPeerIntent) (string, error) {
	if peer.IPBlock != nil {
		if peer.PodSelector != nil || peer.NamespaceSelector != nil {
			return "", fmt.Errorf("IPBlock cannot be combined with selectors")
		}
		prefix, err := netip.ParsePrefix(peer.IPBlock.CIDR)
		if err != nil || prefix.Bits() == 0 {
			return "", fmt.Errorf("invalid or universal CIDR %q", peer.IPBlock.CIDR)
		}
		prefix = prefix.Masked()
		for _, exception := range peer.IPBlock.Except {
			child, err := netip.ParsePrefix(exception)
			if err != nil || child.Addr().BitLen() != prefix.Addr().BitLen() || child.Bits() <= prefix.Bits() || !prefix.Contains(child.Masked().Addr()) {
				return "", fmt.Errorf("exception %q is not a strict subprefix of %s", exception, prefix)
			}
		}
		return "", nil
	}
	if peer.PodSelector == nil {
		if peer.NamespaceSelector == nil {
			return "", fmt.Errorf("peer requires a pod selector, namespace selector exception, or IPBlock")
		}
		if err := positiveNamespaceSelector(peer.NamespaceSelector); err != nil {
			return "", fmt.Errorf("namespace-only peer: %w", err)
		}
		key, err := namespaceSelectorIntentKey(*peer.NamespaceSelector)
		if err != nil {
			return "", fmt.Errorf("encode namespace-only peer selector: %w", err)
		}
		if _, ok := allowedNamespaceOnlyPeers[key]; !ok {
			return "", fmt.Errorf("namespace-only peer requires an exact typed ingress exception")
		}
		return key, nil
	}
	if err := positiveSelector(peer.PodSelector); err != nil {
		return "", fmt.Errorf("pod peer: %w", err)
	}
	if peer.NamespaceSelector != nil {
		if err := positiveNamespaceSelector(peer.NamespaceSelector); err != nil {
			return "", fmt.Errorf("namespace peer: %w", err)
		}
	}
	return "", nil
}

func validatePolicyPeer(peer networkingv1.NetworkPolicyPeer) error {
	_, err := validatePolicyPeerWithIngressIntent(peer, nil)
	return err
}

func validatePolicyPorts(ports []networkingv1.NetworkPolicyPort) error {
	if len(ports) == 0 {
		return fmt.Errorf("allow rule requires explicit ports")
	}
	for _, p := range ports {
		if p.Protocol != nil && *p.Protocol != corev1.ProtocolTCP && *p.Protocol != corev1.ProtocolUDP && *p.Protocol != corev1.ProtocolSCTP {
			return fmt.Errorf("invalid protocol")
		}
		if p.Port == nil {
			return fmt.Errorf("all-port grants are forbidden")
		}
		switch p.Port.Type {
		case intstr.Int:
			if p.Port.IntVal < 1 || p.Port.IntVal > 65535 {
				return fmt.Errorf("invalid port number")
			}
			if p.EndPort != nil && (*p.EndPort < p.Port.IntVal || *p.EndPort > 65535) {
				return fmt.Errorf("invalid endPort")
			}
		case intstr.String:
			if len(validation.IsValidPortName(p.Port.StrVal)) > 0 || p.EndPort != nil {
				return fmt.Errorf("invalid named port or named-port range")
			}
		default:
			return fmt.Errorf("invalid port type")
		}
	}
	return nil
}

func policyDirections(policy *networkingv1.NetworkPolicy) (bool, bool, error) {
	ingress, egress := false, false
	for _, direction := range policy.Spec.PolicyTypes {
		switch direction {
		case networkingv1.PolicyTypeIngress:
			if ingress {
				return false, false, fmt.Errorf("duplicate Ingress direction")
			}
			ingress = true
		case networkingv1.PolicyTypeEgress:
			if egress {
				return false, false, fmt.Errorf("duplicate Egress direction")
			}
			egress = true
		default:
			return false, false, fmt.Errorf("unknown policy direction")
		}
	}
	if !ingress && !egress {
		return false, false, fmt.Errorf("explicit policyTypes are required")
	}
	return ingress, egress, nil
}

// ValidateWorkloadNetworkPolicy validates one directional policy. An ingress-only
// policy is valid here, but does not satisfy ValidateWorkloadNetworkPolicySet.
func ValidateWorkloadNetworkPolicy(policy *networkingv1.NetworkPolicy, identity NetworkPolicyIdentity, egressIntent *NetworkPolicyEgressIntent) error {
	return ValidateWorkloadNetworkPolicyWithIngressIntent(policy, identity, egressIntent, nil)
}

// ValidateWorkloadNetworkPolicyWithIngressIntent additionally validates the
// controller-supplied namespace-only ingress peer exceptions.
func ValidateWorkloadNetworkPolicyWithIngressIntent(policy *networkingv1.NetworkPolicy, identity NetworkPolicyIdentity, egressIntent *NetworkPolicyEgressIntent, ingressIntent *NetworkPolicyIngressIntent) error {
	if policy == nil {
		return fmt.Errorf("policy is required")
	}
	allowedNamespaceOnlyPeers, err := validateNetworkPolicyIngressIntent(ingressIntent)
	if err != nil {
		return err
	}
	expected, err := identity.Labels()
	if err != nil {
		return err
	}
	if err := positiveSelector(&policy.Spec.PodSelector); err != nil {
		return fmt.Errorf("target selector: %w", err)
	}
	for key, value := range expected {
		if policy.Spec.PodSelector.MatchLabels[key] != value {
			return fmt.Errorf("target selector lacks %s identity", key)
		}
	}
	selector, err := metav1.LabelSelectorAsSelector(&policy.Spec.PodSelector)
	if err != nil {
		return fmt.Errorf("target selector: %w", err)
	}
	if !selector.Matches(labels.Set(expected)) {
		return fmt.Errorf("target selector does not select its own identity")
	}
	ingress, egress, err := policyDirections(policy)
	if err != nil {
		return err
	}
	if !ingress && len(policy.Spec.Ingress) != 0 {
		return fmt.Errorf("ingress rules without Ingress direction")
	}
	if !egress && (len(policy.Spec.Egress) != 0 || egressIntent != nil) {
		return fmt.Errorf("egress intent/rules without Egress direction")
	}
	if !ingress && ingressIntent != nil {
		return fmt.Errorf("ingress intent without Ingress direction")
	}
	usedNamespaceOnlyPeers := make(map[string]bool, len(allowedNamespaceOnlyPeers))
	for _, rule := range policy.Spec.Ingress {
		if len(rule.From) == 0 {
			return fmt.Errorf("ingress allow requires peers")
		}
		if err := validatePolicyPorts(rule.Ports); err != nil {
			return err
		}
		for _, peer := range rule.From {
			key, err := validatePolicyPeerWithIngressIntent(peer, allowedNamespaceOnlyPeers)
			if err != nil {
				return err
			}
			if key != "" {
				usedNamespaceOnlyPeers[key] = true
			}
		}
	}
	if len(usedNamespaceOnlyPeers) != len(allowedNamespaceOnlyPeers) {
		return fmt.Errorf("ingress intent contains an unused namespace-only peer exception")
	}
	if !egress {
		return nil
	}
	if egressIntent == nil {
		return fmt.Errorf("Egress direction requires controller-supplied intent")
	}
	switch egressIntent.Mode {
	case NetworkPolicyDenyAll:
		if len(policy.Spec.Egress) != 0 {
			return fmt.Errorf("DenyAll cannot have allow rules")
		}
	case NetworkPolicyRestricted:
		if len(policy.Spec.Egress) == 0 {
			return fmt.Errorf("Restricted requires allow rules")
		}
		for _, rule := range policy.Spec.Egress {
			if len(rule.To) == 0 {
				return fmt.Errorf("Restricted requires destinations")
			}
			if err := validatePolicyPorts(rule.Ports); err != nil {
				return err
			}
			for _, peer := range rule.To {
				if err := validatePolicyPeer(peer); err != nil {
					return err
				}
			}
		}
	case NetworkPolicyAllowAll:
		if strings.TrimSpace(egressIntent.Rationale) == "" || strings.TrimSpace(egressIntent.ResidualRisk) == "" {
			return fmt.Errorf("AllowAll requires rationale and residual risk")
		}
		if len(policy.Spec.Egress) != 1 || len(policy.Spec.Egress[0].To) != 0 || len(policy.Spec.Egress[0].Ports) != 0 {
			return fmt.Errorf("AllowAll requires exactly one empty rule")
		}
	default:
		return fmt.Errorf("explicit known egress mode is required")
	}
	return nil
}

// WorkloadNetworkPolicy associates an authored policy with trusted ingress/egress intent.
type WorkloadNetworkPolicy struct {
	Policy  *networkingv1.NetworkPolicy
	Ingress *NetworkPolicyIngressIntent
	Egress  *NetworkPolicyEgressIntent
}

// ValidateWorkloadNetworkPolicySet requires both directions for one exact target
// selector in one namespace. It is not a runtime audit of all selecting policies.
func ValidateWorkloadNetworkPolicySet(policies []WorkloadNetworkPolicy, identity NetworkPolicyIdentity) error {
	ingress, egress := false, false
	var target, namespace string
	var egressMode NetworkPolicyEgressMode
	seen := map[string]bool{}
	for index, item := range policies {
		if err := ValidateWorkloadNetworkPolicyWithIngressIntent(item.Policy, identity, item.Egress, item.Ingress); err != nil {
			return err
		}
		key := item.Policy.Namespace + "/" + item.Policy.Name
		if item.Policy.Name == "" || item.Policy.GenerateName != "" || len(validation.IsDNS1123Subdomain(item.Policy.Name)) > 0 || seen[key] {
			return fmt.Errorf("policy set requires unique explicit valid names")
		}
		seen[key] = true
		selector, _ := metav1.LabelSelectorAsSelector(&item.Policy.Spec.PodSelector)
		if index == 0 {
			target, namespace = selector.String(), item.Policy.Namespace
		}
		if namespace == "" || item.Policy.Namespace != namespace || selector.String() != target {
			return fmt.Errorf("policy set must share namespace and exact target selector")
		}
		hasIngress, hasEgress, _ := policyDirections(item.Policy)
		if hasEgress {
			if egressMode != "" && egressMode != item.Egress.Mode {
				return fmt.Errorf("policy set mixes conflicting egress intents")
			}
			egressMode = item.Egress.Mode
		}
		ingress = ingress || hasIngress
		egress = egress || hasEgress
	}
	if !ingress || !egress {
		return fmt.Errorf("workload requires explicit ingress and egress coverage")
	}
	return nil
}

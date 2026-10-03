package utils

import (
	"fmt"
	"net/netip"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
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

func validatePolicyPeer(peer networkingv1.NetworkPolicyPeer) error {
	if peer.IPBlock != nil {
		if peer.PodSelector != nil || peer.NamespaceSelector != nil {
			return fmt.Errorf("IPBlock cannot be combined with selectors")
		}
		prefix, err := netip.ParsePrefix(peer.IPBlock.CIDR)
		if err != nil || prefix.Bits() == 0 {
			return fmt.Errorf("invalid or universal CIDR %q", peer.IPBlock.CIDR)
		}
		prefix = prefix.Masked()
		for _, exception := range peer.IPBlock.Except {
			child, err := netip.ParsePrefix(exception)
			if err != nil || child.Addr().BitLen() != prefix.Addr().BitLen() || child.Bits() <= prefix.Bits() || !prefix.Contains(child.Masked().Addr()) {
				return fmt.Errorf("exception %q is not a strict subprefix of %s", exception, prefix)
			}
		}
		return nil
	}
	// Dedicated-namespace broad grants require a separately reviewed interface;
	// this foundation deliberately does not infer namespace trust from a label.
	if err := positiveSelector(peer.PodSelector); err != nil {
		return fmt.Errorf("pod peer: %w", err)
	}
	if peer.NamespaceSelector != nil {
		return positiveSelector(peer.NamespaceSelector)
	}
	return nil
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
// Nil/empty DenyAll slices are equivalent after API serialization; typed intent
// and explicit Egress policyTypes retain the authored deny intent.
func ValidateWorkloadNetworkPolicy(policy *networkingv1.NetworkPolicy, identity NetworkPolicyIdentity, intent *NetworkPolicyEgressIntent) error {
	if policy == nil {
		return fmt.Errorf("policy is required")
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
	ingress, egress, err := policyDirections(policy)
	if err != nil {
		return err
	}
	if !ingress && len(policy.Spec.Ingress) != 0 {
		return fmt.Errorf("ingress rules without Ingress direction")
	}
	if !egress && (len(policy.Spec.Egress) != 0 || intent != nil) {
		return fmt.Errorf("egress intent/rules without Egress direction")
	}
	for _, rule := range policy.Spec.Ingress {
		if len(rule.From) == 0 {
			return fmt.Errorf("ingress allow requires peers")
		}
		if err := validatePolicyPorts(rule.Ports); err != nil {
			return err
		}
		for _, peer := range rule.From {
			if err := validatePolicyPeer(peer); err != nil {
				return err
			}
		}
	}
	if !egress {
		return nil
	}
	if intent == nil {
		return fmt.Errorf("Egress direction requires controller-supplied intent")
	}
	switch intent.Mode {
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
		if strings.TrimSpace(intent.Rationale) == "" || strings.TrimSpace(intent.ResidualRisk) == "" {
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

// WorkloadNetworkPolicy associates an authored policy with trusted egress intent.
type WorkloadNetworkPolicy struct {
	Policy *networkingv1.NetworkPolicy
	Egress *NetworkPolicyEgressIntent
}

// ValidateWorkloadNetworkPolicySet requires both directions for one exact target
// selector in one namespace. It is not a runtime audit of all selecting policies.
func ValidateWorkloadNetworkPolicySet(policies []WorkloadNetworkPolicy, identity NetworkPolicyIdentity) error {
	ingress, egress := false, false
	var target, namespace string
	var egressMode NetworkPolicyEgressMode
	seen := map[string]bool{}
	for index, item := range policies {
		if err := ValidateWorkloadNetworkPolicy(item.Policy, identity, item.Egress); err != nil {
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

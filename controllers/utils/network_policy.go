package utils

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/netip"
	"strings"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	// NetworkPolicyManagedByLabel identifies policies reconciled by this operator.
	NetworkPolicyManagedByLabel = "app.kubernetes.io/managed-by"
	// NetworkPolicyManagedByValue is the value used for NetworkPolicyManagedByLabel.
	NetworkPolicyManagedByValue = "trustyai-service-operator"
	// NetworkPolicyOwnerUIDLabel identifies the owning TrustyAI resource without
	// relying on a user-selected name. It is useful for explicit cleanup where a
	// same-namespace owner reference is not valid.
	NetworkPolicyOwnerUIDLabel = "trustyai.opendatahub.io/network-policy-owner-uid"
)

// NetworkPolicyName returns a deterministic, DNS-label-safe policy name for a
// workload name. A hash reduces collisions when distinct workload names
// normalize to the same slug or need truncation.
func NetworkPolicyName(workloadName string) (string, error) {
	if strings.TrimSpace(workloadName) == "" {
		return "", fmt.Errorf("workload name must not be empty")
	}

	var slug strings.Builder
	lastWasDash := false
	for _, r := range strings.ToLower(workloadName) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			slug.WriteRune(r)
			lastWasDash = false
			continue
		}
		if !lastWasDash && slug.Len() > 0 {
			slug.WriteByte('-')
			lastWasDash = true
		}
	}
	nameSlug := strings.Trim(slug.String(), "-")
	if nameSlug == "" {
		nameSlug = "workload"
	}
	const maxSlugLength = 43 // leaves room for "-np-" and a 16-character hash
	if len(nameSlug) > maxSlugLength {
		nameSlug = strings.TrimRight(nameSlug[:maxSlugLength], "-")
	}

	digest := sha256.Sum256([]byte(workloadName))
	name := fmt.Sprintf("%s-np-%x", nameSlug, digest[:8])
	if problems := validation.IsDNS1123Label(name); len(problems) > 0 {
		return "", fmt.Errorf("generated NetworkPolicy name %q is invalid: %s", name, strings.Join(problems, "; "))
	}
	return name, nil
}

// NetworkPolicyOwnerLabels returns standard management and owner-identity
// labels for an operator-owned NetworkPolicy. A persisted owner UID is required
// so cross-namespace cleanup cannot accidentally match a later object with the
// same name.
func NetworkPolicyOwnerLabels(owner client.Object) (map[string]string, error) {
	if owner == nil {
		return nil, fmt.Errorf("NetworkPolicy owner must not be nil")
	}
	if owner.GetUID() == "" {
		return nil, fmt.Errorf("NetworkPolicy owner %s/%s has no UID", owner.GetNamespace(), owner.GetName())
	}
	return map[string]string{
		NetworkPolicyManagedByLabel: NetworkPolicyManagedByValue,
		NetworkPolicyOwnerUIDLabel:  string(owner.GetUID()),
	}, nil
}

// SetNetworkPolicyOwnerReference sets a controller owner reference only when
// the owner and NetworkPolicy are in the same namespace. Cross-namespace
// policies must use explicit lifecycle mapping and cleanup instead.
func SetNetworkPolicyOwnerReference(policy *networkingv1.NetworkPolicy, owner client.Object, scheme *runtime.Scheme) error {
	if policy == nil {
		return fmt.Errorf("NetworkPolicy must not be nil")
	}
	if owner == nil {
		return fmt.Errorf("NetworkPolicy owner must not be nil")
	}
	if policy.Namespace == "" || owner.GetNamespace() == "" {
		return fmt.Errorf("NetworkPolicy and owner must both be namespaced")
	}
	if policy.Namespace != owner.GetNamespace() {
		return fmt.Errorf("cannot set a cross-namespace owner reference from %s/%s to NetworkPolicy %s/%s", owner.GetNamespace(), owner.GetName(), policy.Namespace, policy.Name)
	}
	if owner.GetUID() == "" {
		return fmt.Errorf("NetworkPolicy owner %s/%s has no UID", owner.GetNamespace(), owner.GetName())
	}
	if scheme == nil {
		return fmt.Errorf("scheme must not be nil")
	}
	return controllerutil.SetControllerReference(owner, policy, scheme)
}

// ValidateNetworkPolicy rejects incomplete or broad rule definitions before a
// controller submits them to the API server. An ingress or egress deny policy
// may have no rules; every allow rule must identify both peers and ports.
func ValidateNetworkPolicy(policy *networkingv1.NetworkPolicy) error {
	if policy == nil {
		return fmt.Errorf("NetworkPolicy must not be nil")
	}
	if problems := validation.IsDNS1123Subdomain(policy.Name); len(problems) > 0 {
		return fmt.Errorf("invalid NetworkPolicy name %q: %s", policy.Name, strings.Join(problems, "; "))
	}
	if problems := validation.IsDNS1123Label(policy.Namespace); len(problems) > 0 {
		return fmt.Errorf("invalid NetworkPolicy namespace %q: %s", policy.Namespace, strings.Join(problems, "; "))
	}
	if len(policy.Spec.PodSelector.MatchLabels) == 0 && len(policy.Spec.PodSelector.MatchExpressions) == 0 {
		return fmt.Errorf("NetworkPolicy %s/%s must have a non-empty pod selector", policy.Namespace, policy.Name)
	}
	if len(policy.Spec.PolicyTypes) == 0 {
		return fmt.Errorf("NetworkPolicy %s/%s must declare policyTypes explicitly", policy.Namespace, policy.Name)
	}

	policyTypes := make(map[networkingv1.PolicyType]struct{}, len(policy.Spec.PolicyTypes))
	for _, policyType := range policy.Spec.PolicyTypes {
		if policyType != networkingv1.PolicyTypeIngress && policyType != networkingv1.PolicyTypeEgress {
			return fmt.Errorf("NetworkPolicy %s/%s has unsupported policy type %q", policy.Namespace, policy.Name, policyType)
		}
		if _, exists := policyTypes[policyType]; exists {
			return fmt.Errorf("NetworkPolicy %s/%s repeats policy type %q", policy.Namespace, policy.Name, policyType)
		}
		policyTypes[policyType] = struct{}{}
	}
	if len(policy.Spec.Ingress) > 0 {
		if _, ok := policyTypes[networkingv1.PolicyTypeIngress]; !ok {
			return fmt.Errorf("NetworkPolicy %s/%s has ingress rules without Ingress in policyTypes", policy.Namespace, policy.Name)
		}
	}
	if len(policy.Spec.Egress) > 0 {
		if _, ok := policyTypes[networkingv1.PolicyTypeEgress]; !ok {
			return fmt.Errorf("NetworkPolicy %s/%s has egress rules without Egress in policyTypes", policy.Namespace, policy.Name)
		}
	}

	for i, rule := range policy.Spec.Ingress {
		if len(rule.From) == 0 {
			return fmt.Errorf("NetworkPolicy %s/%s ingress rule %d has no peers", policy.Namespace, policy.Name, i)
		}
		if err := validateNetworkPolicyPorts(rule.Ports, fmt.Sprintf("ingress rule %d", i)); err != nil {
			return fmt.Errorf("NetworkPolicy %s/%s: %w", policy.Namespace, policy.Name, err)
		}
		for j, peer := range rule.From {
			if err := validateNetworkPolicyPeer(peer); err != nil {
				return fmt.Errorf("NetworkPolicy %s/%s ingress rule %d peer %d: %w", policy.Namespace, policy.Name, i, j, err)
			}
		}
	}
	for i, rule := range policy.Spec.Egress {
		if len(rule.To) == 0 {
			return fmt.Errorf("NetworkPolicy %s/%s egress rule %d has no destinations", policy.Namespace, policy.Name, i)
		}
		if err := validateNetworkPolicyPorts(rule.Ports, fmt.Sprintf("egress rule %d", i)); err != nil {
			return fmt.Errorf("NetworkPolicy %s/%s: %w", policy.Namespace, policy.Name, err)
		}
		for j, peer := range rule.To {
			if err := validateNetworkPolicyPeer(peer); err != nil {
				return fmt.Errorf("NetworkPolicy %s/%s egress rule %d destination %d: %w", policy.Namespace, policy.Name, i, j, err)
			}
		}
	}
	return nil
}

func validateNetworkPolicyPorts(ports []networkingv1.NetworkPolicyPort, rule string) error {
	if len(ports) == 0 {
		return fmt.Errorf("%s has no ports", rule)
	}
	for i, port := range ports {
		if port.Protocol == nil {
			return fmt.Errorf("%s port %d must declare a protocol", rule, i)
		}
		if *port.Protocol != "TCP" && *port.Protocol != "UDP" && *port.Protocol != "SCTP" {
			return fmt.Errorf("%s port %d has unsupported protocol %q", rule, i, *port.Protocol)
		}
		if port.Port == nil {
			return fmt.Errorf("%s port %d must declare a port", rule, i)
		}
		switch port.Port.Type {
		case intstr.Int:
			if port.Port.IntVal < 1 || port.Port.IntVal > 65535 {
				return fmt.Errorf("%s port %d has an invalid numeric port", rule, i)
			}
		case intstr.String:
			if problems := validation.IsValidPortName(port.Port.StrVal); len(problems) > 0 {
				return fmt.Errorf("%s port %d has an invalid named port: %s", rule, i, strings.Join(problems, "; "))
			}
		default:
			return fmt.Errorf("%s port %d has an unsupported port type", rule, i)
		}
		if port.EndPort != nil {
			if port.Port.Type != intstr.Int || *port.EndPort <= port.Port.IntVal || *port.EndPort > 65535 {
				return fmt.Errorf("%s port %d has an invalid endPort range", rule, i)
			}
		}
	}
	return nil
}

func validateNetworkPolicyPeer(peer networkingv1.NetworkPolicyPeer) error {
	if peer.IPBlock != nil {
		if peer.PodSelector != nil || peer.NamespaceSelector != nil {
			return fmt.Errorf("IPBlock cannot be combined with pod or namespace selectors")
		}
		prefix, err := netip.ParsePrefix(peer.IPBlock.CIDR)
		if err != nil {
			return fmt.Errorf("invalid IPBlock CIDR %q: %w", peer.IPBlock.CIDR, err)
		}
		if prefix.Bits() == 0 {
			return fmt.Errorf("IPBlock CIDR %q is unrestricted", peer.IPBlock.CIDR)
		}
		prefix = prefix.Masked()
		for _, exceptCIDR := range peer.IPBlock.Except {
			exception, err := netip.ParsePrefix(exceptCIDR)
			if err != nil {
				return fmt.Errorf("invalid IPBlock exception CIDR %q: %w", exceptCIDR, err)
			}
			exception = exception.Masked()
			if !prefix.Contains(exception.Addr()) || exception.Bits() < prefix.Bits() {
				return fmt.Errorf("IPBlock exception CIDR %q is not contained within parent CIDR %q", exceptCIDR, peer.IPBlock.CIDR)
			}
		}
		return nil
	}

	if peer.PodSelector == nil && peer.NamespaceSelector == nil {
		return fmt.Errorf("peer must specify a pod selector, namespace selector, or IPBlock")
	}
	if peer.NamespaceSelector != nil && len(peer.NamespaceSelector.MatchLabels) == 0 && len(peer.NamespaceSelector.MatchExpressions) == 0 {
		return fmt.Errorf("empty namespace selector matches every namespace")
	}
	if peer.PodSelector != nil && peer.NamespaceSelector == nil && len(peer.PodSelector.MatchLabels) == 0 && len(peer.PodSelector.MatchExpressions) == 0 {
		return fmt.Errorf("empty pod selector matches every pod in the policy namespace")
	}
	return nil
}

// ReconcileNetworkPolicy creates or updates a NetworkPolicy to match desired
// state. It only reconciles the policy spec, labels, annotations, and owner
// references; API-managed metadata and unrelated fields are preserved.
func ReconcileNetworkPolicy(ctx context.Context, c client.Client, desired *networkingv1.NetworkPolicy) error {
	if c == nil {
		return fmt.Errorf("client must not be nil")
	}
	if err := ValidateNetworkPolicy(desired); err != nil {
		return err
	}

	existing := &networkingv1.NetworkPolicy{}
	key := types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}
	if err := c.Get(ctx, key, existing); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get NetworkPolicy %s/%s: %w", key.Namespace, key.Name, err)
		}
		if err := c.Create(ctx, desired.DeepCopy()); err != nil {
			return fmt.Errorf("create NetworkPolicy %s/%s: %w", key.Namespace, key.Name, err)
		}
		return nil
	}

	desiredCopy := desired.DeepCopy()
	reconciledLabels := make(map[string]string, len(existing.Labels)+len(desiredCopy.Labels))
	for key, value := range existing.Labels {
		if key != NetworkPolicyManagedByLabel && key != NetworkPolicyOwnerUIDLabel {
			reconciledLabels[key] = value
		}
	}
	for key, value := range desiredCopy.Labels {
		reconciledLabels[key] = value
	}
	if len(reconciledLabels) == 0 {
		reconciledLabels = nil
	}
	if equality.Semantic.DeepEqual(existing.Spec, desiredCopy.Spec) &&
		equality.Semantic.DeepEqual(existing.Labels, reconciledLabels) &&
		equality.Semantic.DeepEqual(existing.Annotations, desiredCopy.Annotations) &&
		equality.Semantic.DeepEqual(existing.OwnerReferences, desiredCopy.OwnerReferences) {
		return nil
	}

	updated := existing.DeepCopy()
	updated.Spec = desiredCopy.Spec
	updated.Labels = reconciledLabels
	updated.Annotations = desiredCopy.Annotations
	updated.OwnerReferences = desiredCopy.OwnerReferences
	if err := c.Update(ctx, updated); err != nil {
		return fmt.Errorf("update NetworkPolicy %s/%s: %w", key.Namespace, key.Name, err)
	}
	return nil
}

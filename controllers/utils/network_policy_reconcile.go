package utils

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	networkPolicyEgressModeAnnotation = "trustyai.opendatahub.io/network-policy-egress-mode"
	networkPolicyRationaleAnnotation  = "trustyai.opendatahub.io/network-policy-egress-rationale"
	networkPolicyRiskAnnotation       = "trustyai.opendatahub.io/network-policy-egress-residual-risk"
	networkPolicyBehaviorAnnotation   = "trustyai.opendatahub.io/network-policy-egress-behavior"
)

// NetworkPolicyAuthority is supplied by a controller after its own placement and
// namespace authorization checks. RBAC/labels alone do not establish authority.
// VerifiedAdoptionUID must originate from independently verified provenance,
// never a lookup by matching name/labels. Empty means ownerless adoption is denied.
type NetworkPolicyAuthority struct {
	Namespaces          map[string]bool
	VerifiedAdoptionUID types.UID
}

func verifyPolicyAuthority(ctx context.Context, c client.Client, scheme *runtime.Scheme, owner client.Object, namespace string, authority NetworkPolicyAuthority, allowDeleting bool) error {
	if owner == nil || owner.GetUID() == "" || namespace == "" || !authority.Namespaces[namespace] || owner.GetNamespace() != namespace {
		return fmt.Errorf("policy namespace/owner is outside authorized scope")
	}
	if scheme == nil {
		return fmt.Errorf("owner scheme is required")
	}
	gvk, err := apiutil.GVKForObject(owner, scheme)
	if err != nil {
		return err
	}
	owner.GetObjectKind().SetGroupVersionKind(gvk)
	live := owner.DeepCopyObject().(client.Object)
	if err := c.Get(ctx, client.ObjectKeyFromObject(owner), live); err != nil {
		return fmt.Errorf("verify live policy owner: %w", err)
	}
	if live.GetUID() != owner.GetUID() || (!allowDeleting && !live.GetDeletionTimestamp().IsZero()) {
		return fmt.Errorf("policy owner was replaced or is deleting")
	}
	return nil
}

func verifyExistingPolicy(existing *networkingv1.NetworkPolicy, owner client.Object, authority NetworkPolicyAuthority) error {
	ref := metav1.GetControllerOf(existing)
	if ref != nil && !matchesController(existing, owner) {
		return fmt.Errorf("policy has a foreign controller owner")
	}
	if uid, ok := existing.Labels[NetworkPolicyOwnerUIDLabel]; ok {
		expected := string(owner.GetUID())
		if len(validation.IsValidLabelValue(expected)) > 0 {
			expected = policyHash(expected)
		}
		if uid != expected {
			return fmt.Errorf("policy owner label contradicts requested owner")
		}
	}
	if manager, ok := existing.Labels[networkPolicyManagedByLabel]; ok && manager != networkPolicyManager {
		return fmt.Errorf("policy has a foreign manager")
	}
	if ref == nil && (authority.VerifiedAdoptionUID == "" || authority.VerifiedAdoptionUID != existing.UID) {
		return fmt.Errorf("ownerless policy requires independently verified adoption UID")
	}
	return nil
}

// ReconcileWorkloadNetworkPolicy creates or repairs one policy, preserving
// unrelated metadata. API errors/conflicts are returned to the controller for
// retry, never interpreted as absence. The input object is not modified.
func ReconcileWorkloadNetworkPolicy(ctx context.Context, c client.Client, scheme *runtime.Scheme, owner client.Object, identity NetworkPolicyIdentity, desired WorkloadNetworkPolicy, authority NetworkPolicyAuthority) error {
	if err := ValidateWorkloadNetworkPolicy(desired.Policy, identity, desired.Egress); err != nil {
		return err
	}
	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: desired.Policy.Name, Namespace: desired.Policy.Namespace},
		Spec:       *desired.Policy.Spec.DeepCopy(),
	}
	if err := verifyPolicyAuthority(ctx, c, scheme, owner, policy.Namespace, authority, false); err != nil {
		return err
	}
	if !identityOwnedBy(identity, owner) {
		return fmt.Errorf("policy identity differs from live owner")
	}
	if policy.Name == "" || desired.Policy.GenerateName != "" || len(validation.IsDNS1123Subdomain(policy.Name)) > 0 {
		return fmt.Errorf("explicit valid policy name is required")
	}
	if len(desired.Policy.OwnerReferences) > 0 {
		return fmt.Errorf("desired owner references must be supplied through the verified owner argument")
	}
	// Explicit managed metadata is generated from trusted arguments, not desired annotations.
	policy.Labels, _ = identity.Labels()
	policy.Labels[networkPolicyManagedByLabel] = networkPolicyManager
	policy.Annotations = map[string]string{}
	if desired.Egress != nil {
		policy.Annotations[networkPolicyEgressModeAnnotation] = string(desired.Egress.Mode)
		policy.Annotations[networkPolicyRationaleAnnotation] = desired.Egress.Rationale
		policy.Annotations[networkPolicyRiskAnnotation] = desired.Egress.ResidualRisk
		if desired.Egress.Mode == NetworkPolicyAllowAll {
			policy.Annotations[networkPolicyBehaviorAnnotation] = "Unrestricted outbound traffic; other additive policies cannot narrow this grant."
		}
	}
	if err := controllerutil.SetControllerReference(owner, policy, scheme); err != nil {
		return err
	}
	existing := &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(policy), existing); err != nil {
		if apierrors.IsNotFound(err) {
			return c.Create(ctx, policy)
		}
		return err
	}
	if err := verifyExistingPolicy(existing, owner, authority); err != nil {
		return err
	}
	if !existing.DeletionTimestamp.IsZero() {
		return fmt.Errorf("policy is deleting; retry after deletion completes")
	}
	updated := existing.DeepCopy()
	updated.Spec = policy.Spec
	if updated.Labels == nil {
		updated.Labels = map[string]string{}
	}
	for key, value := range policy.Labels {
		updated.Labels[key] = value
	}
	if updated.Annotations == nil {
		updated.Annotations = map[string]string{}
	}
	for _, key := range []string{networkPolicyEgressModeAnnotation, networkPolicyRationaleAnnotation, networkPolicyRiskAnnotation, networkPolicyBehaviorAnnotation} {
		delete(updated.Annotations, key)
	}
	for key, value := range policy.Annotations {
		updated.Annotations[key] = value
	}
	if err := controllerutil.SetControllerReference(owner, updated, scheme); err != nil {
		return err
	}
	normalizePolicy(updated)
	baseline := existing.DeepCopy()
	normalizePolicy(baseline)
	if apiequality.Semantic.DeepEqual(baseline, updated) {
		return nil
	}
	return c.Update(ctx, updated)
}

func normalizePorts(ports []networkingv1.NetworkPolicyPort) {
	for index := range ports {
		if ports[index].Protocol == nil {
			protocol := corev1.ProtocolTCP
			ports[index].Protocol = &protocol
		}
	}
}

func normalizePolicy(policy *networkingv1.NetworkPolicy) {
	if len(policy.Annotations) == 0 {
		policy.Annotations = nil
	}
	if len(policy.Labels) == 0 {
		policy.Labels = nil
	}
	if len(policy.Spec.Ingress) == 0 {
		policy.Spec.Ingress = nil
	}
	if len(policy.Spec.Egress) == 0 {
		policy.Spec.Egress = nil
	}
	for index := range policy.Spec.Ingress {
		normalizePorts(policy.Spec.Ingress[index].Ports)
	}
	for index := range policy.Spec.Egress {
		if len(policy.Spec.Egress[index].To) == 0 {
			policy.Spec.Egress[index].To = nil
		}
		if len(policy.Spec.Egress[index].Ports) == 0 {
			policy.Spec.Egress[index].Ports = nil
		}
		normalizePorts(policy.Spec.Egress[index].Ports)
	}
}

// DeleteOwnedWorkloadNetworkPolicy deletes only an exact, previously recorded
// policy UID. A replacement at the same name is never removed. The caller must
// verify workload drain before deleting legacy grants/protection.
func DeleteOwnedWorkloadNetworkPolicy(ctx context.Context, c client.Client, scheme *runtime.Scheme, owner client.Object, key client.ObjectKey, policyUID types.UID, authority NetworkPolicyAuthority) error {
	if policyUID == "" {
		return fmt.Errorf("policy UID is required for cleanup")
	}
	if err := verifyPolicyAuthority(ctx, c, scheme, owner, key.Namespace, authority, true); err != nil {
		return err
	}
	existing := &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, key, existing); err != nil {
		return client.IgnoreNotFound(err)
	}
	if existing.UID != policyUID {
		return fmt.Errorf("policy UID changed; refusing cleanup")
	}
	// Cleanup never adopts ownerless policies, even when adoption was authorized.
	authority.VerifiedAdoptionUID = ""
	if err := verifyExistingPolicy(existing, owner, authority); err != nil {
		return err
	}
	return client.IgnoreNotFound(c.Delete(ctx, existing, client.Preconditions{UID: &policyUID, ResourceVersion: &existing.ResourceVersion}))
}

// ReconcileWorkloadNetworkPolicySet validates the complete desired set before
// writing any policy and reports success only after every policy reconciles.
// Failures stop writes and preserve both reconcile and status-update errors.
func ReconcileWorkloadNetworkPolicySet(ctx context.Context, c client.Client, scheme *runtime.Scheme, owner client.Object, identity NetworkPolicyIdentity, policies []WorkloadNetworkPolicy, authority NetworkPolicyAuthority, report func(bool, error) error) error {
	if report == nil {
		return fmt.Errorf("policy status reporter is required")
	}
	err := ValidateWorkloadNetworkPolicySet(policies, identity)
	if err == nil {
		for _, policy := range policies {
			if err = ReconcileWorkloadNetworkPolicy(ctx, c, scheme, owner, identity, policy, authority); err != nil {
				break
			}
		}
	}
	return ReportNetworkPolicyResult(err, report)
}

// ReportNetworkPolicyResult persists the complete-set outcome through a
// component-provided status adapter and preserves both reconcile and status
// errors. It does not set CR Ready/DSC health or overwrite evaluation results.
func ReportNetworkPolicyResult(reconcileErr error, report func(ready bool, cause error) error) error {
	if report == nil {
		return errors.Join(reconcileErr, fmt.Errorf("policy status reporter is required"))
	}
	return errors.Join(reconcileErr, report(reconcileErr == nil, reconcileErr))
}

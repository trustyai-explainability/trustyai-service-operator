package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

const (
	NetworkPolicyComponentLabel = "trustyai.opendatahub.io/component"
	NetworkPolicyRoleLabel      = "trustyai.opendatahub.io/role"
	NetworkPolicyOwnerUIDLabel  = "trustyai.opendatahub.io/owner-uid"
	networkPolicyManagedByLabel = "trustyai.opendatahub.io/network-policy-managed-by"
	networkPolicyManager        = "trustyai-service-operator"
)

// NetworkPolicyIdentity describes a workload, not its display name or API version.
// Labels are selection metadata, not authorization against a label-forging tenant.
type NetworkPolicyIdentity struct {
	OwnerKind schema.GroupKind
	OwnerUID  types.UID
	Component string
	Role      string
}

func (i NetworkPolicyIdentity) Validate() error {
	if i.OwnerKind.Kind == "" || i.OwnerUID == "" {
		return fmt.Errorf("owner kind and UID are required")
	}
	if errs := validation.IsDNS1123Subdomain(i.OwnerKind.Group); i.OwnerKind.Group != "" && len(errs) > 0 {
		return fmt.Errorf("invalid owner group: %v", errs)
	}
	for key, value := range map[string]string{"kind": i.OwnerKind.Kind, "component": i.Component, "role": i.Role} {
		if value == "" || len(validation.IsValidLabelValue(value)) > 0 {
			return fmt.Errorf("invalid %s identity %q", key, value)
		}
	}
	return nil
}

func policyHash(value interface{}) string {
	// All callers supply structs/strings, whose JSON encoding cannot fail.
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:32]
}

// Labels returns fresh reserved labels. Long UIDs are hashed, never truncated.
func (i NetworkPolicyIdentity) Labels() (map[string]string, error) {
	if err := i.Validate(); err != nil {
		return nil, err
	}
	uid := string(i.OwnerUID)
	if len(validation.IsValidLabelValue(uid)) > 0 {
		uid = policyHash(uid)
	}
	return map[string]string{NetworkPolicyComponentLabel: i.Component, NetworkPolicyRoleLabel: i.Role, NetworkPolicyOwnerUIDLabel: uid}, nil
}

// Name includes the full unnormalized identity and purpose in its hash. Version
// changes do not rename policies; owner recreation, role and kind changes do.
func (i NetworkPolicyIdentity) Name(purpose string) (string, error) {
	if err := i.Validate(); err != nil {
		return "", err
	}
	if strings.TrimSpace(purpose) == "" {
		return "", fmt.Errorf("policy purpose is required")
	}
	return "trustyai-np-" + policyHash(struct {
		Identity NetworkPolicyIdentity
		Purpose  string
	}{i, purpose}), nil
}

func identityOwnedBy(i NetworkPolicyIdentity, owner client.Object) bool {
	gvk := owner.GetObjectKind().GroupVersionKind()
	return owner.GetUID() == i.OwnerUID && gvk.GroupKind() == i.OwnerKind
}

func matchesController(obj metav1.Object, owner client.Object) bool {
	ref := metav1.GetControllerOf(obj)
	if ref == nil {
		return false
	}
	gvk := owner.GetObjectKind().GroupVersionKind()
	refGV, err := schema.ParseGroupVersion(ref.APIVersion)
	return err == nil && ref.UID == owner.GetUID() && ref.Name == owner.GetName() && ref.Kind == gvk.Kind && refGV.Group == gvk.Group
}

// LabelOwnedNetworkPolicyDeployment stamps only a directly owned Deployment's
// template. The scheme resolves the owner's GVK without relying on TypeMeta being
// populated. The caller must retain legacy policies until old replicas drain and
// protect reserved-label authority. Immutable selectors are never modified.
func LabelOwnedNetworkPolicyDeployment(deployment *appsv1.Deployment, scheme *runtime.Scheme, owner client.Object, identity NetworkPolicyIdentity) error {
	if deployment == nil || owner == nil {
		return fmt.Errorf("deployment and policy owner are required")
	}
	if scheme == nil {
		return fmt.Errorf("owner scheme is required")
	}
	gvk, err := apiutil.GVKForObject(owner, scheme)
	if err != nil {
		return fmt.Errorf("resolve policy owner GVK: %w", err)
	}
	ownerWithGVK := owner.DeepCopyObject().(client.Object)
	ownerWithGVK.GetObjectKind().SetGroupVersionKind(gvk)
	if !identityOwnedBy(identity, ownerWithGVK) || deployment.Namespace != owner.GetNamespace() || !matchesController(deployment, ownerWithGVK) {
		return fmt.Errorf("deployment does not belong to the supplied policy owner")
	}
	labels, err := identity.Labels()
	if err != nil {
		return err
	}
	if deployment.Spec.Selector == nil {
		return fmt.Errorf("deployment selector is required")
	}
	// A conflicting immutable selector would make a template-only repair invalid.
	for key, value := range labels {
		if selected, ok := deployment.Spec.Selector.MatchLabels[key]; ok && selected != value {
			return fmt.Errorf("immutable selector conflicts with reserved label %s", key)
		}
	}
	merged := deployment.Spec.Template.DeepCopy()
	if merged.Labels == nil {
		merged.Labels = map[string]string{}
	}
	for key, value := range labels {
		merged.Labels[key] = value
	}
	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil || !selector.Matches(klabels.Set(merged.Labels)) {
		return fmt.Errorf("reserved labels conflict with deployment selector")
	}
	deployment.Spec.Template = *merged
	return nil
}

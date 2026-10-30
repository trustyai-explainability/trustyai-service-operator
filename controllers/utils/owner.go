package utils

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// OwnerUIDLabel records the owning custom resource when a controller reference cannot be set.
// Kubernetes rejects owner references that cross namespaces.
const OwnerUIDLabel = "trustyai.opendatahub.io/owner-uid"

const (
	ownerNameAnnotation      = "trustyai.opendatahub.io/owner-name"
	ownerNamespaceAnnotation = "trustyai.opendatahub.io/owner-namespace"
)

// IsOwnedBy reports whether obj is owned by owner.
// An owner reference with the owner's UID is sufficient. Otherwise the owner-uid
// label must match. When owner-name or owner-namespace annotations are set, they
// must identify this owner so a copied label cannot claim the object.
func IsOwnedBy(obj, owner metav1.Object) bool {
	if obj == nil || owner == nil || owner.GetUID() == "" {
		return false
	}
	for _, ref := range obj.GetOwnerReferences() {
		if ref.UID == owner.GetUID() {
			return true
		}
	}
	if obj.GetLabels()[OwnerUIDLabel] != string(owner.GetUID()) {
		return false
	}
	name := obj.GetAnnotations()[ownerNameAnnotation]
	namespace := obj.GetAnnotations()[ownerNamespaceAnnotation]
	if name == "" && namespace == "" {
		return true
	}
	return name == owner.GetName() && namespace == owner.GetNamespace()
}

// SetOwnerReference records the owner on the object's owner-uid label.
// It also sets a controller reference when the owner and object share a namespace.
// Otherwise it records the owner name and namespace as annotations, because Kubernetes rejects cross-namespace owner references.
func SetOwnerReference(owner, object metav1.Object, scheme *runtime.Scheme) error {
	if owner.GetUID() != "" {
		labels := object.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels[OwnerUIDLabel] = string(owner.GetUID())
		object.SetLabels(labels)
	}

	ownerNamespace := owner.GetNamespace()
	objectNamespace := object.GetNamespace()
	if ownerNamespace != "" && objectNamespace != "" && ownerNamespace != objectNamespace {
		annotations := object.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations[ownerNameAnnotation] = owner.GetName()
		annotations[ownerNamespaceAnnotation] = ownerNamespace
		object.SetAnnotations(annotations)
		return nil
	}
	return controllerutil.SetControllerReference(owner, object, scheme)
}

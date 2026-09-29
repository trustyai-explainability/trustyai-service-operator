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

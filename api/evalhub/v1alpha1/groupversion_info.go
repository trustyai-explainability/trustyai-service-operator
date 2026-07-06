// Package v1alpha1 contains API Schema definitions for the trustyai.opendatahub.io v1alpha1 API group
// +kubebuilder:object:generate=true
// +groupName=trustyai.opendatahub.io
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

const (
	GroupName     = "trustyai.opendatahub.io"
	Version       = "v1alpha1"
	KindName      = "EvalHub"
	FinalizerName = "trustyai.opendatahub.io/evalhub-finalizer"
)

var (
	// GroupVersion is group version used to register these objects
	GroupVersion = schema.GroupVersion{Group: GroupName, Version: Version}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

// Package v1alpha1 contains the TrustyAI module CR API used by the ODH
// platform orchestrator to manage the TrustyAI module operator lifecycle.
//
// +kubebuilder:object:generate=true
// +groupName=components.platform.opendatahub.io
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	GroupVersion = schema.GroupVersion{
		Group:   "components.platform.opendatahub.io",
		Version: "v1alpha1",
	}

	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	AddToScheme = SchemeBuilder.AddToScheme
)

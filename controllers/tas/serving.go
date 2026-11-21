package tas

import (
	"context"
	"fmt"
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// The serving integration is configured from data, not from a compile-time
// assumption about another component's API: the CRD is read once per pass and
// the group, resource and served version come from it. A version bump within the
// group therefore needs no AI Safety release, and a version whose schema no
// longer describes the one field this operator writes is reported as
// unsupported instead of being written through.
//
// A change of API *group* is deliberately not followed: a different group is a
// different resource with a different schema, so it is configuration rather than
// discovery. It is reported as unsupported too.
// The resource identity, the field written and the annotations read are
// declared in serving_fields.go, so that a single file holds every assumption
// this package makes about the serving API.

// servingState is what the serving API offers on this pass. Absence is a
// supported configuration, not a degradation: the operator runs, reports the
// state and keeps requiring nothing from serving.
type servingState string

const (
	// servingStateAbsent: the InferenceService CRD is not installed.
	servingStateAbsent servingState = "absent"
	// servingStateUnsupported: the CRD is installed, but no served version's
	// schema describes the predictor logger, so writing would corrupt or fail.
	servingStateUnsupported servingState = "unsupported"
	// servingStateUsable: a served version whose schema describes the logger was
	// adopted for this pass.
	servingStateUsable servingState = "usable"
)

// servingContract is the serving API as read from the CRD on this pass.
type servingContract struct {
	state servingState
	// gvr is the resource the adopted version serves; zero unless usable.
	gvr schema.GroupVersionResource
	// version is the adopted version; empty unless usable.
	version string
	// servedVersions is every version the CRD serves, in CRD order, for messages.
	servedVersions []string
}

// groupVersion returns the adopted group and version, or an empty string.
func (c servingContract) groupVersion() string {
	if c.state != servingStateUsable {
		return ""
	}
	return c.gvr.GroupVersion().String()
}

// servingReason is the InferenceServicesPresent reason for this contract.
func (c servingContract) servingReason() string {
	switch c.state {
	case servingStateAbsent:
		return StatusReasonServingCRDNotFound
	case servingStateUnsupported:
		return StatusReasonServingVersionUnsupported
	default:
		return StatusReasonInferenceServicesFound
	}
}

// unusableMessage explains an absent or unsupported serving API in terms an
// administrator can act on.
func (c servingContract) unusableMessage() string {
	switch c.state {
	case servingStateAbsent:
		return fmt.Sprintf("the serving CRD %s is not present; payload capture is unavailable and activates when serving is installed", inferenceServiceCRDName)
	case servingStateUnsupported:
		return fmt.Sprintf("no served version of %s describes %s (served: %s); payload capture is unsupported on this serving API",
			inferenceServiceCRDName, predictorLoggerFieldPath, strings.Join(c.servedVersions, ", "))
	default:
		return ""
	}
}

// servingContractOf reads the InferenceService CRD and derives the contract from
// it. The read goes through the uncached reader so that consulting the CRD never
// builds an informer for a kind the operator does not watch.
//
// An API failure is returned as an error, because it is transient and the
// reconcile should requeue; absence is not an error, it is a state.
func (r *TrustyAIServiceReconciler) servingContractOf(ctx context.Context) (servingContract, error) {
	crd := &apiextensionsv1.CustomResourceDefinition{}
	err := r.uncachedReader().Get(ctx, types.NamespacedName{Name: inferenceServiceCRDName}, crd)
	if err != nil {
		if errors.IsNotFound(err) {
			return deriveServingContract(nil), nil
		}
		return servingContract{}, fmt.Errorf("error getting %s CRD: %v", inferenceServiceCRDName, err)
	}
	return deriveServingContract(crd), nil
}

// deriveServingContract is the whole version-adoption decision, expressed over
// the CRD so it can be tested without a cluster: group from spec.group, resource
// from spec.names.plural, and the version from the served entries, preferring
// v1beta1 and otherwise taking the first served entry whose schema still
// describes the predictor logger.
func deriveServingContract(crd *apiextensionsv1.CustomResourceDefinition) servingContract {
	if crd == nil {
		return servingContract{state: servingStateAbsent}
	}

	served := make([]string, 0, len(crd.Spec.Versions))
	for _, version := range crd.Spec.Versions {
		if version.Served {
			served = append(served, version.Name)
		}
	}

	contract := servingContract{servedVersions: served}
	if len(served) == 0 {
		// Nothing is served, so no version can be adopted. This is reported as
		// unsupported rather than absent: the CRD exists and is broken.
		contract.state = servingStateUnsupported
		return contract
	}

	for _, candidate := range servingVersionCandidates(served) {
		if versionDescribesServingFields(crdSchemaForVersion(crd, candidate)) {
			contract.state = servingStateUsable
			contract.version = candidate
			contract.gvr = schema.GroupVersionResource{
				Group:    crd.Spec.Group,
				Version:  candidate,
				Resource: crd.Spec.Names.Plural,
			}
			return contract
		}
	}

	contract.state = servingStateUnsupported
	return contract
}

// servingVersionCandidates orders the served versions for adoption: the
// preferred version first when it is served, then the rest in CRD order. The
// storage flag is not consulted, because serving storage is not a statement about
// what a client may write.
func servingVersionCandidates(served []string) []string {
	ordered := make([]string, 0, len(served))
	for _, version := range served {
		if version == preferredServingVersion {
			ordered = append(ordered, version)
			break
		}
	}
	for _, version := range served {
		if version != preferredServingVersion {
			ordered = append(ordered, version)
		}
	}
	return ordered
}

func crdSchemaForVersion(crd *apiextensionsv1.CustomResourceDefinition, version string) *apiextensionsv1.JSONSchemaProps {
	for i := range crd.Spec.Versions {
		if crd.Spec.Versions[i].Name != version {
			continue
		}
		if crd.Spec.Versions[i].Schema == nil {
			return nil
		}
		return crd.Spec.Versions[i].Schema.OpenAPIV3Schema
	}
	return nil
}

// versionDescribesServingFields reports whether a served version's schema
// describes every field payload capture needs there. Every declared path is
// required, not just the one currently written, so adding a field to
// servingSchemaFieldPaths tightens adoption rather than being ignored.
func versionDescribesServingFields(schema *apiextensionsv1.JSONSchemaProps) bool {
	for _, declared := range servingSchemaFieldPaths {
		if !schemaDescribesField(schema, strings.Split(declared, ".")) {
			return false
		}
	}
	return true
}

// schemaDescribesField reports whether every step of path is declared as a
// property. A nil schema, or an object left open at any step, means the field
// cannot be verified, and an unverified field is not written.
func schemaDescribesField(schema *apiextensionsv1.JSONSchemaProps, path []string) bool {
	current := schema
	for _, property := range path {
		if current == nil || current.Properties == nil {
			return false
		}
		next, present := current.Properties[property]
		if !present {
			return false
		}
		current = &next
	}
	return current != nil
}

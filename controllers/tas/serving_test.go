package tas

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// The version-adoption decision is expressed over the CRD, so these tests read
// like the cluster states an administrator can produce: the CRD absent, a served
// version that still describes the field this operator writes, a served version
// that does not, and no served version at all.
var _ = Describe("Serving contract derivation", func() {

	// loggerSchema is the minimal shape the operator needs: a structural schema
	// that declares spec.predictor.logger.
	crdWith := func(versions ...apiextensionsv1.CustomResourceDefinitionVersion) *apiextensionsv1.CustomResourceDefinition {
		return &apiextensionsv1.CustomResourceDefinition{
			Spec: apiextensionsv1.CustomResourceDefinitionSpec{
				Group:    "serving.kserve.io",
				Names:    apiextensionsv1.CustomResourceDefinitionNames{Plural: "inferenceservices"},
				Versions: versions,
			},
		}
	}

	schemaWithLogger := func() *apiextensionsv1.CustomResourceValidation {
		return &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
			Type: "object",
			Properties: map[string]apiextensionsv1.JSONSchemaProps{
				"spec": {Type: "object", Properties: map[string]apiextensionsv1.JSONSchemaProps{
					"predictor": {Type: "object", Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"logger": {Type: "object"},
					}},
				}},
			},
		}}
	}

	schemaWithoutLogger := func() *apiextensionsv1.CustomResourceValidation {
		return &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
			Type: "object",
			Properties: map[string]apiextensionsv1.JSONSchemaProps{
				"spec": {Type: "object", Properties: map[string]apiextensionsv1.JSONSchemaProps{
					"predictor": {Type: "object", Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"model": {Type: "object"},
					}},
				}},
			},
		}}
	}

	served := func(name string, schema *apiextensionsv1.CustomResourceValidation) apiextensionsv1.CustomResourceDefinitionVersion {
		return apiextensionsv1.CustomResourceDefinitionVersion{Name: name, Served: true, Storage: true, Schema: schema}
	}

	unserved := func(name string) apiextensionsv1.CustomResourceDefinitionVersion {
		return apiextensionsv1.CustomResourceDefinitionVersion{Name: name, Served: false}
	}

	DescribeTable("derives the state and the adopted version",
		func(crd *apiextensionsv1.CustomResourceDefinition, wantState servingState, wantVersion string, wantReason string) {
			contract := deriveServingContract(crd)
			Expect(contract.state).To(Equal(wantState))
			Expect(contract.version).To(Equal(wantVersion))
			Expect(contract.servingReason()).To(Equal(wantReason))
		},
		Entry("CRD absent is a state, not an error",
			nil, servingStateAbsent, "", StatusReasonServingCRDNotFound),
		Entry("v1beta1 served and describing the logger is adopted",
			crdWith(served("v1beta1", schemaWithLogger())), servingStateUsable, "v1beta1", StatusReasonInferenceServicesFound),
		Entry("v1beta1 is preferred over another served version",
			crdWith(served("v1", schemaWithLogger()), served("v1beta1", schemaWithLogger())),
			servingStateUsable, "v1beta1", StatusReasonInferenceServicesFound),
		Entry("a version other than v1beta1 is adopted when it is the only served one",
			crdWith(served("v1", schemaWithLogger())), servingStateUsable, "v1", StatusReasonInferenceServicesFound),
		Entry("the next served version is adopted when v1beta1 no longer describes the logger",
			crdWith(served("v2", schemaWithLogger()), served("v1beta1", schemaWithoutLogger())),
			servingStateUsable, "v2", StatusReasonInferenceServicesFound),
		Entry("a served version that does not describe the logger is unsupported",
			crdWith(served("v1beta1", schemaWithoutLogger())),
			servingStateUnsupported, "", StatusReasonServingVersionUnsupported),
		Entry("an unserved version is not adopted",
			crdWith(unserved("v1beta1"), served("v1", schemaWithoutLogger())),
			servingStateUnsupported, "", StatusReasonServingVersionUnsupported),
		Entry("a CRD that serves no version is unsupported",
			crdWith(unserved("v1beta1")), servingStateUnsupported, "", StatusReasonServingVersionUnsupported),
		Entry("a served version with no schema is unverifiable and therefore unsupported",
			crdWith(served("v1beta1", nil)), servingStateUnsupported, "", StatusReasonServingVersionUnsupported),
		Entry("a schema that stops at an open object cannot verify the field",
			crdWith(served("v1beta1", &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
				Type:       "object",
				Properties: map[string]apiextensionsv1.JSONSchemaProps{"spec": {Type: "object"}},
			}})),
			servingStateUnsupported, "", StatusReasonServingVersionUnsupported),
	)

	It("builds the GVR from the CRD rather than from the compiled-in group and version", func() {
		crd := crdWith(served("v1", schemaWithLogger()))
		crd.Spec.Group = "serving.example.com"
		crd.Spec.Names.Plural = "predictors"

		contract := deriveServingContract(crd)
		Expect(contract.state).To(Equal(servingStateUsable))
		Expect(contract.gvr.Group).To(Equal("serving.example.com"))
		Expect(contract.gvr.Version).To(Equal("v1"))
		Expect(contract.gvr.Resource).To(Equal("predictors"))
		Expect(contract.groupVersion()).To(Equal("serving.example.com/v1"))
	})

	It("leaves the group version empty when nothing was adopted", func() {
		Expect(deriveServingContract(nil).groupVersion()).To(BeEmpty())
		Expect(deriveServingContract(crdWith(served("v1beta1", schemaWithoutLogger()))).groupVersion()).To(BeEmpty())
	})

	It("names the reason and the served versions an administrator needs to act", func() {
		absent := deriveServingContract(nil)
		Expect(absent.unusableMessage()).To(ContainSubstring(inferenceServiceCRDName))

		unsupported := deriveServingContract(crdWith(served("v1beta1", schemaWithoutLogger()), served("v2", schemaWithoutLogger())))
		Expect(unsupported.unusableMessage()).To(ContainSubstring(predictorLoggerFieldPath))
		Expect(unsupported.unusableMessage()).To(ContainSubstring("v1beta1"))
		Expect(unsupported.unusableMessage()).To(ContainSubstring("v2"))
		Expect(unsupported.servedVersions).To(Equal([]string{"v1beta1", "v2"}))
	})
})

var _ = Describe("Serving state reporting", func() {

	usable := servingContract{
		state:   servingStateUsable,
		version: "v1beta1",
		gvr: schema.GroupVersionResource{
			Group:    "serving.kserve.io",
			Version:  "v1beta1",
			Resource: "inferenceservices",
		},
	}

	It("distinguishes serving usable and idle from serving absent", func() {
		idle := inferenceServicesReport{contract: usable, namespace: "opendatahub"}
		Expect(idle.reason()).To(Equal(StatusReasonNoInferenceServices))
		Expect(idle.message()).To(ContainSubstring("serving.kserve.io/v1beta1"))

		present := inferenceServicesReport{contract: usable, namespace: "opendatahub", present: true}
		Expect(present.reason()).To(Equal(StatusReasonInferenceServicesFound))
		Expect(present.message()).To(ContainSubstring("serving.kserve.io/v1beta1"))

		absent := inferenceServicesReport{contract: servingContract{state: servingStateAbsent}}
		Expect(absent.reason()).To(Equal(StatusReasonServingCRDNotFound))
		Expect(absent.message()).To(ContainSubstring(inferenceServiceCRDName))
	})
})

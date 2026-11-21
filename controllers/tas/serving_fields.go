package tas

import "strings"

// Everything this package assumes about another component's API is declared in
// this file and nowhere else: the resource identity, the one field payload
// capture writes, and the annotations, labels and values that select a write
// path. Keeping them together is what makes the assumption auditable; keeping
// them out of the code that uses them is what makes a change on the serving side
// show up as one failing test rather than a silent write to the wrong place.
//
// The declarations are checked from two directions. Version adoption asks a
// served version's OpenAPI schema about the field it writes, and
// serving_fields_test asks the CRD vendored under tests/crds about all of them,
// and fails if a serving literal appears anywhere else in the package.
const (
	// inferenceServiceCRDName is the CRD the serving contract is derived from.
	inferenceServiceCRDName = "inferenceservices.serving.kserve.io"

	// preferredServingVersion is the version adopted when it is served and its
	// schema describes the field this operator writes. It is a preference, never
	// a requirement: any served version that describes the field is adopted.
	preferredServingVersion = "v1beta1"

	// predictorLoggerFieldPath is the one field payload capture writes.
	predictorLoggerFieldPath = "spec.predictor.logger"

	// deploymentModeAnnotation is KServe's own record of how an InferenceService
	// is deployed, and it selects which sink URL and which write path the
	// operator uses. It is only ever read.
	deploymentModeAnnotation = "serving.kserve.io/deploymentMode"

	// istioInjectAnnotation asks for the service-mesh objects; it is only read.
	istioInjectAnnotation = "sidecar.istio.io/inject"

	// loggerModeAll captures request and response payloads.
	loggerModeAll = "all"

	// tasKServeInferenceServiceLabel is the label KServe sets on the Pods it
	// creates for an InferenceService. The TAS NetworkPolicy admits traffic from
	// Pods carrying it, so it is read by the policy rather than by a client.
	tasKServeInferenceServiceLabel = "serving.kserve.io/inferenceservice"
)

// servingLoggerAnnotation is the annotation that records which logger KServe
// configured. It and the two below are only ever deleted.
const (
	servingLoggerAnnotation        = "internal.serving.kserve.io/logger"
	servingLoggerModeAnnotation    = "internal.serving.kserve.io/logger-mode"
	servingLoggerSinkURLAnnotation = "internal.serving.kserve.io/logger-sink-url"
)

var (
	// predictorLoggerFieldPathParts is the write path as the schema walk needs
	// it: one element per level, so a lookup cannot mistake a field name for a
	// path separator.
	predictorLoggerFieldPathParts = strings.Split(predictorLoggerFieldPath, ".")

	// servingSchemaFieldPaths are the paths a served version's schema has to
	// describe for payload capture to be attempted there. Version adoption walks
	// these, so a version that renames or removes the field is reported as
	// unsupported instead of being written to.
	servingSchemaFieldPaths = []string{predictorLoggerFieldPath}

	// servingLoggerAnnotationKeys are the annotations KServe writes alongside the
	// logger. Payload capture only ever deletes them, and deleting a key that is
	// not there is a no-op, so a change of naming on the serving side degrades to
	// nothing rather than to a failure.
	servingLoggerAnnotationKeys = []string{
		servingLoggerAnnotation,
		servingLoggerModeAnnotation,
		servingLoggerSinkURLAnnotation,
	}

	// servingAnnotationKeys is every annotation key this operator reads or
	// deletes, for the tests that check they are well formed and unique.
	servingAnnotationKeys = []string{
		deploymentModeAnnotation,
		istioInjectAnnotation,
		servingLoggerAnnotation,
		servingLoggerModeAnnotation,
		servingLoggerSinkURLAnnotation,
	}
)

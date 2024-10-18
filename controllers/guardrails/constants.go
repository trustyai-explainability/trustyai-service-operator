package guardrails

// TODO: create new orchestrator image
const (
	defaultImage       = string("")
	finalizerName      = "guardrails.opendatahub.io/finalizer"
	serverTLSMountPath = "/tls/server"
	orchTLSMouthPath   = "/tls/orch"
)

// Allowed TLS modes
const (
	TLSMode_TLS  = "TLS"
	TLSMode_mTLS = "mTLS"
	TLSMode_None = "None"
)

// Status types
const (
	StatusTypeGenerationPresent = "GeneratorPresent"
	StatusTypeDetectorPresent   = "DetectorPresent"
	StatusTypeRouteAvailable    = "RouteAvailable"
	StatusTypeAvailable         = "Available"
)

// Status reasons
const (
	StatusReasonGenerationNotFound = "GeneratorNotFound"
	StatusReasonGenerationFound    = "GeneratorFound"
	StatusReasonDetectorNotFound   = "DetectorNotFound"
	StatusReasonDetectorFound      = "DetectorFound"
	StatusReasonRouteNotFound      = "RouteNotFound"
	StatusReasonRouteFound         = "RouteFound"
	StatusAvailable                = "AllComponentsReady"
	StatusNotAvailable             = "NotAllComponentsReady"
)

// Event reasons
const (
	EventReasonGenerationCreated = "GeneratorServiceCreated"
	EventReasonDetectorCreated   = "DetectorServiceCreated"
)

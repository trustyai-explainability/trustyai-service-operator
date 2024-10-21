package tas

import "time"

const (
	defaultImage         = string("quay.io/trustyai/trustyai-service:latest")
	containerName        = "trustyai-service"
	componentName        = "trustyai"
	serviceMonitorName   = "trustyai-metrics"
	finalizerName        = "trustyai.opendatahub.io/finalizer"
	payloadProcessorName = "MM_PAYLOAD_PROCESSORS"
	tlsKeyCertPathName   = "MM_TLS_KEY_CERT_PATH"
	mmContainerName      = "mm"
	modelMeshLabelKey    = "modelmesh-service"
	modelMeshLabelValue  = "modelmesh-serving"
	volumeMountName      = "volume"
	defaultRequeueDelay  = 30 * time.Second
	dbCredentialsSuffix  = "-db-credentials"
	ServiceName          = "TAS"
)

// Allowed storage formats
const (
	STORAGE_PVC      = "PVC"
	STORAGE_DATABASE = "DATABASE"
)

// Configuration constants
const (
	imageConfigMap               = "trustyai-service-operator-config"
	configMapOAuthProxyImageKey  = "oauthProxyImage"
	configMapServiceImageKey     = "trustyaiServiceImage"
	configMapkServeServerlessKey = "kServeServerless"
)

// OAuth constants
const (
	OAuthServicePort       = 443
	OAuthName              = "oauth-proxy"
	OAuthServicePortName   = "oauth-proxy"
	defaultOAuthProxyImage = "registry.redhat.io/openshift4/ose-oauth-proxy:latest"
)

// Status types
const (
	StatusTypeInferenceServicesPresent = "InferenceServicesPresent"
	StatusTypePVCAvailable             = "PVCAvailable"
	StatusTypeRouteAvailable           = "RouteAvailable"
	StatusTypeAvailable                = "Available"
	StatusTypeDBAvailable              = "DBAvailable"
)

// Status reasons
const (
	StatusReasonInferenceServicesNotFound = "InferenceServicesNotFound"
	StatusReasonInferenceServicesFound    = "InferenceServicesFound"
	StatusReasonPVCNotFound               = "PVCNotFound"
	StatusReasonPVCFound                  = "PVCFound"
	StatusReasonRouteNotFound             = "RouteNotFound"
	StatusReasonRouteFound                = "RouteFound"
	StatusAvailable                       = "AllComponentsReady"
	StatusNotAvailable                    = "NotAllComponentsReady"
	StatusDBCredentialsNotFound           = "DBCredentialsNotFound"
	StatusDBCredentialsError              = "DBCredentialsError"
	StatusDBConnectionError               = "DBConnectionError"
	StatusDBAvailable                     = "DBAvailable"
)

// Event reasons
const (
	EventReasonPVCCreated                 = "PVCCreated"
	EventReasonInferenceServiceConfigured = "InferenceServiceConfigured"
	EventReasonServiceMonitorCreated      = "ServiceMonitorCreated"
)

const (
	StateReasonCrashLoopBackOff = "CrashLoopBackOff"
)

// Phases
const (
	PhaseReady    = "Ready"
	PhaseNotReady = "Not Ready"
)

const migrationAnnotationKey = "trustyai.opendatahub.io/db-migration"

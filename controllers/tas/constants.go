package tas

import (
	"os"
	"time"
)

var (
	defaultImage              = envOrDefault("RELATED_IMAGE_ODH_TRUSTYAI_SERVICE_IMAGE", "quay.io/trustyai/trustyai-service:latest")
	defaultKubeRBACProxyImage = envOrDefault("RELATED_IMAGE_ODH_KUBE_RBAC_PROXY_IMAGE", "quay.io/opendatahub/odh-kube-rbac-proxy:odh-stable")
)

const (
	containerName = "trustyai-service"
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
	imageConfigMap                 = "trustyai-service-operator-config"
	configMapKubeRBACProxyImageKey = "kube-rbac-proxy"
	configMapServiceImageKey       = "trustyaiServiceImage"
	configMapkServeServerlessKey   = "kServeServerless"
)

// Kube-RBAC-Proxy constants
const (
	KubeRBACProxyServicePort     = 8443
	KubeRBACProxyName            = "kube-rbac-proxy"
	KubeRBACProxyServicePortName = "https"
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

func envOrDefault(env, fallback string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return fallback
}

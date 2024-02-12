package controllers

import "time"

const (
	defaultImage         = string("quay.io/trustyai/trustyai-service:latest")
	containerName        = "trustyai-service"
	componentName        = "trustyai"
	serviceMonitorName   = "trustyai-metrics"
	finalizerName        = "trustyai.opendatahub.io/finalizer"
	payloadProcessorName = "MM_PAYLOAD_PROCESSORS"
	modelMeshLabelKey    = "modelmesh-service"
	modelMeshLabelValue  = "modelmesh-serving"
	volumeMountName      = "volume"
	defaultRequeueDelay  = time.Minute
)

// Configuration constants
const (
	imageConfigMap              = "trustyai-service-operator-config"
	imageConfigMapCustom        = "trustyai-service-operator-config-custom"
	configMapOAuthProxyImageKey = "oauthProxyImage"
	configMapServiceImageKey    = "trustyaiServiceImage"
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
)

// Event reasons
const (
	EventReasonPVCCreated                 = "PVCCreated"
	EventReasonInferenceServiceConfigured = "InferenceServiceConfigured"
	EventReasonServiceMonitorCreated      = "ServiceMonitorCreated"
)

package v1

import (
	"github.com/trustyai-explainability/trustyai-service-operator/api/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EvalHub is the Schema for the evalhubs API
// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.ready`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type EvalHub struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   EvalHubSpec   `json:"spec,omitempty"`
	Status EvalHubStatus `json:"status,omitempty"`
}

// Hub marks this version as the conversion hub.
func (*EvalHub) Hub() {}

// DatabaseSpec defines database configuration for EvalHub
type DatabaseSpec struct {
	// Type of the database backend. Must be explicitly set.
	// +kubebuilder:validation:Enum=sqlite;postgresql
	Type string `json:"type"`
	// Name of the K8s Secret containing a pre-composed db-url key.
	// Required when type is "postgresql"; ignored for "sqlite".
	// +optional
	Secret string `json:"secret,omitempty"`
	// Maximum number of open database connections (postgresql only)
	// +kubebuilder:default:=25
	// +optional
	MaxOpenConns int `json:"maxOpenConns,omitempty"`
	// Maximum number of idle database connections (postgresql only)
	// +kubebuilder:default:=5
	// +optional
	MaxIdleConns int `json:"maxIdleConns,omitempty"`
}

// OTELSpec defines OpenTelemetry configuration for EvalHub.
// Its presence in the CR spec implies OTEL is enabled.
type OTELSpec struct {
	// +kubebuilder:default:="otlp-grpc"
	// +kubebuilder:validation:Enum=otlp-grpc;otlp-http;stdout
	ExporterType     string `json:"exporterType,omitempty"`
	ExporterEndpoint string `json:"exporterEndpoint,omitempty"`
	// +kubebuilder:default:=false
	ExporterInsecure bool `json:"exporterInsecure,omitempty"`
	// Trace sampling ratio as a string-encoded float between "0" and "1" (e.g. "0.5").
	// Defaults to "1.0" (sample everything) when omitted.
	// +kubebuilder:default:="1.0"
	// +optional
	SamplingRatio string `json:"samplingRatio,omitempty"`
	// +kubebuilder:default:=true
	EnableTracing bool `json:"enableTracing,omitempty"`
	// +optional
	EnableMetrics bool `json:"enableMetrics,omitempty"`
	// +optional
	EnableLogs bool `json:"enableLogs,omitempty"`
	// TracerTimeout is the trace export timeout (Go duration string, e.g. "30s").
	// +kubebuilder:validation:Pattern=`^((\d+(\.\d+)?(ns|us|µs|ms|s|m|h))+)$`
	// +optional
	TracerTimeout string `json:"tracerTimeout,omitempty"`
	// TracerBatchInterval is the trace batch flush interval (Go duration string, e.g. "5s").
	// +kubebuilder:validation:Pattern=`^((\d+(\.\d+)?(ns|us|µs|ms|s|m|h))+)$`
	// +optional
	TracerBatchInterval string `json:"tracerBatchInterval,omitempty"`
	// EnableJobContainerLogs exports adapter container logs at job terminal transition.
	// Requires enableLogs.
	// +optional
	EnableJobContainerLogs bool `json:"enableJobContainerLogs,omitempty"`
	// ServiceName overrides the default OTEL service.name resource attribute.
	// +optional
	ServiceName string `json:"serviceName,omitempty"`
	// EnableEcsResourceDetection enables ECS resource detection on the OTEL resource.
	// +optional
	EnableEcsResourceDetection bool `json:"enableEcsResourceDetection,omitempty"`
	// DisableRedirectOtelLogs prevents OTEL SDK diagnostic logs from being redirected to the main logger.
	// +optional
	DisableRedirectOtelLogs bool `json:"disableRedirectOtelLogs,omitempty"`
	// DisableDatabaseOtelScans disables database query spans while still allowing DB metrics when enabled.
	// +optional
	DisableDatabaseOtelScans bool `json:"disableDatabaseOtelScans,omitempty"`
	// MetricExportInterval is the metrics export interval (Go duration string, e.g. "60s").
	// +kubebuilder:validation:Pattern=`^((\d+(\.\d+)?(ns|us|µs|ms|s|m|h))+)$`
	// +optional
	MetricExportInterval string `json:"metricExportInterval,omitempty"`
}

// EvalHubMCPSpec defines the optional MCP server deployment configuration.
type EvalHubMCPSpec struct {
	// Enabled controls whether the MCP server is deployed. Defaults to false.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Replicas is the number of MCP server replicas. Defaults to 1.
	// +kubebuilder:validation:Minimum=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Transport is the MCP client transport mode exposed by the MCP server: "http" (default) or "http-sse".
	// +kubebuilder:default:="http"
	// +kubebuilder:validation:Enum="http-sse";"http"
	// +optional
	Transport string `json:"transport,omitempty"`

	// EvalHubTransport overrides EVALHUB_TRANSPORT when set (MCP server transport: "http" or "http-sse").
	// When omitted, the value of transport is used for both --transport and EVALHUB_TRANSPORT.
	// +kubebuilder:validation:Enum="http-sse";"http"
	// +optional
	EvalHubTransport string `json:"evalHubTransport,omitempty"`

	// Env provides additional/override environment variables for the MCP server container.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// Resources defines container resource requests/limits for the MCP server.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Image overrides the default MCP server container image.
	// +optional
	Image string `json:"image,omitempty"`

	// AuthSecret is the name of a Secret containing a "token" key used by evalhub-mcp to call the EvalHub API
	// (fronted by kube-rbac-proxy). External MCP clients reach the MCP Service through a separate kube-rbac-proxy
	// sidecar and must have RBAC get/create on evalhubs/proxy for this instance.
	// +optional
	AuthSecret string `json:"authSecret,omitempty"`
}

// EvalHubMCPStatus contains status information for the optional MCP server.
type EvalHubMCPStatus struct {
	// Phase is the current lifecycle phase of the MCP server.
	// +kubebuilder:validation:Enum=Pending;Ready;Error;Disabled
	Phase string `json:"phase,omitempty"`

	// Ready indicates whether the MCP server deployment is available.
	Ready bool `json:"ready"`

	// URL is the externally accessible endpoint (Route or Service URL).
	// +optional
	URL string `json:"url,omitempty"`

	// Conditions contains detailed status conditions for the MCP server.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// EvalHubSpec defines the desired state of EvalHub
type EvalHubSpec struct {
	// Number of replicas for the eval-hub deployment
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default:=1
	Replicas *int32 `json:"replicas,omitempty"`

	// Environment variables for the eval-hub container
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// Providers is the list of OOTB provider names to mount into the deployment.
	// Each name must match a provider-name label on a ConfigMap in the operator namespace.
	// +kubebuilder:default:={"garak","garak-kfp","lm-evaluation-harness"}
	// +optional
	Providers []string `json:"providers,omitempty"`

	// Collections is the list of OOTB collection names to mount into the deployment.
	// Each name must match a collection-name label on a ConfigMap in the operator namespace.
	// +kubebuilder:default:={"leaderboard-v2","safety-and-fairness-v1","toxicity-and-ethical-principles"}
	// +optional
	Collections []string `json:"collections,omitempty"`

	// Database configuration for persistent storage.
	// This field is required: the operator will not start the service without
	// an explicit database configuration.
	// Set type to "postgresql" with a secret reference, or "sqlite" for
	// lightweight/development deployments.
	// +optional
	Database *DatabaseSpec `json:"database,omitempty"`

	// OpenTelemetry configuration for observability.
	// When set, the operator includes OTEL settings in the generated config.
	// When omitted, the service uses its defaults (OTEL disabled).
	// +optional
	Otel *OTELSpec `json:"otel,omitempty"`

	// MCP optionally enables an MCP server deployment connected to this EvalHub instance.
	// +optional
	MCP *EvalHubMCPSpec `json:"mcp,omitempty"`
}

// EvalHubStatus defines the observed state of EvalHub
type EvalHubStatus struct {
	// Current phase of the EvalHub (Pending, Ready, Error)
	// +kubebuilder:validation:Enum=Pending;Ready;Error
	Phase string `json:"phase,omitempty"`

	// Number of desired replicas
	Replicas int32 `json:"replicas,omitempty"`

	// Number of ready replicas
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// Conditions represent the latest available observations
	Conditions []common.Condition `json:"conditions,omitempty"`

	// Ready indicates whether the EvalHub is ready to serve requests
	Ready corev1.ConditionStatus `json:"ready,omitempty"`

	// URL where the EvalHub service is accessible
	URL string `json:"url,omitempty"`

	// List of active providers
	ActiveProviders []string `json:"activeProviders,omitempty"`

	// List of active collections
	ActiveCollections []string `json:"activeCollections,omitempty"`

	// Last time the status was updated
	LastUpdateTime *metav1.Time `json:"lastUpdateTime,omitempty"`

	// MCP contains status information for the optional MCP server.
	// +optional
	MCP *EvalHubMCPStatus `json:"mcp,omitempty"`
}

// +kubebuilder:object:root=true
// EvalHubList contains a list of EvalHub
type EvalHubList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []EvalHub `json:"items"`
}

func init() {
	SchemeBuilder.Register(&EvalHub{}, &EvalHubList{})
}

// SetStatus sets the status of the EvalHub
func (e *EvalHub) SetStatus(condType, reason, message string, status corev1.ConditionStatus) {
	now := metav1.Now()
	condition := common.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
	}
	found := false
	for i, cond := range e.Status.Conditions {
		if cond.Type == condType {
			e.Status.Conditions[i] = condition
			found = true
			break
		}
	}
	if !found {
		e.Status.Conditions = append(e.Status.Conditions, condition)
	}
	if condType == "Ready" {
		e.Status.Ready = status
	}
	e.Status.LastUpdateTime = &now
}

// IsReady returns true if the EvalHub is ready
func (e *EvalHub) IsReady() bool {
	return e.Status.Ready == corev1.ConditionTrue
}

// GetReplicas returns the number of replicas, defaulting to 1
func (e *EvalHubSpec) GetReplicas() int32 {
	if e.Replicas == nil {
		return 1
	}
	return *e.Replicas
}

// IsDatabaseConfigured returns true if the database spec is set with an explicit type
func (e *EvalHubSpec) IsDatabaseConfigured() bool {
	return e.Database != nil && e.Database.Type != ""
}

// IsPostgreSQL returns true if the database type is postgresql
func (e *EvalHubSpec) IsPostgreSQL() bool {
	return e.IsDatabaseConfigured() && e.Database.Type == "postgresql"
}

// IsSQLite returns true if the database type is sqlite
func (e *EvalHubSpec) IsSQLite() bool {
	return e.IsDatabaseConfigured() && e.Database.Type == "sqlite"
}

// IsOTELConfigured returns true if the OTEL spec is set
func (e *EvalHubSpec) IsOTELConfigured() bool {
	return e.Otel != nil
}

// IsMCPEnabled returns true if the MCP server is explicitly enabled
func (e *EvalHubSpec) IsMCPEnabled() bool {
	return e.MCP != nil && e.MCP.Enabled != nil && *e.MCP.Enabled
}

// GetMCPReplicas returns the number of MCP server replicas, defaulting to 1 when the spec,
// MCP block, or replicas pointer is unset.
func (e *EvalHubSpec) GetMCPReplicas() int32 {
	if e == nil || e.MCP == nil {
		return 1
	}
	return e.MCP.getMCPReplicas()
}

// getMCPReplicas returns MCP replica count; the receiver must be non-nil — use EvalHubSpec.GetMCPReplicas otherwise.
func (e *EvalHubMCPSpec) getMCPReplicas() int32 {
	if e.Replicas == nil {
		return 1
	}
	return *e.Replicas
}

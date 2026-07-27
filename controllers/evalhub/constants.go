package evalhub

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	// Service name for registration
	ServiceName = "EVALHUB"

	// Container configuration
	containerName = "evalhub"
	// evalHubAppPort is the eval-hub container listen port on loopback (API_HOST=127.0.0.1, PORT in deployment env).
	// kube-rbac-proxy upstream is http://127.0.0.1:<evalHubAppPort>/ on the pod network (TLS is terminated on servicePort).
	evalHubAppPort = 8444
	// evalHubInternalHealthPath is the health check path used for kubelet startup/readiness probes and passed to
	// kube-rbac-proxy via --ignore-paths so probes are not subject to SAR. The eval-hub service serves this path
	// without requiring identity headers (X-Tenant / X-User), making it safe for unauthenticated kubelet probes.
	evalHubInternalHealthPath = "/api/v1/health"
	// kubeRBACProxyHealthPath is served by kube-rbac-proxy on kubeRBACProxyHealthPort (see --proxy-endpoints-port).
	kubeRBACProxyHealthPath = "/healthz"

	// Service configuration (public HTTPS targets kube-rbac-proxy on this port)
	servicePort = 8443
	// metricsPort is the dedicated Prometheus metrics port on the EvalHub container.
	// Bound to 0.0.0.0, serves /metrics over plain HTTP with no auth (cluster-internal only).
	// Port 8081 is used (not 9090) because the redhat-ods-applications namespace NetworkPolicy
	// allowlists 8081 but not 9090, avoiding the need for a component-level NetworkPolicy.
	metricsPort = 8081

	// kube-rbac-proxy sidecar
	kubeRBACProxyContainerName       = "kube-rbac-proxy"
	kubeRBACProxyConfigMountPath     = "/etc/kube-rbac-proxy/auth.yaml"
	evalHubAuthConfigMapKey          = "auth.yaml"
	kubeRBACProxyUpstreamCAMountPath = "/etc/kube-rbac-proxy/upstream-ca"
	kubeRBACProxyHealthPort          = 9443

	// Configuration constants
	configMapName                  = "trustyai-service-operator-config"
	configMapEvalHubImageKey       = "evalHubImage"
	configMapKubeRBACProxyImageKey = "kube-rbac-proxy"

	// Operator ConfigMap keys — optional EvalHub / kube-rbac-proxy container CPU and memory.
	configMapEvalHubCPURequestKey    = "evalHubCPURequest"
	configMapEvalHubMemoryRequestKey = "evalHubMemoryRequest"
	configMapEvalHubCPULimitKey      = "evalHubCPULimit"
	configMapEvalHubMemoryLimitKey   = "evalHubMemoryLimit"

	configMapKubeRBACProxyCPURequestKey    = "kubeRBACProxyCPURequest"
	configMapKubeRBACProxyMemoryRequestKey = "kubeRBACProxyMemoryRequest"
	configMapKubeRBACProxyCPULimitKey      = "kubeRBACProxyCPULimit"
	configMapKubeRBACProxyMemoryLimitKey   = "kubeRBACProxyMemoryLimit"

	// TLS configuration (OpenShift service serving certificates)
	tlsSecretMountPath = "/etc/tls/private"
	tlsCertFile        = "tls.crt"
	tlsKeyFile         = "tls.key"

	// Database configuration
	dbSecretVolumeName = "evalhub-db-secret"
	dbSecretMountPath  = "/etc/evalhub/secrets"
	dbSecretKey        = "db-url"
	dbDriver           = "pgx"
	dbDefaultMaxOpen   = 25
	dbDefaultMaxIdle   = 5

	// Service CA configuration
	serviceCAVolumeName = "service-ca"
	serviceCAMountPath  = "/etc/evalhub/ca"
	serviceCACertFile   = "service-ca.crt"

	// MLFlow projected token configuration
	mlflowTokenVolumeName = "mlflow-token"
	mlflowTokenMountPath  = "/var/run/secrets/mlflow"
	mlflowTokenFile       = "token"
	mlflowTokenExpiration = 3600 // seconds

	// EvalHub config directory (contains config.yaml and providers/ subdir)
	configDirPath = "/etc/evalhub/config"

	// Provider ConfigMap configuration
	providerLabel       = "trustyai.opendatahub.io/evalhub-provider-type"
	providerNameLabel   = "trustyai.opendatahub.io/evalhub-provider-name"
	providersVolumeName = "evalhub-providers"
	providersMountPath  = configDirPath + "/providers"

	// Sidecar configuration
	sidecarBaseURL = "http://localhost:8080"

	// MCP server configuration
	mcpBinaryPath    = "/app/evalhub-mcp"
	mcpContainerName = "evalhub-mcp"
	// mcpAppPort is the evalhub-mcp listen port on loopback; kube-rbac-proxy terminates TLS on mcpServicePort.
	mcpAppPort             = 8445
	mcpServicePort         = 8443
	mcpHealthPath          = "/health"
	mcpConfigVolumeName    = "evalhub-mcp-config"
	mcpConfigMountPath     = "/etc/evalhub-mcp"
	mcpConfigFileName      = "config.yaml"
	mcpServiceCAVolumeName = "mcp-service-ca"
	mcpServiceCAMountPath  = "/etc/evalhub-mcp/ca"

	// Discovery ConfigMap injected into every tenant namespace for EvalHub service URL resolution.
	// A single well-known CM is shared across all EvalHub instances; each instance owns one
	// key of the form "{instanceName}.url" so multiple EvalHub instances can coexist.
	discoveryConfigMapName  = "evalhub-discovery"
	discoveryConfigMapLabel = "evalhub.trustyai.opendatahub.io/discovery"

	// Collection ConfigMap configuration
	collectionLabel       = "trustyai.opendatahub.io/evalhub-collection-type"
	collectionNameLabel   = "trustyai.opendatahub.io/evalhub-collection-name"
	collectionsVolumeName = "evalhub-collections"
	collectionsMountPath  = configDirPath + "/collections"

	// Tenancy
	invalidPlacementReason = "InvalidPlacement"

	// Single-tenancy convenience Role names (namespace-scoped, created in instance namespace)
	tenantAdminRoleName    = "evalhub-tenant-admin"
	tenantUserRoleName     = "evalhub-user"
	tenantAdminBindingName = "evalhub-tenant-admin-binding"

	// Tenant ConfigMap label value distinguishing instance-namespace ConfigMaps from system (operator-namespace) ones.
	providerTenantValue   = "tenant"
	collectionTenantValue = "tenant"
	providerSystemValue   = "system"
	collectionSystemValue = "system"

	// Tenant provider/collection ConfigMap volume and mount path configuration.
	// Tenant CMs are mounted at a separate /tenant/ sub-path so they cannot shadow system configs.
	providersTenantVolumeName   = "evalhub-providers-tenant"
	providersTenantMountPath    = configDirPath + "/providers/tenant"
	collectionsTenantVolumeName = "evalhub-collections-tenant"
	collectionsTenantMountPath  = configDirPath + "/collections/tenant"
)

var (
	// Default resource requirements for the eval-hub app container (overridable via operator ConfigMap).
	defaultResourceRequirements = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("2000m"),
			corev1.ResourceMemory: resource.MustParse("2Gi"),
		},
	}

	// defaultKubeRBACProxyResourceRequirements are defaults for the kube-rbac-proxy sidecar (overridable via operator ConfigMap).
	defaultKubeRBACProxyResourceRequirements = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
	}

	// Default MCP resource requirements
	defaultMCPResourceRequirements = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
	}

	// Default security context
	allowPrivilegeEscalation = false
	runAsNonRoot             = true
	defaultSecurityContext   = &corev1.SecurityContext{
		AllowPrivilegeEscalation: &allowPrivilegeEscalation,
		RunAsNonRoot:             &runAsNonRoot,
		// RunAsUser omitted to let OpenShift assign from allowed range
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{
				"ALL",
			},
		},
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}

	// Default pod security context
	runAsNonRootUser          = true
	defaultPodSecurityContext = &corev1.PodSecurityContext{
		RunAsNonRoot: &runAsNonRootUser,
		// FSGroup omitted to let OpenShift assign from allowed range
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
)

package evalhub

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	// Service name for registration
	ServiceName = "EVALHUB"

	// Default image configuration
	defaultEvalHubImage = "quay.io/evalhub/evalhub:latest"

	// Container configuration
	containerName = "evalhub"
	// evalHubAppPort is the eval-hub container listen port on loopback (API_HOST=127.0.0.1, PORT in deployment env).
	// kube-rbac-proxy upstream is http://127.0.0.1:<evalHubAppPort>/ on the pod network (TLS is terminated on servicePort).
	evalHubAppPort = 8444
	// evalHubHealthPath is the application health check path. kube-rbac-proxy serves HTTPS on servicePort and forwards
	// this path to the upstream; the proxy is started with --ignore-paths so kubelet HTTPS probes against the proxy
	// can use this path without going through normal authn/z on every request.
	evalHubHealthPath = "/api/v1/health"

	// Service configuration (public HTTPS targets kube-rbac-proxy on this port)
	serviceName = "evalhub"
	servicePort = 8443

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
	defaultKubeRBACProxyImage      = "quay.io/openshift/origin-kube-rbac-proxy:4.19"

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

	// Route configuration
	routeName = "evalhub"

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

	// Collection ConfigMap configuration
	collectionLabel       = "trustyai.opendatahub.io/evalhub-collection-type"
	collectionNameLabel   = "trustyai.opendatahub.io/evalhub-collection-name"
	collectionsVolumeName = "evalhub-collections"
	collectionsMountPath  = configDirPath + "/collections"
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

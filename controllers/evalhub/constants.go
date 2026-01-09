package evalhub

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	// Service name for registration
	ServiceName = "EVALHUB"

	// Default image configuration
	defaultEvalHubImage = "quay.io/ruimvieira/eval-hub:latest"

	// Container configuration
	containerName = "evalhub"
	containerPort = 8000

	// Service configuration
	serviceName = "evalhub"
	servicePort = 8000

	// Configuration constants
	configMapName                  = "trustyai-service-operator-config"
	configMapEvalHubImageKey       = "evalHubImage"
	configMapKubeRBACProxyImageKey = "kubeRBACProxyImage"

	// kube-rbac-proxy configuration
	kubeRBACProxyPort = 8443

	// Route configuration
	routeName = "evalhub"
)

var (
	// Default resource requirements based on k8s examples
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

	// Default security context
	allowPrivilegeEscalation = false
	runAsNonRoot             = true
	runAsUser                = int64(1001)
	defaultSecurityContext   = &corev1.SecurityContext{
		AllowPrivilegeEscalation: &allowPrivilegeEscalation,
		RunAsNonRoot:             &runAsNonRoot,
		RunAsUser:                &runAsUser,
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
	fsGroup                   = int64(1001)
	defaultPodSecurityContext = &corev1.PodSecurityContext{
		RunAsNonRoot: &runAsNonRootUser,
		FSGroup:      &fsGroup,
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
)

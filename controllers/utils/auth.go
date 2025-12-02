package utils

import (
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
)

type KubeRBACProxyConfig struct {
	Suffix             string
	Name               string
	Namespace          string
	KubeRBACProxyImage string
	UpstreamProtocol   string
	UpstreamHost       string
	UpstreamPort       int
	DownstreamPort     int
	HealthPort         int
}

// requiresAuth checks if the auth annotation key is set in the resource
func RequiresAuth(object metav1.Object) bool {
	val, ok := object.GetAnnotations()[constants.AuthAnnotationKey]
	return ok && strings.ToLower(val) == "true"
}

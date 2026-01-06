package utils

import (
	lmesv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/lmes/v1alpha1"
	"os"

	appsv1 "k8s.io/api/apps/v1"
)

func IsDeploymentReady(deployment *appsv1.Deployment) bool {
	return deployment.Status.Replicas == deployment.Status.UpdatedReplicas &&
		deployment.Status.Replicas == deployment.Status.AvailableReplicas
}

// StringPointer makes a copy of the provided string and returns a pointer to the copy
func StringPointer(s string) *string {
	newStr := s
	return &newStr
}

// containsString checks if a list contains a string
func ContainsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// removeString removes a string from a list
func RemoveString(list []string, s string) []string {
	newList := []string{}
	for _, v := range list {
		if v != s {
			newList = append(newList, v)
		}
	}
	return newList
}

// allTrue checks if all values in a bool array are true, returns false otherwise
func AllTrue(array []bool) bool {
	for _, b := range array {
		if !b {
			return false
		}
	}
	return true
}

// GetNamespace returns the namespace of a pod
func GetNamespace() (string, error) {
	ns, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return "", err
	}
	return string(ns), nil
}

// generateTLSServiceURL generates an internal URL for a TLS-enabled TrustyAI service
func GenerateTLSServiceURL(crName string, namespace string) string {
	return "https://" + crName + "." + namespace + ".svc"
}

// generateNonTLSServiceURL generates an internal URL for a TrustyAI service
func GenerateNonTLSServiceURL(crName string, namespace string) string {
	return "http://" + crName + "." + namespace + ".svc"
}

// generateKServeLoggerURL generates an logger url for KServe Inference Loggers
func GenerateKServeLoggerURL(crName string, namespace string) string {
	return "http://" + crName + "." + namespace + ".svc.cluster.local"
}

// generateHTTPSKServeLoggerURL generates an HTTPS logger url for KServe Inference Loggers
func GenerateHTTPSKServeLoggerURL(crName string, namespace string) string {
	return "https://" + crName + "." + namespace + ".svc.cluster.local"
}

func ProgressTriggeredChange(a, b *lmesv1alpha1.ProgressBar) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Percent == b.Percent && a.Message == b.Message
}

func ProgressArrayTriggeredChange(a, b []lmesv1alpha1.ProgressBar) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !ProgressTriggeredChange(&a[i], &b[i]) {
			return false
		}
	}
	return true
}

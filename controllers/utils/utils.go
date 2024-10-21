package utils

import (
	"os"

	appsv1 "k8s.io/api/apps/v1"
)

func IsDeploymentReady(deployment *appsv1.Deployment) bool {
	return deployment.Status.Replicas == deployment.Status.UpdatedReplicas &&
		deployment.Status.Replicas == deployment.Status.AvailableReplicas
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

package module

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ServiceHealthChecker defines the interface for service-level health checking
type ServiceHealthChecker interface {
	// Name returns the service name (e.g., "TAS", "LMES", "EVALHUB")
	Name() string

	// IsHealthy checks if the service's workloads are healthy
	// Returns: (healthy bool, reason string explaining health status)
	IsHealthy(ctx context.Context) (bool, string)
}

// RunningServiceChecker checks if a service's Deployment is available
type RunningServiceChecker struct {
	serviceName string
	client      client.Client
	namespace   string
}

// NewRunningServiceChecker creates a new deployment-based health checker for a service
func NewRunningServiceChecker(name string, c client.Client, ns string) *RunningServiceChecker {
	return &RunningServiceChecker{
		serviceName: name,
		client:      c,
		namespace:   ns,
	}
}

// Name returns the service name
func (r *RunningServiceChecker) Name() string {
	return r.serviceName
}

// IsHealthy checks if the service's Deployment has ready replicas
// Returns (true, "Deployment ready") if ReadyReplicas == Replicas && ReadyReplicas > 0
// Returns (false, reason) otherwise with specific error details
func (r *RunningServiceChecker) IsHealthy(ctx context.Context) (bool, string) {
	// Try to find deployment by service name label
	deploymentList := &appsv1.DeploymentList{}
	labelSelector := labels.SelectorFromSet(labels.Set{
		"app.kubernetes.io/name": r.serviceName,
	})

	if err := r.client.List(ctx, deploymentList, &client.ListOptions{
		Namespace:     r.namespace,
		LabelSelector: labelSelector,
	}); err != nil {
		return false, fmt.Sprintf("failed to list deployments: %v", err)
	}

	if len(deploymentList.Items) == 0 {
		// Try alternative: look for deployment with name matching service
		deployment := &appsv1.Deployment{}
		err := r.client.Get(ctx, types.NamespacedName{
			Name:      r.serviceName,
			Namespace: r.namespace,
		}, deployment)

		if err != nil {
			return false, "deployment not found"
		}

		return r.checkDeploymentHealth(deployment)
	}

	// Check the first matching deployment
	return r.checkDeploymentHealth(&deploymentList.Items[0])
}

// checkDeploymentHealth verifies deployment replica readiness
func (r *RunningServiceChecker) checkDeploymentHealth(deployment *appsv1.Deployment) (bool, string) {
	if deployment.Spec.Replicas == nil {
		return false, "deployment has no replica count specified"
	}

	desiredReplicas := *deployment.Spec.Replicas
	readyReplicas := deployment.Status.ReadyReplicas

	if desiredReplicas == 0 {
		return false, "deployment scaled to 0 replicas"
	}

	if readyReplicas == 0 {
		return false, fmt.Sprintf("0/%d replicas ready", desiredReplicas)
	}

	if readyReplicas < desiredReplicas {
		return false, fmt.Sprintf("%d/%d replicas ready", readyReplicas, desiredReplicas)
	}

	return true, "deployment ready"
}

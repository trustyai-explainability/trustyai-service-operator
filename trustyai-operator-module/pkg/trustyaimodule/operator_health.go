package trustyaimodule

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// OperatorHealthChecker checks the Deployment running the TrustyAI service
// operator. Deployment availability includes the readiness of the operator
// Pod, so this check verifies that the workload operator is serving before the
// module reports Ready.
type OperatorHealthChecker struct {
	client    client.Client
	namespace string
}

func NewOperatorHealthChecker(c client.Client, namespace string) *OperatorHealthChecker {
	return &OperatorHealthChecker{client: c, namespace: namespace}
}

func (r *OperatorHealthChecker) Name() string { return "TrustyAI operator" }

func (r *OperatorHealthChecker) Check(ctx context.Context) ServiceHealthResult {
	deployment := &appsv1.Deployment{}
	key := types.NamespacedName{Name: OperatorDeploymentName, Namespace: r.namespace}
	if err := r.client.Get(ctx, key, deployment); err != nil {
		if errors.IsNotFound(err) {
			return ServiceHealthResult{Reason: "operator Deployment not found"}
		}
		return ServiceHealthResult{
			Reason:  fmt.Sprintf("failed to get operator Deployment: %v", err),
			Unknown: true,
		}
	}

	desired := int32(1)
	if deployment.Spec.Replicas != nil {
		desired = *deployment.Spec.Replicas
	}
	if desired == 0 {
		return ServiceHealthResult{Reason: "operator Deployment is scaled to zero", Degraded: true}
	}

	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentReplicaFailure && condition.Status == "True" {
			message := condition.Message
			if message == "" {
				message = condition.Reason
			}
			if message == "" {
				message = "operator Deployment has a replica failure"
			}
			return ServiceHealthResult{Reason: message, Degraded: true}
		}
	}

	if deployment.Status.ObservedGeneration != deployment.Generation ||
		deployment.Status.UpdatedReplicas < desired ||
		deployment.Status.ReadyReplicas < desired ||
		deployment.Status.AvailableReplicas < desired {
		return ServiceHealthResult{Reason: fmt.Sprintf(
			"operator Deployment is not ready (%d/%d replicas available)",
			deployment.Status.AvailableReplicas, desired,
		)}
	}

	return ServiceHealthResult{
		Healthy: true,
		Reason:  fmt.Sprintf("operator Deployment has %d available replica(s)", deployment.Status.AvailableReplicas),
	}
}

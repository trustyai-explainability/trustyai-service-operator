package utils

import (
	"context"
	stderrors "errors"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ErrDeploymentProgressDeadlineExceeded means the rollout is stuck and will not become ready.
var ErrDeploymentProgressDeadlineExceeded = stderrors.New("deployment exceeded its progress deadline")

// CheckDeploymentReady reports whether the named Deployment has finished rolling out.
func CheckDeploymentReady(ctx context.Context, c client.Client, name string, namespace string) (bool, error) {
	deployment := &appsv1.Deployment{}
	if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, deployment); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	if deployment.Generation > deployment.Status.ObservedGeneration {
		return false, nil
	}

	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentProgressing && condition.Reason == "ProgressDeadlineExceeded" {
			return false, fmt.Errorf("%s: %w: %s", deployment.Name, ErrDeploymentProgressDeadlineExceeded, condition.Message)
		}
	}

	desired := int32(1)
	if deployment.Spec.Replicas != nil {
		desired = *deployment.Spec.Replicas
	}

	if deployment.Status.UpdatedReplicas != desired {
		return false, nil
	}
	// Old pods from the previous template are still running.
	if deployment.Status.Replicas > deployment.Status.UpdatedReplicas {
		return false, nil
	}
	// AvailableReplicas counts pods that are Ready for at least minReadySeconds.
	if deployment.Status.AvailableReplicas != desired {
		return false, nil
	}
	return true, nil
}

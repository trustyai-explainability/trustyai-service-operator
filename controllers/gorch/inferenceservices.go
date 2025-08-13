package gorch

import (
	"context"
	"fmt"

	kservev1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type NoInferenceServicesError struct {
	Namespace string
}

func (e *NoInferenceServicesError) Error() string {
	return fmt.Sprintf("no InferenceServices found in namespace %s", e.Namespace)
}

func (r *GuardrailsOrchestratorReconciler) checkGeneratorPresent(ctx context.Context, namespace string) (bool, error) {
	isvcList := &kservev1beta1.InferenceServiceList{}
	if err := r.List(ctx, isvcList, client.InNamespace(namespace)); err != nil {
		return false, fmt.Errorf("failed to list InferenceServices: %w", err)
	}

	if len(isvcList.Items) == 0 {
		return false, &NoInferenceServicesError{Namespace: namespace}
	}

	for _, isvc := range isvcList.Items {
		for _, condition := range isvc.Status.Conditions {
			if condition.Type == "Ready" && condition.Status == "True" {
				return true, nil
			}
		}
	}

	return false, nil
}
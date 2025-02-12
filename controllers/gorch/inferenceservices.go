package gorch

import (
	"context"

	kservev1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *GuardrailsOrchestratorReconciler) checkGeneratorPresent(ctx context.Context, namespace string) (bool, error) {
	isvcList := &kservev1beta1.InferenceServiceList{}
	if err := r.List(ctx, isvcList, client.InNamespace(namespace)); err != nil {
		return false, err
	}

	return len(isvcList.Items) > 0, nil
}

package tas

import (
	"context"
	"strings"

	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *TrustyAIServiceReconciler) checkDatabaseAccessible(ctx context.Context, instance *trustyaiopendatahubiov1.TrustyAIService) (bool, error) {
	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, deployment)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	for _, cond := range deployment.Status.Conditions {
		if cond.Type == appsv1.DeploymentAvailable && cond.Status == corev1.ConditionTrue {
			podList := &corev1.PodList{}
			listOpts := []client.ListOption{
				client.InNamespace(instance.Namespace),
				client.MatchingLabels(deployment.Spec.Selector.MatchLabels),
			}
			if err := r.List(ctx, podList, listOpts...); err != nil {
				return false, err
			}

			for _, pod := range podList.Items {
				for _, cs := range pod.Status.ContainerStatuses {
					if cs.Name == "trustyai-service" {
						if cs.State.Running != nil {
							return true, nil
						}

						if cs.LastTerminationState.Terminated != nil {
							termination := cs.LastTerminationState.Terminated
							if termination.Reason == "Error" && termination.Message != "" {
								if strings.Contains(termination.Message, "Socket fail to connect to host:address") {
									return false, nil
								}
							}
						}

						if cs.State.Waiting != nil && cs.State.Waiting.Reason == StateReasonCrashLoopBackOff {
							return false, nil
						}
					}
				}
			}
		}
	}

	return false, nil
}

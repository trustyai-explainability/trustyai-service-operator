package utils

import (
	"context"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

func CheckDeploymentReady(ctx context.Context, c client.Client, name string, namespace string) (bool, error) {
	var err error
	var deployment *appsv1.Deployment
	err = retry.OnError(
		wait.Backoff{
			Duration: 5 * time.Second,
		},
		func(err error) bool {
			return errors.IsNotFound(err) || err != nil
		},
		func() error {
			err = c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, deployment)
			if err != nil {
				return err
			}
			for _, condition := range deployment.Status.Conditions {
				if condition.Type == appsv1.DeploymentAvailable && condition.Status == "True" {
					err = CheckPodsReady(ctx, c, *deployment)
				}
			}
			return fmt.Errorf("deployment %s is not ready", deployment.Name)
		},
	)
	return true, nil
}

func CheckPodsReady(ctx context.Context, c client.Client, deployment appsv1.Deployment) error {
	podList := &corev1.PodList{}
	if err := c.List(ctx, podList, client.InNamespace(deployment.Namespace)); err != nil {
		return err
	}
	// check if all pods are all ready
	for _, pod := range podList.Items {
		for _, cs := range pod.Status.ContainerStatuses {
			if !cs.Ready {
				return fmt.Errorf("pod %s is not ready", pod.Name)
			}
		}
	}
	return nil
}

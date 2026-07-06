package utils

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ServiceAccountConfig struct {
	Owner metav1.Object
	Name  string
}

const serviceAccountResourceKind = "ServiceAccount"

// getServiceAccountName creates a service account name from the orchestrator name
func GetServiceAccountName(owner metav1.Object) string {
	return owner.GetName() + "-serviceaccount"
}

func ReconcileServiceAccount(ctx context.Context, c client.Client, owner metav1.Object) error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetServiceAccountName(owner),
			Namespace: owner.GetNamespace(),
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "trustyai-service-operator",
				"app":                          owner.GetName(),
				"component":                    owner.GetNamespace(),
			},
		},
	}
	_, _, err := ReconcileGenericManuallyDefined[*corev1.ServiceAccount](ctx, c, serviceAccountResourceKind, owner, sa)
	return err
}

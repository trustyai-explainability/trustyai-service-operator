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

func ReconcileServiceAccount(ctx context.Context, c client.Client, owner metav1.Object, name string, serviceAccountTemplatePath string, parser ResourceParserFunc[*corev1.ServiceAccount]) error {
	genericConfig := GetGenericConfig(
		StringPointer(name),
		StringPointer(owner.GetNamespace()),
		ServiceAccountConfig{Owner: owner, Name: name},
	)
	_, _, err := ReconcileGeneric[*corev1.ServiceAccount](ctx, c, owner, serviceAccountResourceKind, genericConfig, serviceAccountTemplatePath, parser)
	return err
}

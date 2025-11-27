package utils

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ServiceConfig struct {
	Name         string
	Namespace    string
	Owner        metav1.Object
	Version      string
	UseAuthProxy bool
}

const serviceResourceKind = "service"

func DefineService(ctx context.Context, c client.Client, owner metav1.Object, serviceConfig ServiceConfig, serviceTemplatePath string, parser ResourceParserFunc[*corev1.Service]) (*corev1.Service, error) {
	genericConfig := GetGenericConfig(&serviceConfig.Name, &serviceConfig.Namespace, serviceConfig)
	return DefineGeneric[*corev1.Service](ctx, c, owner, serviceResourceKind, genericConfig, serviceTemplatePath, parser)
}

func ReconcileService(ctx context.Context, c client.Client, owner metav1.Object, serviceConfig ServiceConfig, serviceTemplatePath string, parser ResourceParserFunc[*corev1.Service]) error {
	genericConfig := GetGenericConfig(&serviceConfig.Name, &serviceConfig.Namespace, serviceConfig)
	_, _, err := ReconcileGeneric[*corev1.Service](ctx, c, owner, serviceResourceKind, genericConfig, serviceTemplatePath, parser)
	return err
}

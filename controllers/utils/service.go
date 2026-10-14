package utils

import (
	"context"
	"reflect"

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

// CompareAndUpdateServicePorts updates svc in-cluster if its Spec.Ports differ from expectedSvc.
func CompareAndUpdateServicePorts(ctx context.Context, c client.Client, svc *corev1.Service, expectedSvc *corev1.Service) error {
	if !reflect.DeepEqual(svc.Spec.Ports, expectedSvc.Spec.Ports) {
		LogInfoUpdating(ctx, serviceResourceKind, svc.Name, svc.Namespace)

		svc.Spec.Ports = expectedSvc.Spec.Ports

		if err := c.Update(ctx, svc); err != nil {
			LogErrorUpdating(ctx, err, serviceResourceKind, svc.Name, svc.Namespace)
			return err
		}
	}
	return nil
}

// EnsureService creates the Service if it is missing, or updates it if it does not match the template.
func EnsureService(ctx context.Context, c client.Client, owner metav1.Object, serviceConfig ServiceConfig, serviceTemplatePath string, parser ResourceParserFunc[*corev1.Service]) error {
	genericConfig := GetGenericConfig(&serviceConfig.Name, &serviceConfig.Namespace, serviceConfig)
	svc, justCreated, err := ReconcileGeneric[*corev1.Service](ctx, c, owner, serviceResourceKind, genericConfig, serviceTemplatePath, parser)
	if err != nil {
		return err
	}
	if justCreated {
		return nil
	}

	expectedSvc, err := DefineService(ctx, c, owner, serviceConfig, serviceTemplatePath, parser)
	if err != nil {
		LogErrorVerb(ctx, err, "defining reference", serviceResourceKind, serviceConfig.Name, serviceConfig.Namespace)
		return err
	}

	return CompareAndUpdateServicePorts(ctx, c, svc, expectedSvc)
}

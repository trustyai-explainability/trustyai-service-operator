package utils

import (
	"context"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"reflect"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type ServiceConfig struct {
	Owner     *metav1.Object
	Name      string
	Namespace string
	Version   string
}

func CreateService(ctx context.Context, c client.Client, owner metav1.Object, templatePath string, parser ResourceParserFunc[corev1.Service]) (*corev1.Service, error) {
	serviceConfig := ServiceConfig{
		Owner:     &owner,
		Name:      owner.GetName(),
		Namespace: owner.GetNamespace(),
		Version:   constants.Version,
	}
	var service *corev1.Service
	service, err := parser(templatePath, serviceConfig, reflect.TypeOf(&corev1.Service{}))
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to parse service template")
		return nil, err
	}
	err = controllerutil.SetControllerReference(owner, service, c.Scheme())
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to set controller reference")
		return nil, err
	}
	return service, nil
}

func ReconcileService(ctx context.Context, c client.Client, owner metav1.Object, templatePath string, parserFunc ResourceParserFunc[corev1.Service]) (ctrl.Result, error) {
	existingService := &corev1.Service{}
	err := c.Get(ctx, types.NamespacedName{Name: owner.GetName() + "-service", Namespace: owner.GetNamespace()}, existingService)
	if err != nil && errors.IsNotFound(err) {
		// Define a new service
		service, err := CreateService(ctx, c, owner, templatePath, parserFunc)
		if err != nil {
			log.FromContext(ctx).Error(err, "Failed to define service", "service", owner.GetName(), "namespace", owner.GetNamespace())
			return ctrl.Result{}, err
		}
		log.FromContext(ctx).Info("Creating a new Service", "Service.Namespace", owner.GetNamespace(), "Service.Name", owner.GetName())
		err = c.Create(ctx, service)
		if err != nil {
			log.FromContext(ctx).Error(err, "Failed to create new Service", "Service.Namespace", owner.GetNamespace(), "Service.Name", owner.GetName())
			return ctrl.Result{}, err
		}
	} else if err != nil {
		log.FromContext(ctx).Error(err, "Failed to get Service")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

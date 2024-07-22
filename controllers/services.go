package controllers

import (
	"context"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/templates"
	corev1 "k8s.io/api/core/v1"
	"reflect"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	serviceTemplatePath = "service/service-internal.tmpl.yaml"
)

type ServiceConfig struct {
	Name                  string
	Namespace             string
	Version               string
	UseServingCertificate bool
}

func (r *TrustyAIServiceReconciler) reconcileService(ctx context.Context, cr *trustyaiopendatahubiov1alpha1.TrustyAIService) (*corev1.Service, error) {

	// Check whether a secret is already provided
	exists, err := r.TLSExists(ctx, cr.Namespace, cr.Name+"-internal")
	if err != nil {
		return nil, err
	}

	serviceConfig := ServiceConfig{
		Name:                  cr.Name,
		Namespace:             cr.Namespace,
		Version:               Version,
		UseServingCertificate: !exists,
	}

	var service *corev1.Service
	service, err = templateParser.ParseResource[corev1.Service](serviceTemplatePath, serviceConfig, reflect.TypeOf(&corev1.Service{}))
	if err != nil {
		log.FromContext(ctx).Error(err, "Error parsing the internal service's deployment template")
		return nil, err
	}
	if err := ctrl.SetControllerReference(cr, service, r.Scheme); err != nil {
		return nil, err
	}

	return service, nil
}

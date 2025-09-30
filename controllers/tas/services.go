package tas

import (
	"context"
	"reflect"

	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/tas/templates"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	serviceTemplatePath = "service/service-internal.tmpl.yaml"
)

type ServiceConfig struct {
	Name      string
	Namespace string
	Version   string
}

func (r *TrustyAIServiceReconciler) reconcileService(ctx context.Context, cr *trustyaiopendatahubiov1.TrustyAIService) (*corev1.Service, error) {

	serviceConfig := ServiceConfig{
		Name:      cr.Name,
		Namespace: cr.Namespace,
		Version:   constants.Version,
	}

	var service *corev1.Service
	service, err := templateParser.ParseResource[corev1.Service](serviceTemplatePath, serviceConfig, reflect.TypeOf(&corev1.Service{}))
	if err != nil {
		log.FromContext(ctx).Error(err, "Error parsing the internal service's deployment template")
		return nil, err
	}
	if err := ctrl.SetControllerReference(cr, service, r.Scheme); err != nil {
		return nil, err
	}

	return service, nil
}

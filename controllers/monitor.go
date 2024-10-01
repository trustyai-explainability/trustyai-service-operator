package controllers

import (
	"context"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/templates"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"reflect"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	localServiceMonitorTemplatePath = "service/service-monitor-local.tmpl.yaml"
)

type ServiceMonitorConfig struct {
	Namespace     string
	ComponentName string
	ServiceName   string
}

// createLocalServiceMonitorObject generates the ServiceMonitor spec for a local ServiceMonitor
func createLocalServiceMonitorObject(ctx context.Context, deploymentNamespace string, serviceMonitorName string) (*monitoringv1.ServiceMonitor, error) {

	config := ServiceMonitorConfig{
		Namespace:     deploymentNamespace,
		ComponentName: componentName,
		ServiceName:   serviceMonitorName,
	}

	var serviceMonitor *monitoringv1.ServiceMonitor
	serviceMonitor, err := templateParser.ParseResource[monitoringv1.ServiceMonitor](localServiceMonitorTemplatePath, config, reflect.TypeOf(&monitoringv1.ServiceMonitor{}))
	if err != nil {
		log.FromContext(ctx).Error(err, "Error parsing the central ServiceMonitor template")
		return nil, err
	}

	return serviceMonitor, nil

}

// ensureLocalServiceMonitor ensures that the local ServiceMonitor is created
func (r *TrustyAIServiceReconciler) ensureLocalServiceMonitor(cr *trustyaiopendatahubiov1alpha1.TrustyAIService, ctx context.Context) error {
	serviceMonitor, err := createLocalServiceMonitorObject(ctx, cr.Namespace, cr.Name)
	if err != nil {
		return err
	}

	// Set TrustyAIService instance as the owner and controller
	err = ctrl.SetControllerReference(cr, serviceMonitor, r.Scheme)
	if err != nil {
		return err
	}

	// Check if the ServiceMonitor already exists
	found := &monitoringv1.ServiceMonitor{}
	err = r.Get(ctx, types.NamespacedName{Name: serviceMonitor.Name, Namespace: serviceMonitor.Namespace}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			log.FromContext(ctx).Info("Creating a new local ServiceMonitor", "ServiceMonitor.Namespace", serviceMonitor.Namespace, "ServiceMonitor.Name", serviceMonitor.Name)
			err = r.Create(ctx, serviceMonitor)
			if err != nil {
				log.FromContext(ctx).Error(err, "Failed to create local ServiceMonitor", "ServiceMonitor.Namespace", serviceMonitor.Namespace, "ServiceMonitor.Name", serviceMonitor.Name)
				return err
			} else {
				r.eventLocalServiceMonitorCreated(cr)
			}
		} else {
			log.FromContext(ctx).Error(err, "Failed to get local ServiceMonitor", "ServiceMonitor.Namespace", serviceMonitor.Namespace, "ServiceMonitor.Name", serviceMonitor.Name)
			return err
		}
	}

	return nil
}

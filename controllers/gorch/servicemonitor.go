package gorch

import (
	"context"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/gorch/templates"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const serviceMonitorTemplatePath = "service-monitor.tmpl.yaml"

type ServiceMonitorConfig struct {
	Orchestrator gorchv1alpha1.GuardrailsOrchestrator
	TargetPort   string
}

func (r *GuardrailsOrchestratorReconciler) createServiceMonitor(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) *monitoringv1.ServiceMonitor {
	targetPort := "built-in-detector"
	if utils.RequiresAuth(orchestrator) {
		targetPort = "built-in-detector-metrics"
	}

	serviceMonitorConfig := ServiceMonitorConfig{
		Orchestrator: *orchestrator,
		TargetPort:   targetPort,
	}

	var serviceMonitor *monitoringv1.ServiceMonitor
	serviceMonitor, err := templateParser.ParseResource[*monitoringv1.ServiceMonitor](serviceMonitorTemplatePath, serviceMonitorConfig, reflect.TypeOf(&monitoringv1.ServiceMonitor{}))
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to parse service monitor template")
	}
	controllerutil.SetControllerReference(orchestrator, serviceMonitor, r.Scheme)
	return serviceMonitor
}

package controllers

import (
	"context"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func generateServiceMonitorSpec(deploymentNamespace string, monitoredNamespace string, serviceName string) *monitoringv1.ServiceMonitor {
	serviceMonitor := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceMonitorName,
			Namespace: deploymentNamespace,
			Labels: map[string]string{
				"modelmesh-service": "modelmesh-serving",
			},
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			NamespaceSelector: monitoringv1.NamespaceSelector{
				MatchNames: []string{monitoredNamespace},
			},
			Endpoints: []monitoringv1.Endpoint{
				{
					Interval:    "4s",
					Path:        "/q/metrics",
					HonorLabels: true,
					Scheme:      "http",
					Params: map[string][]string{
						"match[]": {
							`{__name__= "trustyai_spd"}`,
							`{__name__= "trustyai_dir"}`,
						},
					},
					MetricRelabelConfigs: []*monitoringv1.RelabelConfig{
						{
							Action:       "keep",
							Regex:        "trustyai_.*",
							SourceLabels: []monitoringv1.LabelName{"__name__"},
						},
					},
				},
			},
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name": serviceName,
				},
			},
		},
	}
	return serviceMonitor
}

func (r *TrustyAIServiceReconciler) ensureServiceMonitor(cr *trustyaiopendatahubiov1alpha1.TrustyAIService, ctx context.Context, isLocal bool) error {
	var deploymentNamespace string
	if isLocal {
		deploymentNamespace = cr.Namespace
	} else {
		deploymentNamespace = r.Namespace
	}

	serviceMonitor := generateServiceMonitorSpec(deploymentNamespace, cr.Namespace, cr.Name)

	// Set TrustyAIService instance as the owner and controller
	err := ctrl.SetControllerReference(cr, serviceMonitor, r.Scheme)
	if err != nil {
		return err
	}

	// Check if this ServiceMonitor already exists
	found := &monitoringv1.ServiceMonitor{}
	err = r.Get(ctx, types.NamespacedName{Name: serviceMonitor.Name, Namespace: serviceMonitor.Namespace}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			var logMessage string
			if isLocal {
				logMessage = "Creating a new local ServiceMonitor"
			} else {
				logMessage = "Creating a new central ServiceMonitor"
			}

			log.FromContext(ctx).Info(logMessage, "ServiceMonitor.Namespace", serviceMonitor.Namespace, "ServiceMonitor.Name", serviceMonitor.Name)
			err = r.Create(ctx, serviceMonitor)
			if err != nil {
				log.FromContext(ctx).Error(err, "Failed to create ServiceMonitor", "ServiceMonitor.Namespace", serviceMonitor.Namespace, "ServiceMonitor.Name", serviceMonitor.Name)
				return err
			}
		} else {
			log.FromContext(ctx).Error(err, "Couldn't create new ServiceMonitor", "ServiceMonitor.Namespace", serviceMonitor.Namespace, "ServiceMonitor.Name", serviceMonitor.Name)
			return err
		}
	}

	return nil
}

func (r *TrustyAIServiceReconciler) ensureLocalServiceMonitor(cr *trustyaiopendatahubiov1alpha1.TrustyAIService, ctx context.Context) error {
	return r.ensureServiceMonitor(cr, ctx, true)
}

func (r *TrustyAIServiceReconciler) ensureCentralServiceMonitor(cr *trustyaiopendatahubiov1alpha1.TrustyAIService, ctx context.Context) error {
	return r.ensureServiceMonitor(cr, ctx, false)
}

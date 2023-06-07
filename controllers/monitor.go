package controllers

import (
	"context"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	trustyaiopendatahubiov1alpha1 "github.com/ruivieira/trustyai-service-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func defaultServiceMonitor(cr *trustyaiopendatahubiov1alpha1.TrustyAIService) *monitoringv1.ServiceMonitor {
	serviceMonitor := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name + "-metrics", // generate name based on CR name
			Namespace: cr.Namespace,
			Labels: map[string]string{
				"modelmesh-service": "modelmesh-serving",
			},
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			Endpoints: []monitoringv1.Endpoint{
				{
					Interval:        "4s",
					Path:            "/q/metrics",
					HonorLabels:     true,
					TargetPort:      &intstr.IntOrString{IntVal: 8080},
					Scheme:          "http",
					BearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
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
					"app.kubernetes.io/name": cr.Name,
				},
			},
		},
	}

	return serviceMonitor
}

func (r *TrustyAIServiceReconciler) reconcileServiceMonitor(cr *trustyaiopendatahubiov1alpha1.TrustyAIService, ctx context.Context) (*monitoringv1.ServiceMonitor, error) {
	serviceMonitor := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name + "-metrics", // Generate name based on CR name
			Namespace: cr.Namespace,
		},
	}

	if cr.Spec.ServiceMonitoring != nil {
		// If the ServiceMonitorSpec is defined in the CR, use it
		serviceMonitor.Spec = *cr.Spec.ServiceMonitoring
		log.FromContext(ctx).Info("Using ServiceMonitorSpec from CR")
	} else {
		// Otherwise, use the default values
		serviceMonitor = defaultServiceMonitor(cr)
		log.FromContext(ctx).Info("No ServiceMonitorSpec defined in CR, using default values")
	}

	// Set TrustyAIService instance as the owner and controller
	err := ctrl.SetControllerReference(cr, serviceMonitor, r.Scheme)
	if err != nil {
		return nil, err
	}

	// Check if this ServiceMonitor already exists
	found := &monitoringv1.ServiceMonitor{}
	err = r.Get(ctx, types.NamespacedName{Name: serviceMonitor.Name, Namespace: serviceMonitor.Namespace}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			log.FromContext(ctx).Info("Creating a new ServiceMonitor", "ServiceMonitor.Namespace", serviceMonitor.Namespace, "ServiceMonitor.Name", serviceMonitor.Name)
			err = r.Create(ctx, serviceMonitor)
			if err != nil {
				log.FromContext(ctx).Error(err, "Failed to create new ServiceMonitor", "ServiceMonitor.Namespace", serviceMonitor.Namespace, "ServiceMonitor.Name", serviceMonitor.Name)
				return nil, err
			}
		} else {
			log.FromContext(ctx).Error(err, "Failed to get ServiceMonitor", "ServiceMonitor.Namespace", serviceMonitor.Namespace, "ServiceMonitor.Name", serviceMonitor.Name)
			return nil, err
		}
	} else {
		// If the ServiceMonitor already exists, update it
		serviceMonitor = found.DeepCopy()
		if cr.Spec.ServiceMonitoring != nil {
			serviceMonitor.Spec = *cr.Spec.ServiceMonitoring
		}
		err = r.Update(ctx, serviceMonitor)
		if err != nil {
			log.FromContext(ctx).Error(err, "Failed to update ServiceMonitor", "ServiceMonitor.Namespace", serviceMonitor.Namespace, "ServiceMonitor.Name", serviceMonitor.Name)
			return nil, err
		}
	}

	return serviceMonitor, nil
}

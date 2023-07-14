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

var isCentralServiceMonitorCreated = false

// generateServiceMonitorSpecCentral generates the ServiceMonitor spec for central ServiceMonitor
func generateServiceMonitorSpecCentral(deploymentNamespace string) *monitoringv1.ServiceMonitor {
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
				Any: true,
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
					"app.kubernetes.io/part-of": serviceType,
				},
			},
		},
	}
	return serviceMonitor
}

// ensureCentralServiceMonitor ensures that the central ServiceMonitor is created
func (r *TrustyAIServiceReconciler) ensureCentralServiceMonitor(ctx context.Context) error {
	// Create only one central ServiceMonitor
	if isCentralServiceMonitorCreated {
		return nil
	}

	serviceMonitor := generateServiceMonitorSpecCentral(r.Namespace)

	// Check if this ServiceMonitor already exists
	found := &monitoringv1.ServiceMonitor{}
	err := r.Get(ctx, types.NamespacedName{Name: serviceMonitor.Name, Namespace: serviceMonitor.Namespace}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			log.FromContext(ctx).Info("Creating a new central ServiceMonitor", "ServiceMonitor.Namespace", serviceMonitor.Namespace, "ServiceMonitor.Name", serviceMonitor.Name)
			err = r.Create(ctx, serviceMonitor)
			if err != nil {
				log.FromContext(ctx).Error(err, "Failed to create central ServiceMonitor", "ServiceMonitor.Namespace", serviceMonitor.Namespace, "ServiceMonitor.Name", serviceMonitor.Name)
				return err
			} else {
				// Set the global variable to true after ServiceMonitor is successfully created
				isCentralServiceMonitorCreated = true
			}
		} else {
			log.FromContext(ctx).Error(err, "Failed to get central ServiceMonitor", "ServiceMonitor.Namespace", serviceMonitor.Namespace, "ServiceMonitor.Name", serviceMonitor.Name)
			return err
		}
	} else {
		// Already exists, set the global variable to true
		isCentralServiceMonitorCreated = true
	}

	return nil
}

// generateServiceMonitorSpecLocal generates the ServiceMonitor spec for a local ServiceMonitor
func generateServiceMonitorSpecLocal(deploymentNamespace string, serviceName string) *monitoringv1.ServiceMonitor {
	serviceMonitor := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: deploymentNamespace,
			Labels: map[string]string{
				"modelmesh-service": "modelmesh-serving",
			},
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			NamespaceSelector: monitoringv1.NamespaceSelector{
				MatchNames: []string{deploymentNamespace},
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
					"app.kubernetes.io/part-of": serviceType,
				},
			},
		},
	}
	return serviceMonitor
}

// ensureLocalServiceMonitor ensures that the local ServiceMonitor is created
func (r *TrustyAIServiceReconciler) ensureLocalServiceMonitor(cr *trustyaiopendatahubiov1alpha1.TrustyAIService, ctx context.Context) error {
	serviceMonitor := generateServiceMonitorSpecLocal(cr.Namespace, cr.Name)

	// Set TrustyAIService instance as the owner and controller
	err := ctrl.SetControllerReference(cr, serviceMonitor, r.Scheme)
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
			}
		} else {
			log.FromContext(ctx).Error(err, "Failed to get local ServiceMonitor", "ServiceMonitor.Namespace", serviceMonitor.Namespace, "ServiceMonitor.Name", serviceMonitor.Name)
			return err
		}
	}

	return nil
}

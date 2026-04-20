package evalhub

import (
	"context"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	metricsPath    = "/metrics"
	scrapeInterval = monitoringv1.Duration("30s")
)

func serviceMonitorName(instance *evalhubv1alpha1.EvalHub) string {
	return instance.Name + "-metrics"
}

func (r *EvalHubReconciler) buildServiceMonitor(instance *evalhubv1alpha1.EvalHub) *monitoringv1.ServiceMonitor {
	labels := map[string]string{
		"app":       "eval-hub",
		"component": "api",
		"instance":  instance.Name,
	}

	return &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceMonitorName(instance),
			Namespace: instance.Namespace,
			Labels:    labels,
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			Endpoints: []monitoringv1.Endpoint{
				{
					HonorLabels: true,
					Interval:    scrapeInterval,
					Path:        metricsPath,
					Scheme:      "https",
					Port:        "https",
					TLSConfig: &monitoringv1.TLSConfig{
						SafeTLSConfig: monitoringv1.SafeTLSConfig{
							InsecureSkipVerify: true,
						},
					},
				},
			},
			Selector: metav1.LabelSelector{
				MatchLabels: labels,
			},
			NamespaceSelector: monitoringv1.NamespaceSelector{
				MatchNames: []string{instance.Namespace},
			},
		},
	}
}

func (r *EvalHubReconciler) reconcileServiceMonitor(ctx context.Context, instance *evalhubv1alpha1.EvalHub) error {
	log := log.FromContext(ctx)
	log.Info("Reconciling ServiceMonitor", "name", serviceMonitorName(instance))

	desired := r.buildServiceMonitor(instance)
	if err := controllerutil.SetControllerReference(instance, desired, r.Scheme); err != nil {
		return err
	}

	found := &monitoringv1.ServiceMonitor{}
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), found)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("Creating ServiceMonitor", "name", desired.Name)
			return r.Create(ctx, desired)
		}
		return err
	}

	found.Spec.Endpoints = desired.Spec.Endpoints
	found.Spec.Selector = desired.Spec.Selector
	found.Spec.NamespaceSelector = desired.Spec.NamespaceSelector
	log.Info("Updating ServiceMonitor", "name", found.Name)
	return r.Update(ctx, found)
}

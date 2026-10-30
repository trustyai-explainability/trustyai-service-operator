package evalhub

import (
	"context"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	evalhubv1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	metricsPath    = "/metrics"
	scrapeInterval = monitoringv1.Duration("30s")
)

func (r *EvalHubReconciler) isServiceMonitorSupported() bool {
	if r.RESTMapper() == nil {
		return false
	}
	gvk := schema.GroupVersionKind{
		Group:   "monitoring.coreos.com",
		Version: "v1",
		Kind:    "ServiceMonitor",
	}
	_, err := r.RESTMapper().RESTMapping(gvk.GroupKind(), gvk.Version)
	return err == nil
}

func serviceMonitorName(instance *evalhubv1.EvalHub) string {
	return instance.Name + "-metrics"
}

// serviceMonitorMetadataLabels returns labels for ServiceMonitor metadata,
// including discovery labels for Prometheus.
func serviceMonitorMetadataLabels(instance *evalhubv1.EvalHub) map[string]string {
	labels := metricsServiceLabels(instance)
	labels["openshift.io/user-monitoring"] = "true"
	return labels
}

func (r *EvalHubReconciler) buildServiceMonitor(instance *evalhubv1.EvalHub) *monitoringv1.ServiceMonitor {
	return &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceMonitorName(instance),
			Namespace: instance.Namespace,
			Labels:    serviceMonitorMetadataLabels(instance),
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			Endpoints: []monitoringv1.Endpoint{
				{
					HonorLabels: true,
					Interval:    scrapeInterval,
					Path:        metricsPath,
					Scheme:      "http",
					Port:        "metrics",
				},
			},
			Selector: metav1.LabelSelector{
				MatchLabels: metricsServiceLabels(instance),
			},
			NamespaceSelector: monitoringv1.NamespaceSelector{
				MatchNames: []string{instance.Namespace},
			},
		},
	}
}

// reconcileServiceMonitor ensures a ServiceMonitor exists for the EvalHub instance.
// Reconciles metadata labels to support upgrades from versions that didn't set discovery labels.
func (r *EvalHubReconciler) reconcileServiceMonitor(ctx context.Context, instance *evalhubv1.EvalHub) error {
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

	// Reconcile metadata labels for upgrade path
	needsUpdate := false
	if found.Labels == nil {
		found.Labels = map[string]string{}
	}
	for key, value := range serviceMonitorMetadataLabels(instance) {
		if found.Labels[key] != value {
			found.Labels[key] = value
			needsUpdate = true
		}
	}

	if needsUpdate {
		log.Info("Updating ServiceMonitor labels", "name", found.Name)
		return r.Update(ctx, found)
	}

	return nil
}

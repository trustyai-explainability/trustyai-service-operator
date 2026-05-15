package evalhub

import (
	"context"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	corev1 "k8s.io/api/core/v1"
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

func serviceMonitorName(instance *evalhubv1alpha1.EvalHub) string {
	return instance.Name + "-metrics"
}

func (r *EvalHubReconciler) buildServiceMonitor(instance *evalhubv1alpha1.EvalHub) *monitoringv1.ServiceMonitor {
	labels := map[string]string{
		"app":       "eval-hub",
		"component": "api",
		"instance":  instance.Name,
	}

	serverName := instance.Name + "." + instance.Namespace + ".svc"

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
							ServerName: serverName,
							CA: monitoringv1.SecretOrConfigMap{
								ConfigMap: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: instance.Name + "-service-ca",
									},
									Key: serviceCACertFile,
								},
							},
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

// reconcileServiceMonitor ensures a ServiceMonitor exists for the EvalHub instance.
// The ServiceMonitor spec is fully derived from immutable CR fields (name, namespace),
// so it is created once and never updated in place. If the CR is deleted, the
// ServiceMonitor is garbage-collected via its owner reference.
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

	return nil
}

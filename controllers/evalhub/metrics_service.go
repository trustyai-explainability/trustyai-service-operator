package evalhub

import (
	"context"

	evalhubv1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func metricsServiceName(instance *evalhubv1.EvalHub) string {
	return instance.Name + "-metrics"
}

func metricsServiceLabels(instance *evalhubv1.EvalHub) map[string]string {
	return map[string]string{
		"app":       "eval-hub",
		"instance":  instance.Name,
		"component": "metrics",
	}
}

func (r *EvalHubReconciler) reconcileMetricsService(ctx context.Context, instance *evalhubv1.EvalHub) error {
	log := log.FromContext(ctx)
	name := metricsServiceName(instance)
	log.Info("Reconciling metrics Service", "name", name)

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
		},
	}

	getErr := r.Get(ctx, client.ObjectKeyFromObject(service), service)
	if getErr != nil && !errors.IsNotFound(getErr) {
		return getErr
	}

	desiredSpec := r.buildMetricsServiceSpec(instance)

	if errors.IsNotFound(getErr) {
		service.Spec = desiredSpec
		service.Labels = metricsServiceLabels(instance)
		if err := controllerutil.SetControllerReference(instance, service, r.Scheme); err != nil {
			return err
		}
		log.Info("Creating metrics Service", "name", name)
		return r.Create(ctx, service)
	}

	service.Spec.Ports = desiredSpec.Ports
	service.Spec.Selector = desiredSpec.Selector
	service.Spec.Type = desiredSpec.Type
	log.Info("Updating metrics Service", "name", name)
	return r.Update(ctx, service)
}

func (r *EvalHubReconciler) buildMetricsServiceSpec(instance *evalhubv1.EvalHub) corev1.ServiceSpec {
	return corev1.ServiceSpec{
		Selector: map[string]string{
			"app":       "eval-hub",
			"instance":  instance.Name,
			"component": "api",
		},
		Type: corev1.ServiceTypeClusterIP,
		Ports: []corev1.ServicePort{
			{
				Name:       "metrics",
				Port:       metricsPort,
				TargetPort: intstr.FromString("metrics"),
				Protocol:   corev1.ProtocolTCP,
			},
		},
	}
}

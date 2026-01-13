package evalhub

import (
	"context"

	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// reconcileService creates or updates the Service for EvalHub
func (r *EvalHubReconciler) reconcileService(ctx context.Context, instance *evalhubv1alpha1.EvalHub) error {
	log := log.FromContext(ctx)
	log.Info("Reconciling Service", "name", instance.Name)

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
		},
	}

	// Check if Service already exists
	getErr := r.Get(ctx, client.ObjectKeyFromObject(service), service)
	if getErr != nil && !errors.IsNotFound(getErr) {
		return getErr
	}

	// Define the desired service spec
	desiredSpec := r.buildServiceSpec(instance)

	if errors.IsNotFound(getErr) {
		// Create new Service
		service.Spec = desiredSpec
		service.Labels = map[string]string{
			"app":       "eval-hub",
			"instance":  instance.Name,
			"component": "api",
		}
		service.Annotations = map[string]string{
			"service.beta.openshift.io/serving-cert-secret-name": instance.Name + "-tls",
		}
		if err := controllerutil.SetControllerReference(instance, service, r.Scheme); err != nil {
			return err
		}
		log.Info("Creating Service", "name", service.Name)
		return r.Create(ctx, service)
	} else {
		// Update existing Service
		service.Spec.Ports = desiredSpec.Ports
		service.Spec.Selector = desiredSpec.Selector
		service.Spec.Type = desiredSpec.Type

		// Ensure OpenShift serving certificate annotation is present
		if service.Annotations == nil {
			service.Annotations = make(map[string]string)
		}
		service.Annotations["service.beta.openshift.io/serving-cert-secret-name"] = instance.Name + "-tls"

		log.Info("Updating Service", "name", service.Name)
		return r.Update(ctx, service)
	}
}

// buildServiceSpec builds the service specification for EvalHub
func (r *EvalHubReconciler) buildServiceSpec(instance *evalhubv1alpha1.EvalHub) corev1.ServiceSpec {
	labels := map[string]string{
		"app":       "eval-hub",
		"instance":  instance.Name,
		"component": "api",
	}

	serviceSpec := corev1.ServiceSpec{
		Selector: labels,
		Type:     corev1.ServiceTypeClusterIP,
		Ports: []corev1.ServicePort{
			{
				Name:       "https",
				Port:       kubeRBACProxyPort,
				TargetPort: intstr.FromString("https"),
				Protocol:   corev1.ProtocolTCP,
			},
		},
	}

	return serviceSpec
}

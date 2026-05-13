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

func mcpServiceName(instance *evalhubv1alpha1.EvalHub) string {
	if instance == nil {
		return ""
	}
	return instance.Name + "-mcp"
}

// reconcileMCPService creates or updates the Service for the MCP server.
// If MCP is disabled, any existing MCP service is deleted.
func (r *EvalHubReconciler) reconcileMCPService(ctx context.Context, instance *evalhubv1alpha1.EvalHub) error {
	log := log.FromContext(ctx)
	name := mcpServiceName(instance)

	if !instance.Spec.IsMCPEnabled() {
		return r.deleteMCPResource(ctx, instance, &corev1.Service{}, name)
	}

	log.Info("Reconciling MCP Service", "name", name)

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

	desiredSpec := r.buildMCPServiceSpec(instance)
	tlsSecretName := name + "-tls"

	if errors.IsNotFound(getErr) {
		service.Spec = desiredSpec
		service.Labels = mcpLabels(instance)
		service.Annotations = map[string]string{
			"service.beta.openshift.io/serving-cert-secret-name": tlsSecretName,
		}
		if err := controllerutil.SetControllerReference(instance, service, r.Scheme); err != nil {
			return err
		}
		log.Info("Creating MCP Service", "name", name)
		return r.Create(ctx, service)
	}
	service.Spec.Ports = desiredSpec.Ports
	service.Spec.Selector = desiredSpec.Selector
	service.Spec.Type = desiredSpec.Type
	service.Labels = mcpLabels(instance)
	if service.Annotations == nil {
		service.Annotations = make(map[string]string)
	}
	service.Annotations["service.beta.openshift.io/serving-cert-secret-name"] = tlsSecretName
	log.Info("Updating MCP Service", "name", name)
	return r.Update(ctx, service)
}

func (r *EvalHubReconciler) buildMCPServiceSpec(instance *evalhubv1alpha1.EvalHub) corev1.ServiceSpec {
	return corev1.ServiceSpec{
		Selector: mcpLabels(instance),
		Type:     corev1.ServiceTypeClusterIP,
		Ports: []corev1.ServicePort{
			{
				Name:       "https",
				Port:       mcpServicePort,
				TargetPort: intstr.FromString("https"),
				Protocol:   corev1.ProtocolTCP,
			},
		},
	}
}

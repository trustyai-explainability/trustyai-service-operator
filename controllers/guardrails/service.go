package guardrails

import (
	"context"

	guardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/guardrails/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *GuardrailsReconciler) createService(ctx context.Context, orchestrator *guardrailsv1alpha1.GuardrailsOrchestrator) (*corev1.Service, error) {
	// your logic here
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      orchestrator.Name,
			Namespace: orchestrator.Namespace,
			Labels:    map[string]string{"app": orchestrator.Name},
		},
		Spec: corev1.ServiceSpec{

			IPFamilies: []corev1.IPFamily{corev1.IPv4Protocol},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Protocol:   corev1.ProtocolTCP,
					Port:       8033,
					TargetPort: intstr.FromInt(8033),
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}
	controllerutil.SetControllerReference(orchestrator, service, r.Scheme)
	return service, nil
}

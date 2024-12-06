package guardrails

import (
	"context"

	routev1 "github.com/openshift/api/route/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	controllerutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	guardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/guardrails/v1alpha1"
)

func (r *GuardrailsReconciler) createRoute(ctx context.Context, orchestrator *guardrailsv1alpha1.GuardrailsOrchestrator) (*routev1.Route, error) {
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      orchestrator.Name,
			Namespace: orchestrator.Namespace,
			Labels:    map[string]string{"app": orchestrator.Name},
		},
		Spec: routev1.RouteSpec{
			Host: orchestrator.Name,
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: orchestrator.Name,
			},
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromString("http"),
			},
			TLS: &routev1.TLSConfig{
				Termination:                   routev1.TLSTerminationPassthrough,
				InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
			},
			WildcardPolicy: routev1.WildcardPolicyNone,
		},
	}
	controllerutil.SetControllerReference(orchestrator, route, r.Scheme)
	return route, nil
}

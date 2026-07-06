package evalhub

import (
	"context"

	routev1 "github.com/openshift/api/route/v1"
	evalhubv1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func mcpRouteName(instance *evalhubv1.EvalHub) string {
	return instance.Name + "-mcp"
}

// reconcileMCPRoute creates or updates the Route for the MCP server (OpenShift only).
// If MCP is disabled or routes are not supported, existing MCP routes are cleaned up.
func (r *EvalHubReconciler) reconcileMCPRoute(ctx context.Context, instance *evalhubv1.EvalHub) error {
	log := log.FromContext(ctx)

	if !r.isRouteSupported() {
		log.Info("Routes not supported on this cluster, skipping MCP route")
		return nil
	}

	name := mcpRouteName(instance)

	if !instance.Spec.IsMCPEnabled() {
		return r.deleteMCPResource(ctx, instance, &routev1.Route{}, name)
	}

	log.Info("Reconciling MCP Route", "name", name)

	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
		},
	}

	getErr := r.Get(ctx, client.ObjectKeyFromObject(route), route)
	if getErr != nil && !errors.IsNotFound(getErr) {
		return getErr
	}

	desiredSpec := r.buildMCPRouteSpec(instance)

	if errors.IsNotFound(getErr) {
		route.Spec = desiredSpec
		route.Labels = mcpLabels(instance)
		if err := controllerutil.SetControllerReference(instance, route, r.Scheme); err != nil {
			return err
		}
		log.Info("Creating MCP Route", "name", name)
		return r.Create(ctx, route)
	}

	route.Spec.TLS = desiredSpec.TLS
	route.Spec.To = desiredSpec.To
	route.Spec.Port = desiredSpec.Port
	log.Info("Updating MCP Route", "name", name)
	return r.Update(ctx, route)
}

func (r *EvalHubReconciler) buildMCPRouteSpec(instance *evalhubv1.EvalHub) routev1.RouteSpec {
	return routev1.RouteSpec{
		To: routev1.RouteTargetReference{
			Kind:   "Service",
			Name:   mcpServiceName(instance),
			Weight: &[]int32{100}[0],
		},
		Port: &routev1.RoutePort{
			TargetPort: intstr.FromString("https"),
		},
		TLS: &routev1.TLSConfig{
			Termination:                   routev1.TLSTerminationReencrypt,
			InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
		},
	}
}

package evalhub

import (
	"context"

	routev1 "github.com/openshift/api/route/v1"
	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// reconcileRoute creates or updates the Route for EvalHub (OpenShift only)
func (r *EvalHubReconciler) reconcileRoute(ctx context.Context, instance *evalhubv1alpha1.EvalHub) error {
	log := log.FromContext(ctx)

	// Check if routes are supported (OpenShift)
	if !r.isRouteSupported() {
		log.Info("Routes not supported on this cluster, skipping route creation")
		return nil
	}

	// Routes are enabled by default for EvalHub
	log.Info("Route creation enabled by default for EvalHub")

	log.Info("Reconciling Route", "name", instance.Name)

	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
		},
	}

	// Check if Route already exists
	getErr := r.Get(ctx, client.ObjectKeyFromObject(route), route)
	if getErr != nil && !errors.IsNotFound(getErr) {
		return getErr
	}

	// Define the desired route spec
	desiredSpec := r.buildRouteSpec(instance)

	if errors.IsNotFound(getErr) {
		// Create new Route
		route.Spec = desiredSpec
		route.Labels = map[string]string{
			"app":       "eval-hub",
			"instance":  instance.Name,
			"component": "api",
		}
		if err := controllerutil.SetControllerReference(instance, route, r.Scheme); err != nil {
			return err
		}
		log.Info("Creating Route", "name", route.Name)
		return r.Create(ctx, route)
	} else {
		// Update existing Route
		route.Spec.TLS = desiredSpec.TLS
		route.Spec.To = desiredSpec.To
		route.Spec.Port = desiredSpec.Port
		log.Info("Updating Route", "name", route.Name)
		return r.Update(ctx, route)
	}
}

// buildRouteSpec builds the route specification for EvalHub
func (r *EvalHubReconciler) buildRouteSpec(instance *evalhubv1alpha1.EvalHub) routev1.RouteSpec {
	routeSpec := routev1.RouteSpec{
		To: routev1.RouteTargetReference{
			Kind:   "Service",
			Name:   instance.Name,
			Weight: &[]int32{100}[0],
		},
		Port: &routev1.RoutePort{
			TargetPort: intstr.FromString("https"),
		},
		// Default TLS configuration for EvalHub
		TLS: &routev1.TLSConfig{
			Termination:                   routev1.TLSTerminationReencrypt,
			InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
		},
	}

	return routeSpec
}

// isRouteSupported checks if Route resources are supported in the cluster
func (r *EvalHubReconciler) isRouteSupported() bool {
	if r.RESTMapper() == nil {
		return false
	}

	// Check if the Route resource is available
	gvk := schema.GroupVersionKind{
		Group:   "route.openshift.io",
		Version: "v1",
		Kind:    "Route",
	}

	_, err := r.RESTMapper().RESTMapping(gvk.GroupKind(), gvk.Version)
	return err == nil
}

// RESTMapper returns the REST mapper for API discovery
func (r *EvalHubReconciler) RESTMapper() meta.RESTMapper {
	return r.restMapper
}

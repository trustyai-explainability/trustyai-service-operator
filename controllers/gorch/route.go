package gorch

import (
	"context"
	"reflect"

	routev1 "github.com/openshift/api/route/v1"
	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/gorch/templates"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const routeTemplatePath = "route.tmpl.yaml"

type RouteConfig struct {
	Name      string
	Namespace string
	PortName  string
	Version   string
}

func (r *GuardrailsOrchestratorReconciler) createRoute(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) *routev1.Route {
	routeHttpConfig := RouteConfig{
		Name:      orchestrator.Name,
		Namespace: orchestrator.Namespace,
		PortName:  "http",
		Version:   Version,
	}
	var route *routev1.Route
	route, err := templateParser.ParseResource[routev1.Route](routeTemplatePath, routeHttpConfig, reflect.TypeOf(&routev1.Route{}))

	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to parse route template")
	}
	controllerutil.SetControllerReference(orchestrator, route, r.Scheme)
	return route
}

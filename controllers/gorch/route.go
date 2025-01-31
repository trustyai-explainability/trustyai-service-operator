package gorch

import (
	"context"
	"reflect"

	routev1 "github.com/openshift/api/route/v1"
	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/gorch/templates"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
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

func (r *GuardrailsOrchestratorReconciler) checkRouteReady(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) (bool, error) {
	existingRoute := &routev1.Route{}
	typedNamespaceName := types.NamespacedName{Name: orchestrator.Name + "-route", Namespace: orchestrator.Namespace}
	err := r.Get(ctx, typedNamespaceName, existingRoute)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	for _, ingress := range existingRoute.Status.Ingress {
		for _, condition := range ingress.Conditions {
			if condition.Type == routev1.RouteAdmitted && condition.Status == "True" {
				return true, nil
			}
		}
	}
	return false, nil
}

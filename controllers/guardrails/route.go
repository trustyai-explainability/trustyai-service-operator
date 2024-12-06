package guardrails

import (
	"context"
	"reflect"

	routev1 "github.com/openshift/api/route/v1"
	guardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/guardrails/v1alpha1"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var routeTemplatePath = "templates/route.yaml"

type RouteConfig struct {
	Name      string
	Namespace string
	PortName  string
}

func (r *GuardrailsReconciler) ensureRoute(ctx context.Context, orchestrator *guardrailsv1alpha1.GuardrailsOrchestrator) error {
	// Check if the route already exists, if not create a new one
	existingRoute := &routev1.Route{}
	err := r.Get(ctx, types.NamespacedName{Name: orchestrator.Name, Namespace: orchestrator.Namespace}, existingRoute)
	if err != nil {
		if errors.IsNotFound(err) {
			return r.createRoute(ctx, orchestrator, routeTemplatePath)
		}
		return err
	}
	return nil
}

func (r *GuardrailsReconciler) createRoute(ctx context.Context, orchestrator *guardrailsv1alpha1.GuardrailsOrchestrator, routeTemplatePath string) error {

	routeConfig := RouteConfig{
		Name:      orchestrator.Name,
		Namespace: orchestrator.Namespace,
		PortName:  "http",
	}

	var route *routev1.Route
	route, err := templateParser.ParseResource[routev1.Route](routeTemplatePath, routeConfig, reflect.TypeOf(routev1.Route{}))

	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to parse route template")
		return err
	}
	controllerutil.SetControllerReference(orchestrator, route, r.Scheme)

	return nil
}

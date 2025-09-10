package gorch

import (
	"context"
	"fmt"
	"reflect"
	"time"

	routev1 "github.com/openshift/api/route/v1"
	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/gorch/templates"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type RouteConfig struct {
	Orchestrator  *gorchv1alpha1.GuardrailsOrchestrator
	UseOAuthProxy bool
}

func (r *GuardrailsOrchestratorReconciler) createRoute(ctx context.Context, routeTemplatePath string, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) *routev1.Route {
	routeHttpsConfig := RouteConfig{
		Orchestrator:  orchestrator,
		UseOAuthProxy: requiresOAuth(orchestrator),
	}
	var route *routev1.Route
	route, err := templateParser.ParseResource[routev1.Route](routeTemplatePath, routeHttpsConfig, reflect.TypeOf(&routev1.Route{}))

	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to parse route template")
	}
	controllerutil.SetControllerReference(orchestrator, route, r.Scheme)
	return route
}

func (r *GuardrailsOrchestratorReconciler) checkRouteReady(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator, portName string) (bool, error) {
	// Retry logic for getting the route and checking its readiness
	var existingRoute *routev1.Route
	err := retry.OnError(
		wait.Backoff{
			Duration: time.Second * 5,
		},
		func(err error) bool {
			// Retry on transient errors, such as network errors or resource not found
			return errors.IsNotFound(err) || err != nil
		},
		func() error {
			// Fetch the Route resource
			typedNamespaceName := types.NamespacedName{Name: orchestrator.Name + portName, Namespace: orchestrator.Namespace}
			existingRoute = &routev1.Route{}
			err := r.Get(ctx, typedNamespaceName, existingRoute)
			if err != nil {
				return err
			}

			for _, ingress := range existingRoute.Status.Ingress {
				for _, condition := range ingress.Conditions {
					if condition.Type == routev1.RouteAdmitted && condition.Status == "True" {
						return nil
					}
				}
			}
			// Route is not admitted yet, return an error to retry
			return fmt.Errorf("route %s is not admitted", orchestrator.Name)
		},
	)
	if err != nil {
		return false, err
	}
	return true, nil
}

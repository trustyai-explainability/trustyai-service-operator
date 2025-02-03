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
	// Retry logic for getting the route and checking its readiness
	var existingRoute *routev1.Route
	var err error
	err = retry.OnError(
		wait.Backoff{
			Duration: time.Second * 5,
			Factor:   2,
			Steps:    5,
		},
		func(err error) bool {
			// Retry on transient errors, such as network errors or resource not found
			return errors.IsNotFound(err) || err != nil
		},
		func() error {
			// Fetch the Route resource
			typedNamespaceName := types.NamespacedName{Name: orchestrator.Name + "-route", Namespace: orchestrator.Namespace}
			existingRoute = &routev1.Route{}
			err := r.Get(ctx, typedNamespaceName, existingRoute)
			if err != nil {
				return err
			}

			// Check if the Route is admitted
			for _, ingress := range existingRoute.Status.Ingress {
				for _, condition := range ingress.Conditions {
					if condition.Type == routev1.RouteAdmitted && condition.Status == "True" {
						// Route is admitted
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

	// If we reach here, the route is ready
	return true, nil
}

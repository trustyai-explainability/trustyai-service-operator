package utils

import (
	"context"
	"fmt"
	routev1 "github.com/openshift/api/route/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"reflect"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"time"
)

type RouteConfig struct {
	ServiceName string
	Name        *string
	Namespace   *string
	Owner       *metav1.Object
	PortName    string
	Termination *string
}

const (
	Edge        = "edge"
	Reencrypt   = "reencrypt"
	Passthrough = "passthrough"
)

// applyDefaultValues sets default values for the RouteConfig if not provided
func applyDefaultValues(owner metav1.Object, routeConfig RouteConfig) RouteConfig {

	if routeConfig.Name == nil {
		routeConfig.Name = StringPointer(owner.GetName())
	}

	if routeConfig.Namespace == nil {
		routeConfig.Namespace = StringPointer(owner.GetNamespace())
	}

	if routeConfig.Owner == nil {
		routeConfig.Owner = &owner
	}
	return routeConfig
}

// CreateRoute creates a route object, but does not deploy it to the cluster
func CreateRoute(ctx context.Context, c client.Client, owner metav1.Object, routeConfig RouteConfig, routeTemplatePath string, parser ResourceParserFunc[routev1.Route]) (*routev1.Route, error) {
	// Create route object
	var route *routev1.Route
	routeConfig = applyDefaultValues(owner, routeConfig)
	route, err := parser(routeTemplatePath, routeConfig, reflect.TypeOf(&routev1.Route{}))

	if err != nil {
		log.FromContext(ctx).Error(err, "failed to parse route template")
		return nil, err
	}
	log.FromContext(ctx).Info("route", "route", route.Namespace, "owner", owner.GetNamespace())
	err = controllerutil.SetControllerReference(owner, route, c.Scheme())
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to set controller reference")
		return nil, err
	}
	return route, nil
}

// CheckRouteReady verifies if a route is created and admitted
func CheckRouteReady(ctx context.Context, c client.Client, name string, namespace string) (bool, error) {
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
			typedNamespaceName := types.NamespacedName{Name: name, Namespace: namespace}
			existingRoute = &routev1.Route{}
			err := c.Get(ctx, typedNamespaceName, existingRoute)
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
			return fmt.Errorf("route %s is not admitted", name)
		},
	)
	if err != nil {
		return false, err
	}
	return true, nil
}

// ReconcileRoute contains logic for generic route reconciliation
func ReconcileRoute(ctx context.Context, c client.Client, owner metav1.Object, routeConfig RouteConfig, templatePath string, parserFunc ResourceParserFunc[routev1.Route]) (ctrl.Result, error) {
	existingRoute := &routev1.Route{}

	routeConfig = applyDefaultValues(owner, routeConfig)

	err := c.Get(ctx, types.NamespacedName{Name: *routeConfig.Name, Namespace: *routeConfig.Namespace}, existingRoute)
	if err != nil && errors.IsNotFound(err) {
		// Define a new route
		route, err := CreateRoute(ctx, c, owner, routeConfig, templatePath, parserFunc)
		if err != nil {
			log.FromContext(ctx).Error(err, "Failed to define route", "route", *routeConfig.Name, "namespace", *routeConfig.Namespace)
			return ctrl.Result{}, err
		}
		log.FromContext(ctx).Info("Creating a new Route", "Route.Namespace", route.Namespace, "Route.Name", route.Name)
		err = c.Create(ctx, route)
		if err != nil {
			log.FromContext(ctx).Error(err, "Failed to create new Route", "Route.Namespace", *routeConfig.Namespace, "Route.Name", routeConfig.Name)
			return ctrl.Result{}, err
		}
	} else if err != nil {
		log.FromContext(ctx).Error(err, "Failed to get Route")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

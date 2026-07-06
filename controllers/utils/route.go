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
	"sigs.k8s.io/controller-runtime/pkg/client"
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

const routeResourceKind = "route"

// === GENERIC FUNCTIONS ===============================================================================================

// DefineRoute creates a route object, but does not deploy it to the cluster
func DefineRoute(ctx context.Context, c client.Client, owner metav1.Object, routeConfig RouteConfig, routeTemplatePath string, parser ResourceParserFunc[*routev1.Route]) (*routev1.Route, error) {
	// Create route object
	genericConfig := processRouteConfig(owner, routeConfig)
	return DefineGeneric[*routev1.Route](ctx, c, owner, routeResourceKind, genericConfig, routeTemplatePath, parser)
}

// ReconcileRoute contains logic for generic route reconciliation
func ReconcileRoute(ctx context.Context, c client.Client, owner metav1.Object, routeConfig RouteConfig, routeTemplatePath string, parserFunc ResourceParserFunc[*routev1.Route]) error {
	genericConfig := processRouteConfig(owner, routeConfig)
	_, _, err := ReconcileGeneric[*routev1.Route](ctx, c, owner, routeResourceKind, genericConfig, routeTemplatePath, parserFunc)
	return err
}

// === SPECIFIC ROUTE FUNCTIONS ========================================================================================

// processConfig sets default values for the RouteConfig if not provided, then converts into a GenericConfig
func processRouteConfig(owner metav1.Object, routeConfig RouteConfig) GenericConfig {
	if routeConfig.Name == nil {
		routeConfig.Name = StringPointer(owner.GetName())
	}

	if routeConfig.Namespace == nil {
		routeConfig.Namespace = StringPointer(owner.GetNamespace())
	}

	if routeConfig.Owner == nil {
		routeConfig.Owner = &owner
	}

	return GetGenericConfig(routeConfig.Name, routeConfig.Namespace, routeConfig)
}

// CheckRouteReady verifies if a route is created and admitted
func CheckRouteReady(ctx context.Context, c client.Client, name string, namespace string) (bool, error) {
	// Retry logic for getting the route and checking its readiness
	var existingRoute *routev1.Route
	err := retry.OnError(
		wait.Backoff{
			Steps:    5,
			Duration: time.Second * 5,
			Factor:   1.0,
		},
		func(err error) bool {
			// Retry on transient errors, such as network errors or resource not found
			return errors.IsNotFound(err)
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

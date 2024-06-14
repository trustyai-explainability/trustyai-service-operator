package controllers

import (
	"context"
	routev1 "github.com/openshift/api/route/v1"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/templates"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"reflect"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	routeTemplatePath = "service/route.tmpl.yaml"
)

type RouteConfig struct {
	Name      string
	Namespace string
	PortName  string
}

func (r *TrustyAIServiceReconciler) createRouteObject(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) (*routev1.Route, error) {

	config := RouteConfig{
		Name:      instance.Name,
		Namespace: instance.Namespace,
		PortName:  OAuthServicePortName,
	}

	var route *routev1.Route
	route, err := templateParser.ParseResource[routev1.Route](routeTemplatePath, config, reflect.TypeOf(&routev1.Route{}))
	if err != nil {
		log.FromContext(ctx).Error(err, "Error parsing the route's template")
		return nil, err
	}
	if err := ctrl.SetControllerReference(instance, route, r.Scheme); err != nil {
		return nil, err
	}

	return route, nil
}

// Reconcile will manage the creation, update and deletion of the route returned
// by the newRoute function
func (r *TrustyAIServiceReconciler) reconcileRouteAuth(instance *trustyaiopendatahubiov1alpha1.TrustyAIService,
	ctx context.Context, newRoute func(context.Context, *trustyaiopendatahubiov1alpha1.TrustyAIService) (*routev1.Route, error)) error {

	// Generate the desired route
	desiredRoute, err := newRoute(ctx, instance)
	if err != nil {
		return err
	}

	// Create the route if it does not already exist
	foundRoute := &routev1.Route{}
	//justCreated := false
	err = r.Get(ctx, types.NamespacedName{
		Name:      desiredRoute.Name,
		Namespace: instance.Namespace,
	}, foundRoute)
	if err != nil {
		if errors.IsNotFound(err) {
			log.FromContext(ctx).Info("Creating Route")
			// Add .metatada.ownerReferences to the route to be deleted by the
			// Kubernetes garbage collector if the service is deleted
			err = ctrl.SetControllerReference(instance, desiredRoute, r.Scheme)
			if err != nil {
				log.FromContext(ctx).Error(err, "Unable to add OwnerReference to the Route")
				return err
			}
			// Create the route in the Openshift cluster
			err = r.Create(ctx, desiredRoute)
			if err != nil && !errors.IsAlreadyExists(err) {
				log.FromContext(ctx).Error(err, "Unable to create the Route")
				return err
			}
			//justCreated = true
		} else {
			log.FromContext(ctx).Error(err, "Unable to fetch the Route")
			return err
		}
	}

	return nil
}

// ReconcileRoute will manage the creation, update and deletion of the
// TLS route when the service is reconciled
func (r *TrustyAIServiceReconciler) ReconcileRoute(
	instance *trustyaiopendatahubiov1alpha1.TrustyAIService, ctx context.Context) error {
	return r.reconcileRouteAuth(instance, ctx, r.createRouteObject)
}

func (r *TrustyAIServiceReconciler) checkRouteReady(ctx context.Context, cr *trustyaiopendatahubiov1alpha1.TrustyAIService) (bool, error) {
	existingRoute := &routev1.Route{}

	err := r.Client.Get(ctx, types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}, existingRoute)
	if err != nil {
		log.FromContext(ctx).Info("Unable to find the Route")
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	for _, ingress := range existingRoute.Status.Ingress {
		for _, condition := range ingress.Conditions {
			if condition.Type == routev1.RouteAdmitted && condition.Status == corev1.ConditionTrue {
				// The Route is admitted, so it's ready
				return true, nil
			}
		}
	}

	return false, nil
}

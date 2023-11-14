package controllers

import (
	"context"
	routev1 "github.com/openshift/api/route/v1"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	"reflect"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *TrustyAIServiceReconciler) createRouteObjectNoAuth(cr *trustyaiopendatahubiov1alpha1.TrustyAIService) (*routev1.Route, error) {
	labels := getCommonLabels(cr.Name)

	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name,
			Namespace: cr.Namespace,
			Labels:    labels,
		},
		Spec: routev1.RouteSpec{
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: cr.Name,
			},
			Port: &routev1.RoutePort{
				TargetPort: intstr.IntOrString{
					Type:   intstr.String,
					StrVal: "http",
				},
			},
			TLS: &routev1.TLSConfig{
				Termination:                   routev1.TLSTerminationReencrypt,
				InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
			},
		},
	}

	if err := controllerutil.SetControllerReference(cr, route, r.Scheme); err != nil {
		return nil, err
	}

	return route, nil
}

func (r *TrustyAIServiceReconciler) reconcileRoute(cr *trustyaiopendatahubiov1alpha1.TrustyAIService, ctx context.Context) error {
	createdRoute, err := r.createRouteObjectNoAuth(cr)
	if err != nil {
		log.FromContext(ctx).Error(err, "Error creating Route object.")
		return err
	}

	existingRoute := &routev1.Route{}
	err = r.Client.Get(ctx, types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}, existingRoute)
	if err != nil {
		if errors.IsNotFound(err) {
			// Route does not exist, create it.
			err := r.Client.Create(ctx, createdRoute)
			if err != nil {
				log.FromContext(ctx).Error(err, "Error creating Route.")
				return err
			}
		} else {
			// Error occurred during Get
			log.FromContext(ctx).Error(err, "Error getting Route.")
			return err
		}
	} else {
		// Route exists, check if it is the same as the one we want to create.
		if reflect.DeepEqual(existingRoute.Spec, createdRoute.Spec) {
			// They are the same, so no action needed.
			return nil
		} else {
			// They are different, update the existing route.
			existingRoute.Spec = createdRoute.Spec
			err := r.Client.Update(ctx, existingRoute)
			if err != nil {
				log.FromContext(ctx).Error(err, "Error updating Route.")
				return err
			}
		}
	}

	return nil
}

func (r *TrustyAIServiceReconciler) createRouteObject(instance *trustyaiopendatahubiov1alpha1.TrustyAIService) *routev1.Route {
	return &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"trustyai-service-name": instance.Name,
			},
		},
		Spec: routev1.RouteSpec{
			To: routev1.RouteTargetReference{
				Kind:   "Service",
				Name:   instance.Name + "-tls",
				Weight: pointer.Int32Ptr(100),
			},
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromString(OAuthServicePortName),
			},
			TLS: &routev1.TLSConfig{
				Termination: routev1.TLSTerminationPassthrough,
			},
		},
		Status: routev1.RouteStatus{
			Ingress: []routev1.RouteIngress{},
		},
	}
}

// Reconcile will manage the creation, update and deletion of the route returned
// by the newRoute function
func (r *TrustyAIServiceReconciler) reconcileRouteAuth(instance *trustyaiopendatahubiov1alpha1.TrustyAIService,
	ctx context.Context, newRoute func(*trustyaiopendatahubiov1alpha1.TrustyAIService) *routev1.Route) error {

	// Generate the desired route
	desiredRoute := newRoute(instance)

	// Create the route if it does not already exist
	foundRoute := &routev1.Route{}
	//justCreated := false
	err := r.Get(ctx, types.NamespacedName{
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

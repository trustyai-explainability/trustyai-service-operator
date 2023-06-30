package controllers

import (
	"context"
	routev1 "github.com/openshift/api/route/v1"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *TrustyAIServiceReconciler) createRouteObject(cr *trustyaiopendatahubiov1alpha1.TrustyAIService) (*routev1.Route, error) {
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
				Termination:                   routev1.TLSTerminationEdge,
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
	createdRoute, err := r.createRouteObject(cr)
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

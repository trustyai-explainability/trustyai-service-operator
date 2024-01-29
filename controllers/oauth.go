package controllers

import (
	"context"
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
	tlsServiceTemplatePath = "service/service-tls.tmpl.yaml"
)

type OAuthConfig struct {
	ProxyImage string
}

type ServiceTLSConfig struct {
	Instance *trustyaiopendatahubiov1alpha1.TrustyAIService
}

// generateTrustyAIOAuthService defines the desired OAuth service object
func generateTrustyAIOAuthService(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) (*corev1.Service, error) {

	serviceTLSConfig := ServiceTLSConfig{
		Instance: instance,
	}

	var serviceTLS *corev1.Service
	serviceTLS, err := templateParser.ParseResource[corev1.Service](tlsServiceTemplatePath, serviceTLSConfig, reflect.TypeOf(&corev1.Service{}))
	if err != nil {
		log.FromContext(ctx).Error(err, "Error parsing the service's deployment template")
		return nil, err
	}

	return serviceTLS, nil
}

// reconcileOAuthService will manage the OAuth service reconciliation required
// by the service's OAuth proxy
func (r *TrustyAIServiceReconciler) reconcileOAuthService(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) error {

	// Generate the desired OAuth service object
	desiredService, err := generateTrustyAIOAuthService(ctx, instance)
	if err != nil {
		// Error creating the oauth service resource object
		return err
	}

	// Create the OAuth service if it does not already exist
	foundService := &corev1.Service{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      desiredService.GetName(),
		Namespace: instance.GetNamespace(),
	}, foundService)
	if err != nil {
		if errors.IsNotFound(err) {
			log.FromContext(ctx).Info("Creating OAuth Service")
			// Add .metatada.ownerReferences to the OAuth service to be deleted by
			// the Kubernetes garbage collector if the service is deleted
			err = ctrl.SetControllerReference(instance, desiredService, r.Scheme)
			if err != nil {
				log.FromContext(ctx).Error(err, "Unable to add OwnerReference to the OAuth Service")
				return err
			}
			// Create the OAuth service in the Openshift cluster
			err = r.Create(ctx, desiredService)
			if err != nil && !errors.IsAlreadyExists(err) {
				log.FromContext(ctx).Error(err, "Unable to create the OAuth Service")
				return err
			}
		} else {
			log.FromContext(ctx).Error(err, "Unable to fetch the OAuth Service")
			return err
		}
	}

	return nil
}

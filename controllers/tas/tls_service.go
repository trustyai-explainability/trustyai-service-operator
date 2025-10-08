package tas

import (
	"context"
	"reflect"

	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/tas/templates"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
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
	Instance                 *trustyaiopendatahubiov1alpha1.TrustyAIService
	CustomCertificatesBundle CustomCertificatesBundle
	Version                  string
}

// generateTrustyAITLSService defines the desired TLS service object
func generateTrustyAITLSService(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService, caBundle CustomCertificatesBundle) (*corev1.Service, error) {

	serviceTLSConfig := ServiceTLSConfig{
		Instance:                 instance,
		CustomCertificatesBundle: caBundle,
		Version:                  constants.Version,
	}

	var serviceTLS *corev1.Service
	serviceTLS, err := templateParser.ParseResource[corev1.Service](tlsServiceTemplatePath, serviceTLSConfig, reflect.TypeOf(&corev1.Service{}))
	if err != nil {
		log.FromContext(ctx).Error(err, "Error parsing the service's deployment template")
		return nil, err
	}

	return serviceTLS, nil
}

// reconcileTLSService will manage the TLS service reconciliation required
// by the service's kube-rbac-proxy
func (r *TrustyAIServiceReconciler) reconcileTLSService(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService, caBundle CustomCertificatesBundle) error {

	// Generate the desired TLS service object
	desiredService, err := generateTrustyAITLSService(ctx, instance, caBundle)
	if err != nil {
		// Error creating the TLS service resource object
		return err
	}

	// Create the TLS service if it does not already exist
	foundService := &corev1.Service{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      desiredService.GetName(),
		Namespace: instance.GetNamespace(),
	}, foundService)
	if err != nil {
		if errors.IsNotFound(err) {
			log.FromContext(ctx).Info("Creating TLS Service")
			// Add .metadata.ownerReferences to the TLS service to be deleted by
			// the Kubernetes garbage collector if the service is deleted
			err = ctrl.SetControllerReference(instance, desiredService, r.Scheme)
			if err != nil {
				log.FromContext(ctx).Error(err, "Unable to add OwnerReference to the TLS Service")
				return err
			}
			// Create the TLS service in the OpenShift cluster
			err = r.Create(ctx, desiredService)
			if err != nil && !errors.IsAlreadyExists(err) {
				log.FromContext(ctx).Error(err, "Unable to create the TLS Service")
				return err
			}
		} else {
			log.FromContext(ctx).Error(err, "Unable to fetch the TLS Service")
			return err
		}
	}

	return nil
}

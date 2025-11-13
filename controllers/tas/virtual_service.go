package tas

import (
	"context"
	"fmt"
	"reflect"

	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1alpha1"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/tas/templates"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	virtualServiceTemplatePath = "service/virtual-service.tmpl.yaml"
	virtualServiceCDRName      = "destinationrules.networking.istio.io"
)

// DestinationRuleConfig has the variables for the DestinationRule template
type VirtualServiceConfig struct {
	Name               string
	Namespace          string
	VirtualServiceName string
}

// isVirtualServiceCRDPresent returns true if the DestinationRule CRD is present, false otherwise
func (r *TrustyAIServiceReconciler) isVirtualServiceCRDPresent(ctx context.Context) (bool, error) {
	crd := &apiextensionsv1.CustomResourceDefinition{}

	err := r.Get(ctx, types.NamespacedName{Name: virtualServiceCDRName}, crd)
	if err != nil {
		if !errors.IsNotFound(err) {
			return false, fmt.Errorf("error getting "+virtualServiceCDRName+" CRD: %v", err)
		}
		// Not found
		return false, nil
	}

	// Found
	return true, nil
}

func (r *TrustyAIServiceReconciler) ensureVirtualService(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) error {

	virtualServiceName := instance.Name + "-redirect"

	existingVirtualService := &unstructured.Unstructured{}
	existingVirtualService.SetKind("VirtualService")
	existingVirtualService.SetAPIVersion("networking.istio.io/v1beta1")

	// Check if the DestinationRule already exists
	err := r.Get(ctx, types.NamespacedName{Name: virtualServiceName, Namespace: instance.Namespace}, existingVirtualService)
	if err == nil {
		// DestinationRule exists
		return nil
	}

	if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check for existing VirtualService: %v", err)
	}

	virtualServiceConfig := VirtualServiceConfig{
		Name:               instance.Name,
		Namespace:          instance.Namespace,
		VirtualServiceName: virtualServiceName,
	}

	var virtualService *unstructured.Unstructured
	virtualService, err = templateParser.ParseResource[*unstructured.Unstructured](virtualServiceTemplatePath, virtualServiceConfig, reflect.TypeOf(&unstructured.Unstructured{}))
	if err != nil {
		log.FromContext(ctx).Error(err, "could not parse the VirtualService template")
		return err
	}

	if err := ctrl.SetControllerReference(instance, virtualService, r.Scheme); err != nil {
		return err
	}

	err = r.Create(ctx, virtualService)
	if err != nil {
		return fmt.Errorf("failed to create VirtualService: %v", err)
	}

	return nil
}

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
	destinationRuleTemplatePath = "service/destination-rule.tmpl.yaml"
	destinationRuleCDRName      = "destinationrules.networking.istio.io"
)

// DestinationRuleConfig has the variables for the DestinationRule template
type DestinationRuleConfig struct {
	Name                string
	Namespace           string
	DestinationRuleName string
}

// isDestinationRuleCRDPresent returns true if the DestinationRule CRD is present, false otherwise
func (r *TrustyAIServiceReconciler) isDestinationRuleCRDPresent(ctx context.Context) (bool, error) {
	crd := &apiextensionsv1.CustomResourceDefinition{}

	err := r.Get(ctx, types.NamespacedName{Name: destinationRuleCDRName}, crd)
	if err != nil {
		if !errors.IsNotFound(err) {
			return false, fmt.Errorf("error getting "+destinationRuleCDRName+" CRD: %v", err)
		}
		// Not found
		return false, nil
	}

	// Found
	return true, nil
}

func (r *TrustyAIServiceReconciler) ensureDestinationRule(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) error {

	destinationRuleName := instance.Name + "-internal"

	existingDestinationRule := &unstructured.Unstructured{}
	existingDestinationRule.SetKind("DestinationRule")
	existingDestinationRule.SetAPIVersion("networking.istio.io/v1beta1")

	// Check if the DestinationRule already exists
	err := r.Get(ctx, types.NamespacedName{Name: destinationRuleName, Namespace: instance.Namespace}, existingDestinationRule)
	if err == nil {
		// DestinationRule exists
		return nil
	}

	if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check for existing DestinationRule: %v", err)
	}

	destinationRuleConfig := DestinationRuleConfig{
		Name:                instance.Name,
		Namespace:           instance.Namespace,
		DestinationRuleName: destinationRuleName,
	}

	var destinationRule *unstructured.Unstructured
	destinationRule, err = templateParser.ParseResource[*unstructured.Unstructured](destinationRuleTemplatePath, destinationRuleConfig, reflect.TypeOf(&unstructured.Unstructured{}))
	if err != nil {
		log.FromContext(ctx).Error(err, "could not parse the DestinationRule template")
		return err
	}

	if err := ctrl.SetControllerReference(instance, destinationRule, r.Scheme); err != nil {
		return err
	}

	err = r.Create(ctx, destinationRule)
	if err != nil {
		return fmt.Errorf("failed to create DestinationRule: %v", err)
	}

	return nil
}

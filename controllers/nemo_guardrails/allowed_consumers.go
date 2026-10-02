package nemo_guardrails

import (
	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// validateAllowedConsumers checks spec.allowedConsumers.namespaces.selector using the same
// label key/value validation Kubernetes applies to a LabelSelector.
// Returns an error if the selector is invalid.
func validateAllowedConsumers(ac *nemoguardrailsv1alpha1.AllowedConsumers) field.ErrorList {
	if ac == nil || ac.Namespaces == nil || ac.Namespaces.Selector == nil {
		return nil
	}
	return metav1validation.ValidateLabelSelector(
		ac.Namespaces.Selector,
		metav1validation.LabelSelectorValidationOptions{},
		field.NewPath("spec", "allowedConsumers", "namespaces", "selector"),
	)
}

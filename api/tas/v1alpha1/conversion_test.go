package v1alpha1

import (
	"testing"

	v1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestConvertFromNormalizesLegacyV1Conditions(t *testing.T) {
	src := &v1.TrustyAIService{}
	src.Status.Conditions = []v1.TASCondition{{
		Type:   "Ready",
		Status: metav1.ConditionFalse,
	}}
	dst := &TrustyAIService{}

	if err := dst.ConvertFrom(src); err != nil {
		t.Fatalf("ConvertFrom() returned error: %v", err)
	}

	if len(dst.Status.Conditions) != 1 {
		t.Fatalf("ConvertFrom() produced %d conditions, want 1", len(dst.Status.Conditions))
	}
	condition := dst.Status.Conditions[0]
	if condition.LastTransitionTime.IsZero() {
		t.Error("ConvertFrom() left lastTransitionTime unset")
	}
	if condition.Reason != "ConditionNotSet" {
		t.Errorf("ConvertFrom() reason = %q, want %q", condition.Reason, "ConditionNotSet")
	}
	if condition.Message != "Condition has not been evaluated" {
		t.Errorf("ConvertFrom() message = %q, want %q", condition.Message, "Condition has not been evaluated")
	}
}

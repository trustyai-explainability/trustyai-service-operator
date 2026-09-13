package trustyaimodule

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestOperandHealthCheckerAggregatesInstancesAcrossNamespaces(t *testing.T) {
	objects := make([]client.Object, 0, 5)
	for _, namespace := range []string{"team-a", "team-b", "team-c", "team-d", "team-e"} {
		objects = append(objects, testOperand("TrustyAIService", "v1", namespace, "tas", "True", ""))
	}

	checker := NewOperandHealthChecker("TAS", fake.NewClientBuilder().WithObjects(objects...).Build())
	result := checker.Check(context.Background())
	if !result.Healthy || result.Degraded {
		t.Fatalf("expected all five TAS instances to be healthy, got %#v", result)
	}
}

func TestOperandHealthCheckerReportsFailedInstance(t *testing.T) {
	objects := []client.Object{
		testOperand("EvalHub", "v1", "team-a", "evalhub-a", "True", ""),
		testOperand("EvalHub", "v1", "team-b", "evalhub-b", "False", "deployment unavailable"),
	}

	checker := NewOperandHealthChecker("EVALHUB", fake.NewClientBuilder().WithObjects(objects...).Build())
	result := checker.Check(context.Background())
	if result.Healthy || !result.Degraded {
		t.Fatalf("expected failed EvalHub instance to degrade service, got %#v", result)
	}
	if result.Reason != "team-b/evalhub-b: deployment unavailable" {
		t.Fatalf("expected failed instance in reason, got %q", result.Reason)
	}
}

func TestOperandHealthCheckerTreatsNoInstancesAsWaiting(t *testing.T) {
	checker := NewOperandHealthChecker("NEMO_GUARDRAILS", fake.NewClientBuilder().Build())
	result := checker.Check(context.Background())
	if result.Healthy || result.Degraded || result.Reason != "no operand instances found" {
		t.Fatalf("expected no instances to be waiting, got %#v", result)
	}
}

func testOperand(kind, version, namespace, name, ready, message string) *unstructured.Unstructured {
	operand := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "trustyai.opendatahub.io/" + version,
		"kind":       kind,
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": namespace,
		},
		"status": map[string]interface{}{
			"conditions": []interface{}{map[string]interface{}{
				"type":    "Ready",
				"status":  ready,
				"message": message,
			}},
		},
	}}
	operand.SetGroupVersionKind(operand.GroupVersionKind())
	return operand
}

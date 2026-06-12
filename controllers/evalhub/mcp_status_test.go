package evalhub

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	evalhubv1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("MCP status helpers", func() {
	It("records reconcile errors on status.mcp without touching top-level Ready", func() {
		enabled := true
		evalHub := &evalhubv1.EvalHub{
			ObjectMeta: metav1.ObjectMeta{Name: "eh", Namespace: "ns", Generation: 2},
			Spec: evalhubv1.EvalHubSpec{
				MCP:      &evalhubv1.EvalHubMCPSpec{Enabled: &enabled},
				Replicas: func() *int32 { r := int32(1); return &r }(),
			},
			Status: evalhubv1.EvalHubStatus{
				Ready: corev1.ConditionTrue,
				Phase: "Ready",
			},
		}

		setMCPReconcileError(evalHub, "MCPConfigMapFailed", "boom")

		Expect(evalHub.Status.Ready).To(Equal(corev1.ConditionTrue))
		Expect(evalHub.Status.Phase).To(Equal("Ready"))
		Expect(evalHub.Status.MCP).NotTo(BeNil())
		Expect(evalHub.Status.MCP.Phase).To(Equal("Error"))
		Expect(evalHub.Status.MCP.Ready).To(BeFalse())

		cond := meta.FindStatusCondition(evalHub.Status.MCP.Conditions, mcpConditionReconciled)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal("MCPConfigMapFailed"))
		Expect(cond.Message).To(Equal("boom"))
	})

})

func TestSetMCPRouteSuccess(t *testing.T) {
	evalHub := &evalhubv1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{Name: "eh", Namespace: "ns", Generation: 3},
		Status: evalhubv1.EvalHubStatus{
			MCP: &evalhubv1.EvalHubMCPStatus{
				Conditions: []metav1.Condition{{
					Type:   mcpConditionRouteReady,
					Status: metav1.ConditionFalse,
					Reason: "MCPRouteFailed",
				}},
			},
		},
	}

	setMCPRouteSuccess(evalHub)

	cond := meta.FindStatusCondition(evalHub.Status.MCP.Conditions, mcpConditionRouteReady)
	if cond == nil {
		t.Fatal("expected RouteReady condition")
	}
	if cond.Status != metav1.ConditionTrue {
		t.Fatalf("status: got %s, want True", cond.Status)
	}
	if cond.Reason != "MCPRouteReady" {
		t.Fatalf("reason: got %q, want MCPRouteReady", cond.Reason)
	}
	if cond.Message != "Route reconciliation succeeded" {
		t.Fatalf("message: got %q", cond.Message)
	}
	if cond.ObservedGeneration != 3 {
		t.Fatalf("observedGeneration: got %d, want 3", cond.ObservedGeneration)
	}
}

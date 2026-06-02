package evalhub

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/meta"
)

var _ = Describe("MCP status helpers", func() {
	It("records reconcile errors on status.mcp without touching top-level Ready", func() {
		enabled := true
		evalHub := &evalhubv1alpha1.EvalHub{
			ObjectMeta: metav1.ObjectMeta{Name: "eh", Namespace: "ns", Generation: 2},
			Spec: evalhubv1alpha1.EvalHubSpec{
				MCP:     &evalhubv1alpha1.EvalHubMCPSpec{Enabled: &enabled},
				Replicas: func() *int32 { r := int32(1); return &r }(),
			},
			Status: evalhubv1alpha1.EvalHubStatus{
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

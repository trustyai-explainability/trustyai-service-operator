package evalhub

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

var _ = Describe("GenerateMCPConfigData", func() {
	var r *EvalHubReconciler

	BeforeEach(func() {
		r = &EvalHubReconciler{}
	})

	It("defaults MCP client and EvalHub backend transport to http", func() {
		evalHub := &evalhubv1alpha1.EvalHub{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-evalhub",
				Namespace: "test-namespace",
			},
			Spec: evalhubv1alpha1.EvalHubSpec{},
		}
		data, err := r.generateMCPConfigData(evalHub)
		Expect(err).NotTo(HaveOccurred())
		Expect(data).To(HaveKey(mcpConfigFileName))
		var cfg MCPConfig
		Expect(yaml.Unmarshal([]byte(data[mcpConfigFileName]), &cfg)).To(Succeed())
		Expect(cfg.Transport).To(Equal("http"))
		Expect(cfg.EvalHub.Transport).To(Equal("http"))
	})

	It("uses explicit transport values from MCP spec", func() {
		evalHub := &evalhubv1alpha1.EvalHub{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-evalhub",
				Namespace: "test-namespace",
			},
			Spec: evalhubv1alpha1.EvalHubSpec{
				MCP: &evalhubv1alpha1.EvalHubMCPSpec{
					Transport:        "http-sse",
					EvalHubTransport: "http-sse",
				},
			},
		}
		data, err := r.generateMCPConfigData(evalHub)
		Expect(err).NotTo(HaveOccurred())
		Expect(data).To(HaveKey(mcpConfigFileName))
		var cfg MCPConfig
		Expect(yaml.Unmarshal([]byte(data[mcpConfigFileName]), &cfg)).To(Succeed())
		Expect(cfg.Transport).To(Equal("http-sse"))
		Expect(cfg.EvalHub.Transport).To(Equal("http-sse"))
	})
})

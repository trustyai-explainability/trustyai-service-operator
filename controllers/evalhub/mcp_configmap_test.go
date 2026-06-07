package evalhub

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"
)

var _ = Describe("generateMCPConfigData", func() {
	var (
		reconciler *EvalHubReconciler
		evalHub    *evalhubv1alpha1.EvalHub
	)

	BeforeEach(func() {
		reconciler = &EvalHubReconciler{}
	})

	Context("with default MCP settings", func() {
		BeforeEach(func() {
			evalHub = &evalhubv1alpha1.EvalHub{
				ObjectMeta: metav1.ObjectMeta{Name: "test-evalhub", Namespace: "team-a"},
				Spec:       evalhubv1alpha1.EvalHubSpec{},
			}
		})

		It("should generate config.yaml and auth.yaml with expected defaults", func() {
			data, err := reconciler.generateMCPConfigData(evalHub)
			Expect(err).NotTo(HaveOccurred())

			var cfg MCPConfig
			Expect(yaml.Unmarshal([]byte(data[mcpConfigFileName]), &cfg)).To(Succeed())
			Expect(cfg.Transport).To(Equal("http"))
			Expect(cfg.BaseURL).To(Equal("https://test-evalhub.team-a.svc.cluster.local:8443"))
			Expect(cfg.Host).To(Equal("127.0.0.1"))
			Expect(cfg.Port).To(Equal(mcpAppPort))

			Expect(data).To(HaveKey(evalHubAuthConfigMapKey))
			auth := data[evalHubAuthConfigMapKey]
			Expect(auth).To(ContainSubstring("evalhubs"))
			Expect(auth).To(ContainSubstring("methods: [get]"))
			Expect(auth).To(ContainSubstring("methods: [post]"))
		})
	})

	Context("with explicit MCP transport", func() {
		BeforeEach(func() {
			enabled := true
			evalHub = &evalhubv1alpha1.EvalHub{
				ObjectMeta: metav1.ObjectMeta{Name: "eh", Namespace: "ns"},
				Spec: evalhubv1alpha1.EvalHubSpec{
					MCP: &evalhubv1alpha1.EvalHubMCPSpec{
						Enabled:   &enabled,
						Transport: "http-sse",
					},
				},
			}
		})

		It("should set transport from spec.mcp.transport", func() {
			data, err := reconciler.generateMCPConfigData(evalHub)
			Expect(err).NotTo(HaveOccurred())

			var cfg MCPConfig
			Expect(yaml.Unmarshal([]byte(data[mcpConfigFileName]), &cfg)).To(Succeed())
			Expect(cfg.Transport).To(Equal("http-sse"))
		})
	})
})

var _ = Describe("mcpConfigMapMatchesDesired", func() {
	It("returns true when data, labels, and controller owner match", func() {
		enabled := true
		evalHub := &evalhubv1alpha1.EvalHub{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "eh",
				Namespace: "team-a",
				UID:       "uid-1",
			},
			Spec: evalhubv1alpha1.EvalHubSpec{
				MCP: &evalhubv1alpha1.EvalHubMCPSpec{Enabled: &enabled},
			},
		}
		data, err := (&EvalHubReconciler{}).generateMCPConfigData(evalHub)
		Expect(err).NotTo(HaveOccurred())

		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "eh-mcp-config",
				Namespace: "team-a",
				Labels:    mcpLabels(evalHub),
			},
			Data: data,
		}
		Expect(controllerutil.SetControllerReference(evalHub, cm, scheme.Scheme)).To(Succeed())

		Expect(mcpConfigMapMatchesDesired(cm, data, mcpLabels(evalHub), evalHub)).To(BeTrue())
	})

	It("returns false when data differs", func() {
		evalHub := &evalhubv1alpha1.EvalHub{
			ObjectMeta: metav1.ObjectMeta{Name: "eh", Namespace: "team-a", UID: "uid-1"},
		}
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "eh-mcp-config",
				Namespace: "team-a",
				Labels:    mcpLabels(evalHub),
			},
			Data: map[string]string{"config.yaml": "old"},
		}
		Expect(controllerutil.SetControllerReference(evalHub, cm, scheme.Scheme)).To(Succeed())

		Expect(mcpConfigMapMatchesDesired(cm, map[string]string{"config.yaml": "new"}, mcpLabels(evalHub), evalHub)).To(BeFalse())
	})
})

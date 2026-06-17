package nemo_guardrails

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("MCP Gateway", func() {
	const (
		ns          = "test-ns"
		gatewayName = "test-mcp-gateway"
	)

	var (
		ctx        context.Context
		reconciler *NemoGuardrailsReconciler
	)

	// buildFakeClient constructs a scheme + fake client pre-loaded with an
	// MCPGatewayExtension pointing at gatewayName and the Gateway itself.
	buildFakeClient := func(gwName, gwNamespace string) client.Client {
		s := runtime.NewScheme()
		_ = nemoguardrailsv1alpha1.AddToScheme(s)

		mcpGV := schema.GroupVersion{Group: "mcp.kuadrant.io", Version: "v1alpha1"}
		s.AddKnownTypeWithName(mcpGV.WithKind("MCPGatewayExtension"), &unstructured.Unstructured{})
		s.AddKnownTypeWithName(mcpGV.WithKind("MCPGatewayExtensionList"), &unstructured.UnstructuredList{})

		gwGV := schema.GroupVersion{Group: "gateway.networking.k8s.io", Version: "v1"}
		s.AddKnownTypeWithName(gwGV.WithKind("Gateway"), &unstructured.Unstructured{})
		s.AddKnownTypeWithName(gwGV.WithKind("GatewayList"), &unstructured.UnstructuredList{})

		mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{mcpGV, gwGV})
		mapper.Add(mcpGV.WithKind("MCPGatewayExtension"), meta.RESTScopeNamespace)
		mapper.Add(gwGV.WithKind("Gateway"), meta.RESTScopeNamespace)

		ext := &unstructured.Unstructured{Object: map[string]interface{}{
			"kind":       "MCPGatewayExtension",
			"apiVersion": "mcp.kuadrant.io/v1alpha1",
			"metadata":   map[string]interface{}{"name": "test-ext", "namespace": ns},
			"spec": map[string]interface{}{
				"targetRef": map[string]interface{}{
					"name":      gwName,
					"namespace": gwNamespace,
				},
			},
		}}
		gw := &unstructured.Unstructured{Object: map[string]interface{}{
			"kind":       "Gateway",
			"apiVersion": "gateway.networking.k8s.io/v1",
			"metadata":   map[string]interface{}{"name": gwName, "namespace": gwNamespace},
		}}

		return fake.NewClientBuilder().
			WithScheme(s).
			WithRESTMapper(mapper).
			WithObjects(ext, gw).
			Build()
	}

	BeforeEach(func() {
		ctx = context.Background()
		fakeClient := buildFakeClient(gatewayName, ns)
		reconciler = &NemoGuardrailsReconciler{
			Client:    fakeClient,
			Scheme:    fakeClient.Scheme(),
			Namespace: ns,
		}
	})

	Describe("isMCPGatewayPresent", func() {
		Context("when the Gateway exists", func() {
			It("returns true with no error", func() {
				found, err := reconciler.isMCPGatewayPresent(ctx, ns, gatewayName)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
			})
		})

		Context("when the Gateway does not exist", func() {
			It("returns false with no error", func() {
				found, err := reconciler.isMCPGatewayPresent(ctx, ns, "does-not-exist")
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeFalse())
			})
		})
	})

	Describe("discoverMCPGateway", func() {
		Context("with no name override (auto-discovery)", func() {
			It("finds the gateway via the MCPGatewayExtension", func() {
				ref, status := reconciler.discoverMCPGateway(ctx, ns, "")
				Expect(status.MCPGatewayFound).To(BeTrue())
				Expect(ref).NotTo(BeNil())
				Expect(ref.Name).To(Equal(gatewayName))
				Expect(ref.Namespace).To(Equal(ns))
			})
		})

		Context("when the name override matches no extension targetRef", func() {
			It("returns nil ref and MCPGatewayFound=false", func() {
				override := "does-not-exist"
				ref, status := reconciler.discoverMCPGateway(ctx, ns, override)
				Expect(ref).To(BeNil())
				Expect(status.MCPGatewayFound).To(BeFalse())
				Expect(status.MCPGatewayError).To(Equal(
					fmt.Sprintf("MCP gateway not found: %s", override),
				))
			})
		})
	})

	// Ensure the unused NemoGuardrails struct literal still compiles correctly
	// with the nested Template.Pod.MCPGateway path.
	It("accepts NemoGuardrailsSpec with nested MCPGateway config", func() {
		ng := &nemoguardrailsv1alpha1.NemoGuardrails{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: ns},
			Spec: nemoguardrailsv1alpha1.NemoGuardrailsSpec{
				Template: &nemoguardrailsv1alpha1.NemoGuardrailsTemplate{
					Pod: &nemoguardrailsv1alpha1.NemoGuardrailsPodTemplate{
						MCPGateway: &nemoguardrailsv1alpha1.MCPGatewayConfig{
							Name:      gatewayName,
							Namespace: ns,
						},
					},
				},
			},
		}
		Expect(ng.Spec.Template.Pod.MCPGateway.Name).To(Equal(gatewayName))
	})
})

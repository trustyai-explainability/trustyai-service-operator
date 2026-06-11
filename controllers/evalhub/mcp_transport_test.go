package evalhub

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	evalhubv1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
)

// Runs under the EvalHub Controller Suite envtest lifecycle in suite_test.go (BeforeSuite/AfterSuite).

var _ = Describe("MCP transport helpers", func() {
	Describe("mcpClientTransport", func() {
		It("defaults to http when spec is nil", func() {
			Expect(mcpClientTransport(nil)).To(Equal("http"))
		})

		It("returns spec.transport when set", func() {
			spec := &evalhubv1.EvalHubMCPSpec{Transport: "http-sse"}
			Expect(mcpClientTransport(spec)).To(Equal("http-sse"))
		})
	})

	Describe("mcpTransportEnv", func() {
		It("defaults to http when spec is nil", func() {
			Expect(mcpTransportEnv(nil)).To(Equal("http"))
		})

		It("uses evalHubTransport when set", func() {
			spec := &evalhubv1.EvalHubMCPSpec{Transport: "http", EvalHubTransport: "http-sse"}
			Expect(mcpTransportEnv(spec)).To(Equal("http-sse"))
		})

		It("falls back to transport when evalHubTransport is unset", func() {
			spec := &evalhubv1.EvalHubMCPSpec{Transport: "http-sse"}
			Expect(mcpTransportEnv(spec)).To(Equal("http-sse"))
		})
	})
})

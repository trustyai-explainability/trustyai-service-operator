package evalhub

import evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"

const (
	defaultMCPClientTransport      = "http"
	defaultEvalHubBackendTransport = "http"
)

func mcpClientTransport(mcp *evalhubv1alpha1.EvalHubMCPSpec) string {
	if mcp != nil && mcp.Transport != "" {
		return mcp.Transport
	}
	return defaultMCPClientTransport
}

func evalHubBackendTransport(mcp *evalhubv1alpha1.EvalHubMCPSpec) string {
	if mcp != nil && mcp.EvalHubTransport != "" {
		return mcp.EvalHubTransport
	}
	return defaultEvalHubBackendTransport
}

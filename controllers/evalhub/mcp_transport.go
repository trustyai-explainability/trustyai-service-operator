package evalhub

import evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"

const (
	defaultMCPClientTransport = "http"
)

func mcpClientTransport(mcp *evalhubv1alpha1.EvalHubMCPSpec) string {
	if mcp != nil && mcp.Transport != "" {
		return mcp.Transport
	}
	return defaultMCPClientTransport
}

// mcpTransportEnv returns the EVALHUB_TRANSPORT value (MCP server transport mode).
// evalHubTransport overrides transport when set.
func mcpTransportEnv(mcp *evalhubv1alpha1.EvalHubMCPSpec) string {
	if mcp != nil && mcp.EvalHubTransport != "" {
		return mcp.EvalHubTransport
	}
	return mcpClientTransport(mcp)
}

package evalhub

import (
	"testing"

	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
)

func TestMCPClientTransport(t *testing.T) {
	if got := mcpClientTransport(nil); got != "http" {
		t.Fatalf("expected http default, got %q", got)
	}
	spec := &evalhubv1alpha1.EvalHubMCPSpec{Transport: "http-sse"}
	if got := mcpClientTransport(spec); got != "http-sse" {
		t.Fatalf("expected http-sse, got %q", got)
	}
}

func TestEvalHubBackendTransport(t *testing.T) {
	if got := evalHubBackendTransport(nil); got != "http" {
		t.Fatalf("expected http default, got %q", got)
	}
	spec := &evalhubv1alpha1.EvalHubMCPSpec{EvalHubTransport: "http-sse"}
	if got := evalHubBackendTransport(spec); got != "http-sse" {
		t.Fatalf("expected http-sse, got %q", got)
	}
}

package evalhub

import (
	"testing"

	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

func TestGenerateMCPConfigData(t *testing.T) {
	r := &EvalHubReconciler{}
	evalHub := &evalhubv1alpha1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{Name: "test-evalhub", Namespace: "team-a"},
		Spec:       evalhubv1alpha1.EvalHubSpec{},
	}

	data, err := r.generateMCPConfigData(evalHub)
	if err != nil {
		t.Fatal(err)
	}
	var cfg MCPConfig
	if err := yaml.Unmarshal([]byte(data[mcpConfigFileName]), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Transport != "http" {
		t.Fatalf("transport: got %q want http", cfg.Transport)
	}
	if cfg.BaseURL != "https://test-evalhub.team-a.svc.cluster.local:8443" {
		t.Fatalf("base_url: got %q", cfg.BaseURL)
	}
	if cfg.Port != mcpContainerPort {
		t.Fatalf("port: got %d want %d", cfg.Port, mcpContainerPort)
	}
}

func TestGenerateMCPConfigData_explicitTransport(t *testing.T) {
	r := &EvalHubReconciler{}
	enabled := true
	evalHub := &evalhubv1alpha1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{Name: "eh", Namespace: "ns"},
		Spec: evalhubv1alpha1.EvalHubSpec{
			MCP: &evalhubv1alpha1.EvalHubMCPSpec{
				Enabled:   &enabled,
				Transport: "http-sse",
			},
		},
	}
	data, err := r.generateMCPConfigData(evalHub)
	if err != nil {
		t.Fatal(err)
	}
	var cfg MCPConfig
	if err := yaml.Unmarshal([]byte(data[mcpConfigFileName]), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Transport != "http-sse" {
		t.Fatalf("transport: got %q want http-sse", cfg.Transport)
	}
}

package main

import (
	"testing"

	"github.com/trustyai-explainability/trustyai-service-operator/controllers"
)

func TestEnabledServicesSetIgnoresRetiredServices(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected controllers.EnabledServices
	}{
		{
			name:     "GORCH alone is silently dropped",
			input:    "GORCH",
			expected: controllers.EnabledServices{},
		},
		{
			name:     "GORCH mixed with valid services is dropped",
			input:    "TAS,GORCH",
			expected: controllers.EnabledServices{"TAS"},
		},
		{
			name:     "LMES alone enables nothing",
			input:    "LMES",
			expected: controllers.EnabledServices{},
		},
		{
			name:     "LMES mixed with valid services is dropped",
			input:    "TAS,LMES",
			expected: controllers.EnabledServices{"TAS"},
		},
		{
			name:     "a service list still carrying LMES starts the remaining services",
			input:    "TAS,LMES,EVALHUB,NEMO_GUARDRAILS",
			expected: controllers.EnabledServices{"TAS", "EVALHUB", "NEMO_GUARDRAILS"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var es controllers.EnabledServices
			if err := es.Set(tt.input); err != nil {
				t.Fatalf("Set(%q) returned error: %v", tt.input, err)
			}
			if len(es) != len(tt.expected) {
				t.Fatalf("got %v, want %v", es, tt.expected)
			}
			for i := range es {
				if es[i] != tt.expected[i] {
					t.Fatalf("got %v, want %v", es, tt.expected)
				}
			}
		})
	}
}

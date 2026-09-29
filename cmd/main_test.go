package main

import (
	"testing"

	"github.com/trustyai-explainability/trustyai-service-operator/controllers"
)

func TestFilterLegacyServices(t *testing.T) {
	tests := []struct {
		name     string
		input    controllers.EnabledServices
		expected controllers.EnabledServices
	}{
		{
			name:     "GORCH is removed",
			input:    controllers.EnabledServices{"TAS", "GORCH", "LMES"},
			expected: controllers.EnabledServices{"TAS", "LMES"},
		},
		{
			name:     "GORCH only",
			input:    controllers.EnabledServices{"GORCH"},
			expected: controllers.EnabledServices{},
		},
		{
			name:     "no GORCH is unchanged",
			input:    controllers.EnabledServices{"TAS", "LMES"},
			expected: controllers.EnabledServices{"TAS", "LMES"},
		},
		{
			name:     "empty input",
			input:    controllers.EnabledServices{},
			expected: controllers.EnabledServices{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterLegacyServices(tt.input)
			if len(result) != len(tt.expected) {
				t.Fatalf("got %v, want %v", result, tt.expected)
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Fatalf("got %v, want %v", result, tt.expected)
				}
			}
		})
	}
}

package v1alpha1

import (
	"testing"
)

func TestStorageSpecGetSize(t *testing.T) {
	tests := []struct {
		name     string
		storage  StorageSpec
		expected string
	}{
		{
			name: "PVC with specified size",
			storage: StorageSpec{
				Format: "PVC",
				Size:   "10Gi",
			},
			expected: "10Gi",
		},
		{
			name: "PVC with missing size",
			storage: StorageSpec{
				Format: "PVC",
			},
			expected: DefaultPVCSize,
		},
		{
			name: "Database mode with specified size (should be ignored)",
			storage: StorageSpec{
				Format: "DATABASE",
				Size:   "10Gi",
			},
			expected: "",
		},
		{
			name: "Database mode with no size specified",
			storage: StorageSpec{
				Format: "DATABASE",
			},
			expected: "",
		},
		{
			name: "Migration case with size (should still be ignored)",
			storage: StorageSpec{
				Format: "DATABASE",
				Folder: "/data",
				Size:   "5Gi",
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.storage.GetSize()
			if result != tt.expected {
				t.Errorf("GetSize() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

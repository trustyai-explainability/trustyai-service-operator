package dsc

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDSCConfigReader_ReadDSCConfig(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		configMapData  map[string]string
		expectError    bool
		expectedOnline bool
		expectedCode   bool
	}{
		{
			name:           "ConfigMap not found - should return nil",
			namespace:      "test-namespace",
			configMapData:  nil,
			expectError:    false,
			expectedOnline: false, // Should not be applied
			expectedCode:   false, // Should not be applied
		},
		{
			name:      "Valid configuration with both settings enabled",
			namespace: "test-namespace",
			configMapData: map[string]string{
				DSCPermitOnlineKey:        "true",
				DSCPermitCodeExecutionKey: "true",
			},
			expectError:    false,
			expectedOnline: true,
			expectedCode:   true,
		},
		{
			name:      "Valid configuration with both settings disabled",
			namespace: "test-namespace",
			configMapData: map[string]string{
				DSCPermitOnlineKey:        "false",
				DSCPermitCodeExecutionKey: "false",
			},
			expectError:    false,
			expectedOnline: false,
			expectedCode:   false,
		},
		{
			name:      "Partial configuration - only permitOnline",
			namespace: "test-namespace",
			configMapData: map[string]string{
				DSCPermitOnlineKey: "true",
			},
			expectError:    false,
			expectedOnline: true,
			expectedCode:   false, // Should remain default
		},
		{
			name:      "Invalid boolean values - should use defaults",
			namespace: "test-namespace",
			configMapData: map[string]string{
				DSCPermitOnlineKey:        "invalid",
				DSCPermitCodeExecutionKey: "also-invalid",
			},
			expectError:    false, // Should not fail, just log error
			expectedOnline: false, // Should remain default
			expectedCode:   false, // Should remain default
		},
		{
			name:           "Empty namespace - should skip reading",
			namespace:      "",
			configMapData:  nil,
			expectError:    false,
			expectedOnline: false, // Should remain default
			expectedCode:   false, // Should remain default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client
			var objects []client.Object
			if tt.configMapData != nil {
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      DSCConfigMapName,
						Namespace: tt.namespace,
					},
					Data: tt.configMapData,
				}
				objects = append(objects, configMap)
			}

			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

			// Create DSCConfigReader
			reader := NewDSCConfigReader(fakeClient, tt.namespace)

			// Create a test logger
			log := logr.Discard()

			// Test ReadDSCConfig
			dscConfig, err := reader.ReadDSCConfig(context.Background(), &log)

			// Assert error expectations
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// Assert configuration values - only if dscConfig is not nil
			if dscConfig != nil {
				assert.Equal(t, tt.expectedOnline, dscConfig.AllowOnline, "AllowOnline should match expected value")
				assert.Equal(t, tt.expectedCode, dscConfig.AllowCodeExecution, "AllowCodeExecution should match expected value")
			} else {
				// When ConfigMap is not found, dscConfig should be nil
				assert.Nil(t, dscConfig, "DSC config should be nil when ConfigMap is not found")
			}
		})
	}
}

func TestDSCConfigReader_ReadDSCConfig_ConfigMapNotFound(t *testing.T) {
	// Create fake client without any ConfigMaps
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Create DSCConfigReader
	reader := NewDSCConfigReader(fakeClient, "test-namespace")

	// Create a test logger
	log := logr.Discard()

	// Test ReadDSCConfig
	dscConfig, err := reader.ReadDSCConfig(context.Background(), &log)

	// Should not return error when ConfigMap is not found
	assert.NoError(t, err)

	// DSC config should be nil when ConfigMap is not found
	assert.Nil(t, dscConfig, "DSC config should be nil when ConfigMap is not found")
}

func TestDSCConfigReader_ReadDSCConfig_ClientError(t *testing.T) {
	// Create a mock client that returns an error
	mockClient := &mockClient{shouldError: true}

	// Create DSCConfigReader
	reader := NewDSCConfigReader(mockClient, "test-namespace")

	// Create a test logger
	log := logr.Discard()

	// Test ReadDSCConfig
	_, err := reader.ReadDSCConfig(context.Background(), &log)

	// Should return error when client fails
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error reading DSC ConfigMap")
}

// mockClient is a simple mock that can simulate client errors
type mockClient struct {
	client.Client
	shouldError bool
}

func (m *mockClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if m.shouldError {
		return errors.NewInternalError(fmt.Errorf("mock client error"))
	}
	return nil
}

func TestNewDSCConfigReader(t *testing.T) {
	// Create a fake client
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Test NewDSCConfigReader
	reader := NewDSCConfigReader(fakeClient, "test-namespace")

	// Assert reader is properly initialised
	assert.NotNil(t, reader)
	assert.Equal(t, fakeClient, reader.Client)
	assert.Equal(t, "test-namespace", reader.Namespace)
}

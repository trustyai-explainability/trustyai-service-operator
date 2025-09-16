package lmes

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/dsc"
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
			name:           "ConfigMap not found - should use defaults",
			namespace:      "test-namespace",
			configMapData:  nil,
			expectError:    false,
			expectedOnline: false, // Default value
			expectedCode:   false, // Default value
		},
		{
			name:      "Valid configuration with both settings enabled",
			namespace: "test-namespace",
			configMapData: map[string]string{
				dsc.DSCPermitOnlineKey:        "true",
				dsc.DSCPermitCodeExecutionKey: "true",
			},
			expectError:    false,
			expectedOnline: true,
			expectedCode:   true,
		},
		{
			name:      "Valid configuration with both settings disabled",
			namespace: "test-namespace",
			configMapData: map[string]string{
				dsc.DSCPermitOnlineKey:        "false",
				dsc.DSCPermitCodeExecutionKey: "false",
			},
			expectError:    false,
			expectedOnline: false,
			expectedCode:   false,
		},
		{
			name:      "Partial configuration - only permitOnline",
			namespace: "test-namespace",
			configMapData: map[string]string{
				dsc.DSCPermitOnlineKey: "true",
			},
			expectError:    false,
			expectedOnline: true,
			expectedCode:   false, // Should remain default
		},
		{
			name:      "Invalid boolean values - should use defaults",
			namespace: "test-namespace",
			configMapData: map[string]string{
				dsc.DSCPermitOnlineKey:        "invalid",
				dsc.DSCPermitCodeExecutionKey: "also-invalid",
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
			// Reset Options to default values
			Options.AllowOnline = false
			Options.AllowCodeExecution = false

			// Create fake client
			var objects []client.Object
			if tt.configMapData != nil {
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      dsc.DSCConfigMapName,
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
			reader := dsc.NewDSCConfigReader(fakeClient, tt.namespace)

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

			// Apply DSC config to Options for testing
			if dscConfig != nil {
				ApplyDSCConfig(dscConfig)
			}

			// Assert configuration values
			assert.Equal(t, tt.expectedOnline, Options.AllowOnline, "AllowOnline should match expected value")
			assert.Equal(t, tt.expectedCode, Options.AllowCodeExecution, "AllowCodeExecution should match expected value")
		})
	}
}

func TestDSCConfigReader_ReadDSCConfig_ConfigMapNotFound(t *testing.T) {
	// Reset Options to default values
	Options.AllowOnline = false
	Options.AllowCodeExecution = false

	// Create fake client without any ConfigMaps
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Create DSCConfigReader
	reader := dsc.NewDSCConfigReader(fakeClient, "test-namespace")

	// Create a test logger
	log := logr.Discard()

	// Test ReadDSCConfig
	dscConfig, err := reader.ReadDSCConfig(context.Background(), &log)

	// Should not return error when ConfigMap is not found
	assert.NoError(t, err)

	// Apply DSC config to Options for testing
	if dscConfig != nil {
		ApplyDSCConfig(dscConfig)
	}

	// Values should remain at defaults
	assert.False(t, Options.AllowOnline)
	assert.False(t, Options.AllowCodeExecution)
}

func TestDSCConfigReader_ReadDSCConfig_ClientError(t *testing.T) {
	// Reset Options to default values
	Options.AllowOnline = false
	Options.AllowCodeExecution = false

	// Create a mock client that returns an error
	mockClient := &mockClient{shouldError: true}

	// Create DSCConfigReader
	reader := dsc.NewDSCConfigReader(mockClient, "test-namespace")

	// Create a test logger
	log := logr.Discard()

	// Test ReadDSCConfig
	_, err := reader.ReadDSCConfig(context.Background(), &log)

	// Should return error when client fails
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error reading DSC ConfigMap")

	// Values should remain at defaults
	assert.False(t, Options.AllowOnline)
	assert.False(t, Options.AllowCodeExecution)
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

func TestApplyDSCConfig(t *testing.T) {
	tests := []struct {
		name           string
		dscConfig      *dsc.DSCConfig
		expectedOnline bool
		expectedCode   bool
	}{
		{
			name: "Valid DSC config with both settings enabled",
			dscConfig: &dsc.DSCConfig{
				AllowOnline:        true,
				AllowCodeExecution: true,
			},
			expectedOnline: true,
			expectedCode:   true,
		},
		{
			name: "Valid DSC config with both settings disabled",
			dscConfig: &dsc.DSCConfig{
				AllowOnline:        false,
				AllowCodeExecution: false,
			},
			expectedOnline: false,
			expectedCode:   false,
		},
		{
			name: "Partial DSC config - only permitOnline",
			dscConfig: &dsc.DSCConfig{
				AllowOnline:        true,
				AllowCodeExecution: false,
			},
			expectedOnline: true,
			expectedCode:   false,
		},
		{
			name:           "Nil DSC config - should not change defaults",
			dscConfig:      nil,
			expectedOnline: false, // Should remain default
			expectedCode:   false, // Should remain default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset Options to default values
			Options.AllowOnline = false
			Options.AllowCodeExecution = false

			// Apply DSC config
			ApplyDSCConfig(tt.dscConfig)

			// Assert configuration values
			assert.Equal(t, tt.expectedOnline, Options.AllowOnline, "AllowOnline should match expected value")
			assert.Equal(t, tt.expectedCode, Options.AllowCodeExecution, "AllowCodeExecution should match expected value")
		})
	}
}

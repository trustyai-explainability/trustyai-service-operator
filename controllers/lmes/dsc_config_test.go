package lmes

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	lmesv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/lmes/v1alpha1"
)

func TestGetDSCLMEvalSettings(t *testing.T) {
	tests := []struct {
		name                     string
		configMap                *corev1.ConfigMap
		expectAllowOnline        *bool
		expectAllowCodeExecution *bool
		expectError              bool
	}{
		{
			name: "DSC ConfigMap with both settings enabled",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DSCConfigMapName,
					Namespace: "test-namespace",
					Annotations: map[string]string{
						"opendatahub.io/config-source": "datasciencecluster",
					},
				},
				Data: map[string]string{
					DSCAllowOnlineKey:        "true",
					DSCAllowCodeExecutionKey: "true",
				},
			},
			expectAllowOnline:        boolPtr(true),
			expectAllowCodeExecution: boolPtr(true),
			expectError:              false,
		},
		{
			name: "DSC ConfigMap with both settings disabled",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DSCConfigMapName,
					Namespace: "test-namespace",
					Annotations: map[string]string{
						"opendatahub.io/config-source": "datasciencecluster",
					},
				},
				Data: map[string]string{
					DSCAllowOnlineKey:        "false",
					DSCAllowCodeExecutionKey: "false",
				},
			},
			expectAllowOnline:        boolPtr(false),
			expectAllowCodeExecution: boolPtr(false),
			expectError:              false,
		},
		{
			name: "DSC ConfigMap with only online setting",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DSCConfigMapName,
					Namespace: "test-namespace",
					Annotations: map[string]string{
						"opendatahub.io/config-source": "datasciencecluster",
					},
				},
				Data: map[string]string{
					DSCAllowOnlineKey: "true",
				},
			},
			expectAllowOnline:        boolPtr(true),
			expectAllowCodeExecution: nil,
			expectError:              false,
		},
		{
			name: "DSC ConfigMap with only code execution setting",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DSCConfigMapName,
					Namespace: "test-namespace",
					Annotations: map[string]string{
						"opendatahub.io/config-source": "datasciencecluster",
					},
				},
				Data: map[string]string{
					DSCAllowCodeExecutionKey: "true",
				},
			},
			expectAllowOnline:        nil,
			expectAllowCodeExecution: boolPtr(true),
			expectError:              false,
		},
		{
			name: "ConfigMap without DSC annotation",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DSCConfigMapName,
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					DSCAllowOnlineKey:        "true",
					DSCAllowCodeExecutionKey: "true",
				},
			},
			expectAllowOnline:        nil,
			expectAllowCodeExecution: nil,
			expectError:              false,
		},
		{
			name: "ConfigMap with invalid boolean values",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DSCConfigMapName,
					Namespace: "test-namespace",
					Annotations: map[string]string{
						"opendatahub.io/config-source": "datasciencecluster",
					},
				},
				Data: map[string]string{
					DSCAllowOnlineKey:        "invalid",
					DSCAllowCodeExecutionKey: "also-invalid",
				},
			},
			expectAllowOnline:        nil,
			expectAllowCodeExecution: nil,
			expectError:              false, // Invalid values are logged but don't cause errors
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)

			var client client.Client
			if tt.configMap != nil {
				client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.configMap).Build()
			} else {
				client = fake.NewClientBuilder().WithScheme(scheme).Build()
			}

			reconciler := &LMEvalJobReconciler{
				Client:    client,
				Namespace: "test-namespace",
			}

			allowOnline, allowCodeExecution, err := reconciler.getDSCLMEvalSettings(context.Background(), logr.Discard())

			// Check error
			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			// Check allowOnline
			if tt.expectAllowOnline == nil && allowOnline != nil {
				t.Errorf("Expected allowOnline to be nil, got %v", *allowOnline)
			}
			if tt.expectAllowOnline != nil && allowOnline == nil {
				t.Errorf("Expected allowOnline to be %v, got nil", *tt.expectAllowOnline)
			}
			if tt.expectAllowOnline != nil && allowOnline != nil && *tt.expectAllowOnline != *allowOnline {
				t.Errorf("Expected allowOnline to be %v, got %v", *tt.expectAllowOnline, *allowOnline)
			}

			// Check allowCodeExecution
			if tt.expectAllowCodeExecution == nil && allowCodeExecution != nil {
				t.Errorf("Expected allowCodeExecution to be nil, got %v", *allowCodeExecution)
			}
			if tt.expectAllowCodeExecution != nil && allowCodeExecution == nil {
				t.Errorf("Expected allowCodeExecution to be %v, got nil", *tt.expectAllowCodeExecution)
			}
			if tt.expectAllowCodeExecution != nil && allowCodeExecution != nil && *tt.expectAllowCodeExecution != *allowCodeExecution {
				t.Errorf("Expected allowCodeExecution to be %v, got %v", *tt.expectAllowCodeExecution, *allowCodeExecution)
			}
		})
	}
}

func TestCreatePodWithDSCSettings(t *testing.T) {
	tests := []struct {
		name                  string
		dscConfigMap          *corev1.ConfigMap
		jobAllowOnline        *bool
		jobAllowCodeExecution *bool
		operatorAllowOnline   bool
		operatorAllowCodeExec bool
		expectOnlineMode      bool
		expectCodeExecution   bool
	}{
		{
			name: "DSC allows both, job wants both",
			dscConfigMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DSCConfigMapName,
					Namespace: "test-namespace",
					Annotations: map[string]string{
						"opendatahub.io/config-source": "datasciencecluster",
					},
				},
				Data: map[string]string{
					DSCAllowOnlineKey:        "true",
					DSCAllowCodeExecutionKey: "true",
				},
			},
			jobAllowOnline:        boolPtr(true),
			jobAllowCodeExecution: boolPtr(true),
			operatorAllowOnline:   false,
			operatorAllowCodeExec: false,
			expectOnlineMode:      true,
			expectCodeExecution:   true,
		},
		{
			name: "DSC disallows both, job wants both",
			dscConfigMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DSCConfigMapName,
					Namespace: "test-namespace",
					Annotations: map[string]string{
						"opendatahub.io/config-source": "datasciencecluster",
					},
				},
				Data: map[string]string{
					DSCAllowOnlineKey:        "false",
					DSCAllowCodeExecutionKey: "false",
				},
			},
			jobAllowOnline:        boolPtr(true),
			jobAllowCodeExecution: boolPtr(true),
			operatorAllowOnline:   true,
			operatorAllowCodeExec: true,
			expectOnlineMode:      false,
			expectCodeExecution:   false,
		},
		{
			name:                  "No DSC ConfigMap, operator allows both, job wants both",
			dscConfigMap:          nil,
			jobAllowOnline:        boolPtr(true),
			jobAllowCodeExecution: boolPtr(true),
			operatorAllowOnline:   true,
			operatorAllowCodeExec: true,
			expectOnlineMode:      true,
			expectCodeExecution:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)

			var client client.Client
			if tt.dscConfigMap != nil {
				client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.dscConfigMap).Build()
			} else {
				client = fake.NewClientBuilder().WithScheme(scheme).Build()
			}

			reconciler := &LMEvalJobReconciler{
				Client:    client,
				Namespace: "test-namespace",
			}

			// Create service options
			svcOpts := &serviceOptions{
				AllowOnline:        tt.operatorAllowOnline,
				AllowCodeExecution: tt.operatorAllowCodeExec,
			}

			// Create job
			job := &lmesv1alpha1.LMEvalJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-job",
					Namespace: "test-namespace",
				},
				Spec: lmesv1alpha1.LMEvalJobSpec{
					Model: "test-model",
					TaskList: lmesv1alpha1.TaskList{
						TaskNames: []string{"test-task"},
					},
					AllowOnline:        tt.jobAllowOnline,
					AllowCodeExecution: tt.jobAllowCodeExecution,
				},
			}

			// Create pod
			pod := CreatePod(reconciler, svcOpts, job, logr.Discard())

			// Check environment variables
			codeExecutionEnabled := false

			for _, env := range pod.Spec.Containers[0].Env {
				switch env.Name {
				case "TRUST_REMOTE_CODE", "HF_DATASETS_TRUST_REMOTE_CODE", "UNITXT_ALLOW_UNVERIFIED_CODE":
					if env.Value == "1" || env.Value == "True" {
						codeExecutionEnabled = true
					}
				}
			}

			// Check command line flags
			hasAllowOnlineFlag := false
			for _, cmd := range pod.Spec.Containers[0].Command {
				if cmd == "--allow-online" {
					hasAllowOnlineFlag = true
					break
				}
			}

			// Verify expectations
			if tt.expectOnlineMode != hasAllowOnlineFlag {
				t.Errorf("Expected online mode flag to be %v, got %v", tt.expectOnlineMode, hasAllowOnlineFlag)
			}

			if tt.expectCodeExecution != codeExecutionEnabled {
				t.Errorf("Expected code execution to be %v, got %v", tt.expectCodeExecution, codeExecutionEnabled)
			}
		})
	}
}

func boolPtr(b bool) *bool {
	return &b
}

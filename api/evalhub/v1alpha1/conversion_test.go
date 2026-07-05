package v1alpha1

import (
	"testing"

	"github.com/trustyai-explainability/trustyai-service-operator/api/common"
	v1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func boolPtr(b bool) *bool    { return &b }
func int32Ptr(i int32) *int32 { return &i }

func fullEvalHubV1Alpha1() *EvalHub {
	now := metav1.Now()
	return &EvalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-evalhub",
			Namespace: "test-ns",
		},
		Spec: EvalHubSpec{
			Replicas:    int32Ptr(3),
			Env:         []corev1.EnvVar{{Name: "KEY", Value: "val"}},
			Providers:   []string{"garak", "lm-evaluation-harness"},
			Collections: []string{"safety-and-fairness-v1"},
			Database: &DatabaseSpec{
				Type:         "postgresql",
				Secret:       "db-secret",
				MaxOpenConns: 50,
				MaxIdleConns: 10,
			},
			Otel: &OTELSpec{
				ExporterType:     "otlp-grpc",
				ExporterEndpoint: "otel-collector:4317",
				ExporterInsecure: true,
				SamplingRatio:    "0.5",
				EnableTracing:    true,
				EnableMetrics:    true,
				EnableLogs:       false,
			},
			MCP: &EvalHubMCPSpec{
				Enabled:          boolPtr(true),
				Replicas:         int32Ptr(2),
				Transport:        "http",
				EvalHubTransport: "http-sse",
				Env:              []corev1.EnvVar{{Name: "MCP_KEY", Value: "mcp_val"}},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("100m"),
					},
				},
				Image:      "quay.io/test/mcp:latest",
				AuthSecret: "mcp-auth",
			},
		},
		Status: EvalHubStatus{
			Phase:             "Ready",
			Replicas:          3,
			ReadyReplicas:     3,
			Ready:             corev1.ConditionTrue,
			URL:               "https://evalhub.test.svc:8443",
			ActiveProviders:   []string{"garak"},
			ActiveCollections: []string{"safety-and-fairness-v1"},
			LastUpdateTime:    &now,
			Conditions: []common.Condition{
				{
					Type:               "Available",
					Status:             corev1.ConditionTrue,
					Reason:             "Ready",
					Message:            "All replicas ready",
					LastTransitionTime: now,
				},
			},
			MCP: &EvalHubMCPStatus{
				Phase: "Ready",
				Ready: true,
				URL:   "https://mcp.test.svc:8443",
				Conditions: []metav1.Condition{
					{
						Type:               "Available",
						Status:             metav1.ConditionTrue,
						Reason:             "Ready",
						Message:            "MCP ready",
						LastTransitionTime: now,
					},
				},
			},
		},
	}
}

func TestConvertTo(t *testing.T) {
	src := fullEvalHubV1Alpha1()
	dst := &v1.EvalHub{}

	if err := src.ConvertTo(dst); err != nil {
		t.Fatalf("ConvertTo failed: %v", err)
	}

	if dst.Name != "test-evalhub" {
		t.Errorf("Name = %q, want %q", dst.Name, "test-evalhub")
	}
	if *dst.Spec.Replicas != 3 {
		t.Errorf("Replicas = %d, want 3", *dst.Spec.Replicas)
	}
	if dst.Spec.Database == nil || dst.Spec.Database.Type != "postgresql" {
		t.Error("Database not converted")
	}
	if dst.Spec.Otel == nil || dst.Spec.Otel.ExporterEndpoint != "otel-collector:4317" {
		t.Error("Otel not converted")
	}
	if dst.Spec.MCP == nil || !*dst.Spec.MCP.Enabled {
		t.Error("MCP not converted")
	}
	if dst.Status.Phase != "Ready" {
		t.Errorf("Status.Phase = %q, want %q", dst.Status.Phase, "Ready")
	}
	if len(dst.Status.Conditions) != 1 {
		t.Errorf("Conditions len = %d, want 1", len(dst.Status.Conditions))
	}
	if dst.Status.MCP == nil || !dst.Status.MCP.Ready {
		t.Error("MCP status not converted")
	}
}

func TestConvertFrom(t *testing.T) {
	src := fullEvalHubV1Alpha1()
	hub := &v1.EvalHub{}

	if err := src.ConvertTo(hub); err != nil {
		t.Fatalf("ConvertTo failed: %v", err)
	}

	roundTripped := &EvalHub{}
	if err := roundTripped.ConvertFrom(hub); err != nil {
		t.Fatalf("ConvertFrom failed: %v", err)
	}

	if roundTripped.Name != src.Name {
		t.Errorf("Name = %q, want %q", roundTripped.Name, src.Name)
	}
	if *roundTripped.Spec.Replicas != *src.Spec.Replicas {
		t.Errorf("Replicas = %d, want %d", *roundTripped.Spec.Replicas, *src.Spec.Replicas)
	}
	if roundTripped.Spec.Database.Secret != src.Spec.Database.Secret {
		t.Errorf("Database.Secret = %q, want %q", roundTripped.Spec.Database.Secret, src.Spec.Database.Secret)
	}
	if roundTripped.Spec.Otel.SamplingRatio != src.Spec.Otel.SamplingRatio {
		t.Errorf("Otel.SamplingRatio = %q, want %q", roundTripped.Spec.Otel.SamplingRatio, src.Spec.Otel.SamplingRatio)
	}
	if roundTripped.Spec.MCP.AuthSecret != src.Spec.MCP.AuthSecret {
		t.Errorf("MCP.AuthSecret = %q, want %q", roundTripped.Spec.MCP.AuthSecret, src.Spec.MCP.AuthSecret)
	}
	if roundTripped.Status.URL != src.Status.URL {
		t.Errorf("Status.URL = %q, want %q", roundTripped.Status.URL, src.Status.URL)
	}
}

func TestConvertNilPointerFields(t *testing.T) {
	src := &EvalHub{
		ObjectMeta: metav1.ObjectMeta{Name: "minimal"},
		Spec: EvalHubSpec{
			Replicas: int32Ptr(1),
		},
	}
	dst := &v1.EvalHub{}

	if err := src.ConvertTo(dst); err != nil {
		t.Fatalf("ConvertTo failed: %v", err)
	}
	if dst.Spec.Database != nil {
		t.Error("Database should be nil")
	}
	if dst.Spec.Otel != nil {
		t.Error("Otel should be nil")
	}
	if dst.Spec.MCP != nil {
		t.Error("MCP should be nil")
	}
	if dst.Status.MCP != nil {
		t.Error("Status.MCP should be nil")
	}
	if dst.Status.LastUpdateTime != nil {
		t.Error("LastUpdateTime should be nil")
	}

	roundTripped := &EvalHub{}
	if err := roundTripped.ConvertFrom(dst); err != nil {
		t.Fatalf("ConvertFrom failed: %v", err)
	}
	if roundTripped.Spec.Database != nil {
		t.Error("Round-tripped Database should be nil")
	}
}

func TestConvertToPreservesV1OnlyOTELFields(t *testing.T) {
	hub := &v1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{Name: "test-evalhub"},
		Spec: v1.EvalHubSpec{
			Replicas: int32Ptr(1),
			Otel: &v1.OTELSpec{
				ExporterType:               "otlp-grpc",
				ExporterEndpoint:           "otel-collector:4317",
				TracerTimeout:              "30s",
				TracerBatchInterval:        "5s",
				EnableJobContainerLogs:     true,
				ServiceName:                "evalhub-test",
				EnableEcsResourceDetection: true,
				DisableRedirectOtelLogs:    true,
				DisableDatabaseOtelScans:   true,
				MetricExportInterval:       "60s",
			},
		},
	}

	spoke := &EvalHub{
		Spec: EvalHubSpec{
			Replicas: int32Ptr(1),
			Otel: &OTELSpec{
				ExporterType:     "otlp-http",
				ExporterEndpoint: "new-endpoint:4318",
				EnableTracing:    true,
			},
		},
	}

	if err := spoke.ConvertTo(hub); err != nil {
		t.Fatalf("ConvertTo failed: %v", err)
	}

	if hub.Spec.Otel.ExporterEndpoint != "new-endpoint:4318" {
		t.Errorf("ExporterEndpoint = %q, want %q", hub.Spec.Otel.ExporterEndpoint, "new-endpoint:4318")
	}
	if hub.Spec.Otel.TracerTimeout != "30s" {
		t.Errorf("TracerTimeout = %q, want %q", hub.Spec.Otel.TracerTimeout, "30s")
	}
	if hub.Spec.Otel.ServiceName != "evalhub-test" {
		t.Errorf("ServiceName = %q, want %q", hub.Spec.Otel.ServiceName, "evalhub-test")
	}
	if hub.Spec.Otel.MetricExportInterval != "60s" {
		t.Errorf("MetricExportInterval = %q, want %q", hub.Spec.Otel.MetricExportInterval, "60s")
	}
	if !hub.Spec.Otel.EnableJobContainerLogs {
		t.Error("EnableJobContainerLogs was cleared")
	}
}

func TestConvertFromDropsV1OnlyOTELFields(t *testing.T) {
	hub := &v1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{Name: "test-evalhub"},
		Spec: v1.EvalHubSpec{
			Replicas: int32Ptr(1),
			Otel: &v1.OTELSpec{
				ExporterType:         "otlp-grpc",
				ExporterEndpoint:     "otel-collector:4317",
				TracerTimeout:        "30s",
				ServiceName:          "evalhub-test",
				MetricExportInterval: "60s",
			},
		},
	}

	dst := &EvalHub{}
	if err := dst.ConvertFrom(hub); err != nil {
		t.Fatalf("ConvertFrom failed: %v", err)
	}

	if dst.Spec.Otel == nil {
		t.Fatal("Otel should be converted")
	}
	if dst.Spec.Otel.ExporterEndpoint != "otel-collector:4317" {
		t.Errorf("ExporterEndpoint = %q, want %q", dst.Spec.Otel.ExporterEndpoint, "otel-collector:4317")
	}
}

func TestConvertDeepCopyIsolation(t *testing.T) {
	src := fullEvalHubV1Alpha1()
	dst := &v1.EvalHub{}

	if err := src.ConvertTo(dst); err != nil {
		t.Fatalf("ConvertTo failed: %v", err)
	}

	// Mutate the source after conversion
	src.Spec.Database.Secret = "MUTATED"
	src.Spec.Otel.ExporterEndpoint = "MUTATED"
	src.Spec.MCP.Image = "MUTATED"

	if dst.Spec.Database.Secret == "MUTATED" {
		t.Error("Database was not deep-copied — mutation leaked")
	}
	if dst.Spec.Otel.ExporterEndpoint == "MUTATED" {
		t.Error("Otel was not deep-copied — mutation leaked")
	}
	if dst.Spec.MCP.Image == "MUTATED" {
		t.Error("MCP was not deep-copied — mutation leaked")
	}
}

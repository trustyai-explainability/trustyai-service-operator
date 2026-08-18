package tracing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestSetupWithoutEndpointUsesNoopProvider(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "")
	t.Setenv("OTEL_METRICS_PROMETHEUS_DISABLED", "true")
	t.Setenv("OTEL_SDK_DISABLED", "")

	shutdown, err := Setup(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	_, span := Tracer("test").Start(context.Background(), "noop-span")
	span.End()
	assert.False(t, span.SpanContext().IsValid())

	counter, err := Meter("test").Int64Counter("noop.counter")
	require.NoError(t, err)
	counter.Add(context.Background(), 1)
}

func TestPrometheusMetricsEnabledByDefault(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "")
	t.Setenv("OTEL_METRICS_PROMETHEUS_DISABLED", "")
	t.Setenv("OTEL_SDK_DISABLED", "")
	assert.True(t, prometheusMetricsEnabled())
	assert.True(t, metricsEnabled())
}

func TestPrometheusMetricsDisabledEnv(t *testing.T) {
	t.Setenv("OTEL_METRICS_PROMETHEUS_DISABLED", "true")
	assert.False(t, prometheusMetricsEnabled())
}

func TestReconcileOutcome(t *testing.T) {
	assert.Equal(t, "success", ReconcileOutcome(false, 0, nil))
	assert.Equal(t, "requeue", ReconcileOutcome(true, 0, nil))
	assert.Equal(t, "requeue", ReconcileOutcome(false, time.Second, nil))
	assert.Equal(t, "error", ReconcileOutcome(false, 0, errors.New("boom")))
}

func TestOtlpMetricsProtocolSpecificWins(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_PROTOCOL", "http/protobuf")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
	assert.Equal(t, "http/protobuf", otlpMetricsProtocol())
}

func TestOtlpTraceProtocolSpecificWins(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "http/protobuf")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
	assert.Equal(t, "http/protobuf", otlpTraceProtocol())
}

func TestResolveOTLPProtocol(t *testing.T) {
	tests := []struct {
		name    string
		protocol string
		want    otlpExporterKind
		wantErr bool
	}{
		{name: "empty defaults to grpc", protocol: "", want: otlpExporterGRPC},
		{name: "grpc", protocol: "grpc", want: otlpExporterGRPC},
		{name: "grpc trimmed and lowercased", protocol: "  GRPC  ", want: otlpExporterGRPC},
		{name: "http alias", protocol: "http", want: otlpExporterHTTP},
		{name: "http/protobuf", protocol: "http/protobuf", want: otlpExporterHTTP},
		{name: "http/json rejected", protocol: "http/json", wantErr: true},
		{name: "invalid protocol", protocol: "ftp", wantErr: true},
		{name: "unknown http variant", protocol: "http/xml", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveOTLPProtocol(tt.protocol)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "unsupported OTLP protocol")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNewOTLPTraceExporterInvalidProtocol(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "ftp")
	_, err := newOTLPTraceExporter(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported OTLP protocol")
}

func TestNewOTLPMetricExporterInvalidProtocol(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_PROTOCOL", "ftp")
	_, err := newOTLPMetricExporter(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported OTLP protocol")
}

func TestWithPhaseRecordsErrorStatus(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	const tracerName = "test-controller"
	ctx, parent := StartReconcileSpan(context.Background(), tracerName, "parent")

	phaseErr := errors.New("deployment failed")
	err := WithPhase(ctx, "child.phase", func(context.Context) error {
		return phaseErr
	})
	require.ErrorIs(t, err, phaseErr)
	parent.End()

	spans := exporter.GetSpans()
	require.Len(t, spans, 2)

	var child *tracetest.SpanStub
	for i := range spans {
		if spans[i].Name == "child.phase" {
			child = &spans[i]
			break
		}
	}
	require.NotNil(t, child)
	assert.Equal(t, codes.Error, child.Status.Code)
	assert.Contains(t, child.Status.Description, "deployment failed")
}

func TestRecordPhaseErrorOnNonFatalPhase(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	prevTP := otel.GetTracerProvider()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(prevTP)
	})

	ctx, parent := StartReconcileSpan(context.Background(), "test-controller", "parent")
	softErr := errors.New("route unavailable")
	err := WithPhase(ctx, "child.phase", func(ctx context.Context) error {
		RecordPhaseError(ctx, softErr)
		return nil
	})
	require.NoError(t, err)
	parent.End()

	spans := exporter.GetSpans()
	require.Len(t, spans, 2)

	var child *tracetest.SpanStub
	for i := range spans {
		if spans[i].Name == "child.phase" {
			child = &spans[i]
			break
		}
	}
	require.NotNil(t, child)
	assert.Equal(t, codes.Error, child.Status.Code)
	assert.Contains(t, child.Status.Description, "route unavailable")
}

func TestSetReconcileOutcome(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	_, span := Tracer("test").Start(context.Background(), "reconcile")
	SetReconcileOutcome(span, false, 30*time.Second, nil)
	span.End()

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "requeue", attrValue(spans[0].Attributes, "reconcile.outcome"))
	assert.Equal(t, "30s", attrValue(spans[0].Attributes, "reconcile.requeue_after"))
}

func TestOtlpProtocolTraceSpecificWins(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "http/protobuf")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
	assert.Equal(t, "http/protobuf", otlpTraceProtocol())
}

func TestFinishReconcileOutcomePreservesExplicitOutcome(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	_, span := Tracer("test").Start(context.Background(), "reconcile")
	SetSpanOutcome(span, "invalid_placement")
	outcome := FinishReconcileOutcome(span, true, 0, errors.New("ignored"))
	span.End()

	assert.Equal(t, "invalid_placement", outcome)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "invalid_placement", attrValue(spans[0].Attributes, "reconcile.outcome"))
	assert.Equal(t, codes.Ok, spans[0].Status.Code)
}

func attrValue(attrs []attribute.KeyValue, key string) string {
	for _, a := range attrs {
		if string(a.Key) == key {
			return a.Value.AsString()
		}
	}
	return ""
}

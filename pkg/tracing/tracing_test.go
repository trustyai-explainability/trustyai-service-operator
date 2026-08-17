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
	t.Setenv("OTEL_SDK_DISABLED", "")

	shutdown, err := Setup(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	_, span := Tracer("test").Start(context.Background(), "noop-span")
	span.End()
	assert.False(t, span.SpanContext().IsValid())
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
	assert.Equal(t, "http/protobuf", otlpProtocol())
}

func TestFinishReconcileOutcomePreservesExplicitOutcome(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	_, span := Tracer("test").Start(context.Background(), "reconcile")
	SetSpanOutcome(span, "invalid_placement")
	FinishReconcileOutcome(span, true, 0, errors.New("ignored"))
	span.End()

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

// Package tracing provides opt-in OpenTelemetry tracing for the TrustyAI Service Operator.
//
// Tracing is disabled unless OTLP export is configured via environment variables:
//   - OTEL_EXPORTER_OTLP_ENDPOINT (or OTEL_EXPORTER_OTLP_TRACES_ENDPOINT)
//   - OTEL_EXPORTER_OTLP_PROTOCOL (optional: grpc default, or http/protobuf)
//   - OTEL_SERVICE_NAME (optional, default: trustyai-service-operator)
//   - OTEL_SDK_DISABLED=true disables tracing even when an endpoint is set
package tracing

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const defaultServiceName = "trustyai-service-operator"

type tracerScopeKey struct{}

var explicitReconcileOutcomes sync.Map

// Setup initializes the global TracerProvider. When OTLP is not configured, a noop provider is used.
func Setup(ctx context.Context) (func(context.Context) error, error) {
	if !tracingEnabled() {
		otel.SetTracerProvider(noop.NewTracerProvider())
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := newOTLPExporter(ctx)
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName()),
			semconv.ServiceVersion(constants.Version),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create trace resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

// Tracer returns a named tracer from the global TracerProvider.
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

// StartReconcileSpan starts a parent reconcile span and stores the tracer scope in ctx.
func StartReconcileSpan(ctx context.Context, tracerName, spanName string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	ctx = context.WithValue(ctx, tracerScopeKey{}, tracerName)
	tracer := otel.Tracer(tracerName)
	return tracer.Start(ctx, spanName, trace.WithAttributes(attrs...))
}

// WithPhase runs fn inside a child span using the tracer scope from ctx.
func WithPhase(ctx context.Context, spanName string, fn func(context.Context) error) error {
	tracer := tracerFromContext(ctx)
	ctx, span := tracer.Start(ctx, spanName)
	defer span.End()

	if err := fn(ctx); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

// SetReconcileOutcome records the reconcile result on the parent span.
func SetReconcileOutcome(span trace.Span, requeue bool, requeueAfter time.Duration, err error) {
	outcome := "success"
	if err != nil {
		outcome = "error"
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else if requeue || requeueAfter > 0 {
		outcome = "requeue"
		if requeueAfter > 0 {
			span.SetAttributes(attribute.String("reconcile.requeue_after", requeueAfter.String()))
		}
	} else {
		span.SetStatus(codes.Ok, "")
	}
	span.SetAttributes(attribute.String("reconcile.outcome", outcome))
}

// SetSpanOutcome sets reconcile.outcome without treating the span as an error (e.g. validation halt).
func SetSpanOutcome(span trace.Span, outcome string) {
	span.SetAttributes(attribute.String("reconcile.outcome", outcome))
	span.SetStatus(codes.Ok, "")
	if sc := span.SpanContext(); sc.IsValid() {
		explicitReconcileOutcomes.Store(spanContextKey(sc), struct{}{})
	}
}

// FinishReconcileOutcome records reconcile result unless SetSpanOutcome already set an explicit outcome.
func FinishReconcileOutcome(span trace.Span, requeue bool, requeueAfter time.Duration, err error) {
	if sc := span.SpanContext(); sc.IsValid() {
		if _, explicit := explicitReconcileOutcomes.LoadAndDelete(spanContextKey(sc)); explicit {
			return
		}
	}
	SetReconcileOutcome(span, requeue, requeueAfter, err)
}

func spanContextKey(sc trace.SpanContext) string {
	return sc.TraceID().String() + "\x00" + sc.SpanID().String()
}

func tracerFromContext(ctx context.Context) trace.Tracer {
	name, _ := ctx.Value(tracerScopeKey{}).(string)
	if name == "" {
		name = defaultServiceName
	}
	return otel.Tracer(name)
}

func tracingEnabled() bool {
	if sdkDisabled() {
		return false
	}
	return otlpEndpoint() != ""
}

func sdkDisabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("OTEL_SDK_DISABLED")))
	return v == "true" || v == "1"
}

func otlpEndpoint() string {
	if ep := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")); ep != "" {
		return ep
	}
	return strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
}

func serviceName() string {
	if name := strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME")); name != "" {
		return name
	}
	return defaultServiceName
}

func otlpProtocol() string {
	if protocol := strings.TrimSpace(strings.ToLower(os.Getenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL"))); protocol != "" {
		return protocol
	}
	return strings.TrimSpace(strings.ToLower(os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")))
}

func newOTLPExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	protocol := otlpProtocol()
	if protocol == "http/protobuf" || protocol == "http" {
		return otlptracehttp.New(ctx)
	}
	return otlptracegrpc.New(ctx)
}

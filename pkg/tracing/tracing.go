// Package tracing provides opt-in OpenTelemetry tracing and metrics for the TrustyAI Service Operator.
//
// Tracing is disabled unless OTLP trace export is configured via environment variables:
//   - OTEL_EXPORTER_OTLP_ENDPOINT (or OTEL_EXPORTER_OTLP_TRACES_ENDPOINT)
//   - OTEL_EXPORTER_OTLP_PROTOCOL (optional: grpc default, or http/protobuf)
//   - OTEL_EXPORTER_OTLP_TRACES_PROTOCOL (optional, overrides OTEL_EXPORTER_OTLP_PROTOCOL for traces)
//
// Metrics can be exported two ways (independently or together):
//   - Prometheus scrape on the operator :8080/metrics endpoint via the OTEL Prometheus exporter
//     (enabled by default; set OTEL_METRICS_PROMETHEUS_DISABLED=true to disable)
//   - OTLP push when OTEL_EXPORTER_OTLP_ENDPOINT or OTEL_EXPORTER_OTLP_METRICS_ENDPOINT is set
//   - OTEL_EXPORTER_OTLP_METRICS_PROTOCOL (optional, overrides OTEL_EXPORTER_OTLP_PROTOCOL for metrics)
//   - OTEL_SERVICE_NAME (optional, default: trustyai-service-operator)
//   - OTEL_SDK_DISABLED=true disables tracing and metrics even when endpoints are set
//   - OTEL_METRICS_PROMETHEUS_DISABLED=true disables the Prometheus bridge on :8080/metrics
package tracing

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/sdk/resource"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

const defaultServiceName = "trustyai-service-operator"

type tracerScopeKey struct{}
type phaseSpanKey struct{}

var explicitReconcileOutcomes sync.Map

var setupOnce sync.Once
var setupShutdown func(context.Context) error
var setupErr error

// Setup initializes global TracerProvider and MeterProvider.
// When OTLP is not configured for a signal, a noop provider is used for that signal.
// Safe to call multiple times; only the first invocation takes effect.
func Setup(ctx context.Context) (func(context.Context) error, error) {
	setupOnce.Do(func() {
		setupShutdown, setupErr = doSetup(ctx)
	})
	return setupShutdown, setupErr
}

func doSetup(ctx context.Context) (func(context.Context) error, error) {
	var shutdowns []func(context.Context) error

	if tracingEnabled() {
		traceShutdown, err := setupTracing(ctx)
		if err != nil {
			return nil, err
		}
		shutdowns = append(shutdowns, traceShutdown)
	} else {
		otel.SetTracerProvider(noop.NewTracerProvider())
	}

	if metricsEnabled() {
		metricsShutdown, err := setupMetrics(ctx)
		if err != nil {
			_ = runShutdowns(ctx, shutdowns)
			return nil, err
		}
		shutdowns = append(shutdowns, metricsShutdown)
	} else {
		otel.SetMeterProvider(metricnoop.NewMeterProvider())
	}

	return func(ctx context.Context) error {
		return runShutdowns(ctx, shutdowns)
	}, nil
}

// ResetForTest resets the Setup sync.Once so tests can call Setup multiple times.
// Must only be used in tests.
func ResetForTest() {
	setupOnce = sync.Once{}
	setupShutdown = nil
	setupErr = nil
}

func runShutdowns(ctx context.Context, shutdowns []func(context.Context) error) error {
	var errs []error
	for _, shutdown := range shutdowns {
		if err := shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Tracer returns a named tracer from the global TracerProvider.
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

// Meter returns a named meter from the global MeterProvider.
func Meter(name string) metric.Meter {
	return otel.Meter(name)
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

	ctx = context.WithValue(ctx, phaseSpanKey{}, span)
	if err := fn(ctx); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

// RecordPhaseError records a non-fatal error on the active WithPhase span.
func RecordPhaseError(ctx context.Context, err error) {
	if err == nil {
		return
	}
	span, _ := ctx.Value(phaseSpanKey{}).(trace.Span)
	if span == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// ReconcileOutcome derives the reconcile result label from controller-runtime result and error.
func ReconcileOutcome(requeue bool, requeueAfter time.Duration, err error) string {
	if err != nil {
		return "error"
	}
	if requeue || requeueAfter > 0 {
		return "requeue"
	}
	return "success"
}

// SetReconcileOutcome records the reconcile result on the parent span.
func SetReconcileOutcome(span trace.Span, requeue bool, requeueAfter time.Duration, err error) {
	outcome := ReconcileOutcome(requeue, requeueAfter, err)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else if requeue || requeueAfter > 0 {
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
		explicitReconcileOutcomes.Store(spanContextKey(sc), outcome)
	}
}

// FinishReconcileOutcome records reconcile result unless SetSpanOutcome already set an explicit outcome.
func FinishReconcileOutcome(span trace.Span, requeue bool, requeueAfter time.Duration, err error) string {
	if sc := span.SpanContext(); sc.IsValid() {
		if outcome, explicit := explicitReconcileOutcomes.LoadAndDelete(spanContextKey(sc)); explicit {
			return outcome.(string)
		}
	}
	SetReconcileOutcome(span, requeue, requeueAfter, err)
	return ReconcileOutcome(requeue, requeueAfter, err)
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

func setupTracing(ctx context.Context) (func(context.Context) error, error) {
	exporter, err := newOTLPTraceExporter(ctx)
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}

	res, err := otelResource()
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

func setupMetrics(ctx context.Context) (func(context.Context) error, error) {
	res, err := otelResource()
	if err != nil {
		return nil, err
	}

	var providerOpts []sdkmetric.Option
	providerOpts = append(providerOpts, sdkmetric.WithResource(res))

	if prometheusMetricsEnabled() {
		promExporter, err := otelprom.New(otelprom.WithRegisterer(ctrlmetrics.Registry))
		if err != nil {
			return nil, fmt.Errorf("create Prometheus metric exporter: %w", err)
		}
		providerOpts = append(providerOpts, sdkmetric.WithReader(promExporter))
	}

	if otlpMetricsEndpoint() != "" {
		otlpExporter, err := newOTLPMetricExporter(ctx)
		if err != nil {
			return nil, fmt.Errorf("create OTLP metric exporter: %w", err)
		}
		providerOpts = append(providerOpts, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(otlpExporter)))
	}

	mp := sdkmetric.NewMeterProvider(providerOpts...)
	otel.SetMeterProvider(mp)
	return mp.Shutdown, nil
}

func otelResource() (*resource.Resource, error) {
	res, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(
			semconv.ServiceName(serviceName()),
			semconv.ServiceVersion(constants.Version),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create otel resource: %w", err)
	}
	return res, nil
}

func tracingEnabled() bool {
	if sdkDisabled() {
		return false
	}
	return otlpTraceEndpoint() != ""
}

func metricsEnabled() bool {
	if sdkDisabled() {
		return false
	}
	return prometheusMetricsEnabled() || otlpMetricsEndpoint() != ""
}

func prometheusMetricsEnabled() bool {
	if sdkDisabled() {
		return false
	}
	v := strings.TrimSpace(strings.ToLower(os.Getenv("OTEL_METRICS_PROMETHEUS_DISABLED")))
	return v != "true" && v != "1"
}

func sdkDisabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("OTEL_SDK_DISABLED")))
	return v == "true" || v == "1"
}

func otlpTraceEndpoint() string {
	if ep := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")); ep != "" {
		return ep
	}
	return strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
}

func otlpMetricsEndpoint() string {
	if ep := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT")); ep != "" {
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

func otlpTraceProtocol() string {
	if protocol := strings.TrimSpace(strings.ToLower(os.Getenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL"))); protocol != "" {
		return protocol
	}
	return strings.TrimSpace(strings.ToLower(os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")))
}

func otlpMetricsProtocol() string {
	if protocol := strings.TrimSpace(strings.ToLower(os.Getenv("OTEL_EXPORTER_OTLP_METRICS_PROTOCOL"))); protocol != "" {
		return protocol
	}
	return strings.TrimSpace(strings.ToLower(os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")))
}

type otlpExporterKind int

const (
	otlpExporterGRPC otlpExporterKind = iota
	otlpExporterHTTP
)

func resolveOTLPProtocol(protocol string) (otlpExporterKind, error) {
	switch strings.TrimSpace(strings.ToLower(protocol)) {
	case "", "grpc":
		return otlpExporterGRPC, nil
	case "http", "http/protobuf":
		return otlpExporterHTTP, nil
	default:
		return 0, fmt.Errorf(
			"unsupported OTLP protocol %q (supported: grpc, http/protobuf)",
			protocol,
		)
	}
}

func newOTLPTraceExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	kind, err := resolveOTLPProtocol(otlpTraceProtocol())
	if err != nil {
		return nil, err
	}
	switch kind {
	case otlpExporterHTTP:
		return otlptracehttp.New(ctx)
	default:
		return otlptracegrpc.New(ctx)
	}
}

func newOTLPMetricExporter(ctx context.Context) (sdkmetric.Exporter, error) {
	kind, err := resolveOTLPProtocol(otlpMetricsProtocol())
	if err != nil {
		return nil, err
	}
	switch kind {
	case otlpExporterHTTP:
		return otlpmetrichttp.New(ctx)
	default:
		return otlpmetricgrpc.New(ctx)
	}
}

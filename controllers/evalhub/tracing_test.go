package evalhub

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/trustyai-explainability/trustyai-service-operator/pkg/tracing"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

func TestEvalHubReconcileSpanAttributes(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(noop.NewTracerProvider())
		_ = tp.Shutdown(context.Background())
	})

	ctx, span := startEvalHubReconcileSpan(context.Background(), spanReconcile, "ns-1", "hub-1", 7)
	span.End()

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, spanReconcile, spans[0].Name)
	assert.Equal(t, "ns-1", attrString(spans[0].Attributes, "k8s.namespace"))
	assert.Equal(t, "hub-1", attrString(spans[0].Attributes, "evalhub.name"))
	assert.Equal(t, int64(7), attrInt64(spans[0].Attributes, "reconcile.generation"))

	_ = ctx
}

func TestEvalHubReconcilePhaseSpanOnFailure(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(noop.NewTracerProvider())
		_ = tp.Shutdown(context.Background())
	})

	ctx, parent := startEvalHubReconcileSpan(context.Background(), spanReconcile, "ns-1", "hub-1", 1)
	phaseErr := errors.New("deployment unavailable")
	err := tracing.WithPhase(ctx, spanReconcileDeployment, func(context.Context) error {
		return phaseErr
	})
	require.ErrorIs(t, err, phaseErr)
	parent.End()

	names := spanNames(exporter.GetSpans())
	assert.Contains(t, names, spanReconcile)
	assert.Contains(t, names, spanReconcileDeployment)
}

func TestExitCodeFromPod(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			InitContainerStatuses: []corev1.ContainerStatus{
				{
					Name: initContainerName,
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Reason:   "Error",
							ExitCode: 42,
						},
					},
				},
			},
		},
	}
	code, ok := exitCodeFromPod(pod)
	require.True(t, ok)
	assert.Equal(t, int32(42), code)
}

func TestTruncateFailureReason(t *testing.T) {
	long := strings.Repeat("a", 600)
	truncated := truncateFailureReason(long)
	assert.LessOrEqual(t, len([]rune(truncated)), maxFailureReasonRunes+1)
	assert.True(t, strings.HasSuffix(truncated, "…"))

	unicodeLong := strings.Repeat("é", 600)
	truncatedUnicode := truncateFailureReason(unicodeLong)
	assert.Equal(t, maxFailureReasonRunes+1, len([]rune(truncatedUnicode)))
	assert.True(t, strings.HasSuffix(truncatedUnicode, "…"))
	withinLimit := strings.Repeat("é", maxFailureReasonRunes)
	assert.Equal(t, withinLimit, truncateFailureReason(withinLimit))
}

func TestFinishEvalHubReconcileSpanPreservesExplicitOutcome(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(noop.NewTracerProvider())
		_ = tp.Shutdown(context.Background())
	})

	_, span := startEvalHubReconcileSpan(context.Background(), spanReconcile, "ns-1", "hub-1", 1)
	tracing.SetSpanOutcome(span, "validation_error")
	finishEvalHubReconcileSpan(span, metricControllerEvalHub, time.Now(), ctrl.Result{}, errors.New("ignored"))
	span.End()

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "validation_error", attrString(spans[0].Attributes, "reconcile.outcome"))
	assert.Equal(t, codes.Ok, spans[0].Status.Code)
}

func attrString(attrs []attribute.KeyValue, key string) string {
	for _, a := range attrs {
		if string(a.Key) == key {
			return a.Value.AsString()
		}
	}
	return ""
}

func attrInt64(attrs []attribute.KeyValue, key string) int64 {
	for _, a := range attrs {
		if string(a.Key) == key {
			return a.Value.AsInt64()
		}
	}
	return 0
}

func spanNames(spans tracetest.SpanStubs) []string {
	names := make([]string, len(spans))
	for i := range spans {
		names[i] = spans[i].Name
	}
	return names
}

func TestJobFailureSpanExitCodeAttribute(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-1"},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: sidecarContainerName,
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Reason:   "Error",
							ExitCode: 13,
						},
					},
				},
			},
		},
	}
	code, ok := exitCodeFromPod(pod)
	require.True(t, ok)
	assert.Equal(t, int32(13), code)

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(noop.NewTracerProvider())
		_ = tp.Shutdown(context.Background())
	})

	_, span := tracing.StartReconcileSpan(context.Background(), evalHubTracerName, spanJobFailureReconcile,
		attribute.String("k8s.namespace", "tenant-ns"),
		attribute.String("evalhub.job.name", "job-1"),
		attribute.String("evalhub.job.failure_reason", "sidecar failed"),
		attribute.Int("evalhub.job.exit_code", int(code)),
	)
	span.End()

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, spanJobFailureReconcile, spans[0].Name)
	assert.Equal(t, int64(13), attrInt64(spans[0].Attributes, "evalhub.job.exit_code"))

	_ = batchv1.Job{}
}

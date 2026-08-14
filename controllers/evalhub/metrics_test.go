package evalhub

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func resetEvalHubMetricsForTest() {
	evalHubMetricsOnce = sync.Once{}
	evalHubMetricsErr = nil
}

func setupEvalHubMetricsTest(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	resetEvalHubMetricsForTest()
	t.Cleanup(func() {
		resetEvalHubMetricsForTest()
	})
	return reader
}

func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &metrics))
	return metrics
}

func TestRecordEvalHubReconcileMetrics(t *testing.T) {
	reader := setupEvalHubMetricsTest(t)

	recordEvalHubReconcileMetrics(metricControllerEvalHub, "success", nil, 250*time.Millisecond)

	metrics := collectMetrics(t, reader)
	require.Len(t, metrics.ScopeMetrics, 1)
	scopeMetrics := metrics.ScopeMetrics[0].Metrics
	require.Len(t, scopeMetrics, 2)

	assert.Equal(t, metricReconcileDuration, scopeMetrics[0].Name)
	histogram, ok := scopeMetrics[0].Data.(metricdata.Histogram[float64])
	require.True(t, ok)
	require.Len(t, histogram.DataPoints, 1)
	attrs := histogram.DataPoints[0].Attributes
	assert.Equal(t, metricControllerEvalHub, attributeValue(attrs, "controller"))
	assert.Equal(t, "success", attributeValue(attrs, "result"))
	assert.Equal(t, uint64(1), histogram.DataPoints[0].Count)

	assert.Equal(t, metricReconcileTotal, scopeMetrics[1].Name)
	total, ok := scopeMetrics[1].Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, total.DataPoints, 1)
	assert.Equal(t, int64(1), total.DataPoints[0].Value)
}

func TestRecordEvalHubReconcileErrorMetrics(t *testing.T) {
	reader := setupEvalHubMetricsTest(t)

	recordEvalHubReconcileMetrics(metricControllerEvalHub, "error", assert.AnError, time.Second)

	metrics := collectMetrics(t, reader)
	scopeMetrics := metrics.ScopeMetrics[0].Metrics
	require.Len(t, scopeMetrics, 3)

	assert.Equal(t, metricReconcileErrors, scopeMetrics[2].Name)
	errorsSum, ok := scopeMetrics[2].Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, errorsSum.DataPoints, 1)
	assert.Equal(t, errorTypeOther, attributeValue(errorsSum.DataPoints[0].Attributes, "error_type"))
}

func TestClassifyFailureReason(t *testing.T) {
	assert.Equal(t, failureReasonInit, classifyFailureReason("init container failed"))
	assert.Equal(t, failureReasonAdapter, classifyFailureReason("adapter exited with code 1"))
	assert.Equal(t, failureReasonSidecar, classifyFailureReason("sidecar crash loop"))
	assert.Equal(t, failureReasonScheduling, classifyFailureReason("pod unschedulable"))
	assert.Equal(t, failureReasonOther, classifyFailureReason("unknown failure"))
}

func TestRecordJobFailureEvent(t *testing.T) {
	reader := setupEvalHubMetricsTest(t)

	recordJobFailureEvent("sidecar container failed")

	metrics := collectMetrics(t, reader)
	scopeMetrics := metrics.ScopeMetrics[0].Metrics
	require.Len(t, scopeMetrics, 1)
	assert.Equal(t, metricJobFailureEvents, scopeMetrics[0].Name)

	sum, ok := scopeMetrics[0].Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, sum.DataPoints, 1)
	assert.Equal(t, failureReasonSidecar, attributeValue(sum.DataPoints[0].Attributes, "failure_reason"))
}

func TestRecordManagedInstanceDelta(t *testing.T) {
	reader := setupEvalHubMetricsTest(t)

	recordManagedInstanceDelta(1)
	recordManagedInstanceDelta(-1)

	metrics := collectMetrics(t, reader)
	sum, ok := metrics.ScopeMetrics[0].Metrics[0].Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, sum.DataPoints, 1)
	assert.Equal(t, int64(0), sum.DataPoints[0].Value)
}

func attributeValue(attrs attribute.Set, key string) string {
	value, ok := attrs.Value(attribute.Key(key))
	if !ok {
		return ""
	}
	return value.AsString()
}

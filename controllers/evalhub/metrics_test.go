package evalhub

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	evalhubv1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func resetEvalHubMetricsForTest() {
	evalHubMetricsOnce = sync.Once{}
	evalHubMetricsErr = nil
	managedEvalHubListerMu.Lock()
	managedEvalHubLister = nil
	managedEvalHubListerMu.Unlock()
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

func metricByName(t *testing.T, metrics metricdata.ResourceMetrics, name string) metricdata.Metrics {
	t.Helper()
	require.Len(t, metrics.ScopeMetrics, 1)
	for _, m := range metrics.ScopeMetrics[0].Metrics {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("metric %q not found", name)
	return metricdata.Metrics{}
}

func TestRecordEvalHubReconcileMetrics(t *testing.T) {
	reader := setupEvalHubMetricsTest(t)

	recordEvalHubReconcileMetrics(metricControllerEvalHub, "success", nil, 250*time.Millisecond)

	metrics := collectMetrics(t, reader)

	durationMetric := metricByName(t, metrics, metricReconcileDuration)
	histogram, ok := durationMetric.Data.(metricdata.Histogram[float64])
	require.True(t, ok)
	require.Len(t, histogram.DataPoints, 1)
	attrs := histogram.DataPoints[0].Attributes
	assert.Equal(t, metricControllerEvalHub, attributeValue(attrs, "controller"))
	assert.Equal(t, "success", attributeValue(attrs, "result"))
	assert.Equal(t, uint64(1), histogram.DataPoints[0].Count)

	totalMetric := metricByName(t, metrics, metricReconcileTotal)
	total, ok := totalMetric.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, total.DataPoints, 1)
	assert.Equal(t, int64(1), total.DataPoints[0].Value)
}

func TestRecordEvalHubReconcileErrorMetrics(t *testing.T) {
	reader := setupEvalHubMetricsTest(t)

	recordEvalHubReconcileMetrics(metricControllerEvalHub, "error", assert.AnError, time.Second)

	metrics := collectMetrics(t, reader)

	errorsMetric := metricByName(t, metrics, metricReconcileErrors)
	errorsSum, ok := errorsMetric.Data.(metricdata.Sum[int64])
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
	jobFailureMetric := metricByName(t, metrics, metricJobFailureEvents)
	assert.Equal(t, metricJobFailureEvents, jobFailureMetric.Name)

	sum, ok := jobFailureMetric.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, sum.DataPoints, 1)
	assert.Equal(t, failureReasonSidecar, attributeValue(sum.DataPoints[0].Attributes, "failure_reason"))
}

func TestObserveManagedEvalHubCount(t *testing.T) {
	reader := setupEvalHubMetricsTest(t)

	managed := &evalhubv1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "managed",
			Namespace:  "default",
			Finalizers: []string{evalhubv1.FinalizerName},
		},
	}
	unmanaged := &evalhubv1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unmanaged",
			Namespace: "default",
		},
	}
	scheme := runtime.NewScheme()
	require.NoError(t, evalhubv1.AddToScheme(scheme))
	SetManagedEvalHubLister(fake.NewClientBuilder().WithScheme(scheme).WithObjects(managed, unmanaged).Build())
	t.Cleanup(func() { SetManagedEvalHubLister(nil) })

	_, err := getEvalHubMetrics()
	require.NoError(t, err)

	metrics := collectMetrics(t, reader)
	gaugeMetric := metricByName(t, metrics, metricManagedInstances)
	assert.Equal(t, metricManagedInstances, gaugeMetric.Name)

	gauge, ok := gaugeMetric.Data.(metricdata.Gauge[int64])
	require.True(t, ok)
	require.Len(t, gauge.DataPoints, 1)
	assert.Equal(t, int64(1), gauge.DataPoints[0].Value)
}

func TestCountManagedEvalHubs(t *testing.T) {
	managed := &evalhubv1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "managed",
			Namespace:  "default",
			Finalizers: []string{evalhubv1.FinalizerName},
		},
	}
	withoutFinalizer := &evalhubv1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pending",
			Namespace: "default",
		},
	}
	scheme := runtime.NewScheme()
	require.NoError(t, evalhubv1.AddToScheme(scheme))
	SetManagedEvalHubLister(fake.NewClientBuilder().WithScheme(scheme).WithObjects(managed, withoutFinalizer).Build())
	t.Cleanup(func() { SetManagedEvalHubLister(nil) })

	count, err := countManagedEvalHubs(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
	assert.True(t, controllerutil.ContainsFinalizer(managed, evalhubv1.FinalizerName))
}

func TestManagedInstancesGaugeRegisteredOnIdleCluster(t *testing.T) {
	reader := setupEvalHubMetricsTest(t)

	scheme := runtime.NewScheme()
	require.NoError(t, evalhubv1.AddToScheme(scheme))
	SetManagedEvalHubLister(fake.NewClientBuilder().WithScheme(scheme).Build())
	t.Cleanup(func() { SetManagedEvalHubLister(nil) })

	_, err := getEvalHubMetrics()
	require.NoError(t, err)

	metrics := collectMetrics(t, reader)
	gaugeMetric := metricByName(t, metrics, metricManagedInstances)
	gauge, ok := gaugeMetric.Data.(metricdata.Gauge[int64])
	require.True(t, ok)
	require.Len(t, gauge.DataPoints, 1)
	assert.Equal(t, int64(0), gauge.DataPoints[0].Value)
}

func TestGaugeInitializedAtStartupBeforeReconciliation(t *testing.T) {
	reader := setupEvalHubMetricsTest(t)

	_, err := getEvalHubMetrics()
	require.NoError(t, err, "getEvalHubMetrics should succeed even with nil lister")

	metrics := collectMetrics(t, reader)
	gaugeMetric := metricByName(t, metrics, metricManagedInstances)
	gauge, ok := gaugeMetric.Data.(metricdata.Gauge[int64])
	require.True(t, ok, "managed_instances should be a gauge")
	require.Len(t, gauge.DataPoints, 1)
	assert.Equal(t, int64(0), gauge.DataPoints[0].Value,
		"gauge should report 0 when no lister is configured (pre-reconciliation startup)")
}

func attributeValue(attrs attribute.Set, key string) string {
	value, ok := attrs.Value(attribute.Key(key))
	if !ok {
		return ""
	}
	return value.AsString()
}

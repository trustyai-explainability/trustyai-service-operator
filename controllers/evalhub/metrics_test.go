package evalhub

import (
	"errors"
	"fmt"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// histogramCount extracts the sample count from a prometheus.Observer by type-asserting
// to prometheus.Histogram (always valid for metrics returned by HistogramVec.WithLabelValues).
func histogramCount(t *testing.T, obs prometheus.Observer) uint64 {
	t.Helper()
	h, ok := obs.(prometheus.Histogram)
	require.True(t, ok, "observer must implement prometheus.Histogram")
	var m dto.Metric
	require.NoError(t, h.Write(&m))
	return m.GetHistogram().GetSampleCount()
}

func TestClassifyError(t *testing.T) {
	gr := schema.GroupResource{Group: "trustyai.opendatahub.io", Resource: "evalhubs"}
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, errorTypeOther},
		{"not_found", k8serrors.NewNotFound(gr, "test"), errorTypeNotFound},
		{"status_update", errors.New("failed to update status for resource"), errorTypeStatusUpdateFailed},
		{"update", errors.New("failed to update resource"), errorTypeUpdateFailed},
		{"patch", errors.New("failed to patch object"), errorTypeUpdateFailed},
		{"config", errors.New("database config is wrong"), errorTypeConfigInvalid},
		{"invalid", errors.New("spec is invalid"), errorTypeConfigInvalid},
		{"missing", errors.New("secret is missing"), errorTypeConfigInvalid},
		{"other", errors.New("some unexpected error"), errorTypeOther},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, classifyError(tt.err))
		})
	}
}

func TestRecordReconcile_AllResultLabels(t *testing.T) {
	results := []string{resultSuccess, resultRequeue, resultError}
	for i, result := range results {
		ctrl := fmt.Sprintf("test-reconcile-result-%d", i)
		result := result
		t.Run(result, func(t *testing.T) {
			recordReconcile(ctrl, result, 42*time.Millisecond)

			assert.Equal(t, float64(1),
				testutil.ToFloat64(reconcileTotal.WithLabelValues(ctrl, result)),
				"reconcile_total counter should be 1 after one call")

			assert.Equal(t, uint64(1),
				histogramCount(t, reconcileDuration.WithLabelValues(ctrl, result)),
				"reconcile_duration histogram should have sample count 1 after one observation")
		})
	}
}

func TestRecordReconcile_DurationObserved(t *testing.T) {
	ctrl := "test-reconcile-duration"
	recordReconcile(ctrl, resultSuccess, 100*time.Millisecond)

	var m dto.Metric
	h := reconcileDuration.WithLabelValues(ctrl, resultSuccess).(prometheus.Histogram)
	require.NoError(t, h.Write(&m))
	// 100 ms = 0.1 s; sum must be at least that
	assert.GreaterOrEqual(t, m.GetHistogram().GetSampleSum(), 0.09,
		"histogram sum should reflect the observed duration")
}

func TestRecordReconcileError_AllErrorTypes(t *testing.T) {
	errorTypes := []string{
		errorTypeNotFound,
		errorTypeUpdateFailed,
		errorTypeStatusUpdateFailed,
		errorTypeConfigInvalid,
		errorTypeOther,
	}
	for i, errType := range errorTypes {
		ctrl := fmt.Sprintf("test-reconcile-error-%d", i)
		errType := errType
		t.Run(errType, func(t *testing.T) {
			recordReconcileError(ctrl, errType)
			assert.Equal(t, float64(1),
				testutil.ToFloat64(reconcileErrors.WithLabelValues(ctrl, errType)),
				"reconcile_errors_total counter should be 1 after one call")
		})
	}
}

func TestSetManagedInstances(t *testing.T) {
	ctrl := "test-gauge"

	setManagedInstances(ctrl, 3)
	assert.Equal(t, float64(3),
		testutil.ToFloat64(managedInstances.WithLabelValues(ctrl)),
		"gauge should reflect the set value")

	setManagedInstances(ctrl, 7)
	assert.Equal(t, float64(7),
		testutil.ToFloat64(managedInstances.WithLabelValues(ctrl)),
		"gauge should update to the new value")
}

func TestRecordJobFailureEvent_AllReasons(t *testing.T) {
	reasons := []string{failureReasonRuntimeFailure, failureReasonQueueError, failureReasonOther}
	for i, reason := range reasons {
		ctrl := fmt.Sprintf("test-failure-%d", i)
		reason := reason
		t.Run(reason, func(t *testing.T) {
			recordJobFailureEvent(ctrl, reason)
			assert.Equal(t, float64(1),
				testutil.ToFloat64(jobFailureEvents.WithLabelValues(ctrl, reason)),
				"failure_events_total counter should be 1 after one call")
		})
	}
}

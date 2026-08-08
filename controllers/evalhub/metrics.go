package evalhub

import (
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	resultSuccess = "success"
	resultRequeue = "requeue"
	resultError   = "error"

	errorTypeNotFound           = "not_found"
	errorTypeUpdateFailed       = "update_failed"
	errorTypeStatusUpdateFailed = "status_update_failed"
	errorTypeConfigInvalid      = "config_invalid"
	errorTypeOther              = "other"

	failureReasonRuntimeFailure = "runtime_failure"
	failureReasonQueueError     = "queue_error"
	failureReasonOther          = "other"
)

var (
	reconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "evalhub_controller_reconcile_duration_seconds",
			Help:    "Duration in seconds of EvalHub controller reconciliation cycles.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"controller", "result"},
	)

	reconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "evalhub_controller_reconcile_total",
			Help: "Total number of EvalHub controller reconciliations by result.",
		},
		[]string{"controller", "result"},
	)

	reconcileErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "evalhub_controller_reconcile_errors_total",
			Help: "Total number of EvalHub controller reconciliation errors by error type.",
		},
		[]string{"controller", "error_type"},
	)

	managedInstances = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "evalhub_controller_managed_instances",
			Help: "Current number of EvalHub instances managed by the controller.",
		},
		[]string{"controller"},
	)

	jobFailureEvents = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "evalhub_evaluation_job_failure_events_total",
			Help: "Total number of evaluation job failure events recorded by the EvalHub controller.",
		},
		[]string{"controller", "failure_reason"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		reconcileDuration,
		reconcileTotal,
		reconcileErrors,
		managedInstances,
		jobFailureEvents,
	)
}

func recordReconcile(controller, result string, duration time.Duration) {
	reconcileTotal.WithLabelValues(controller, result).Inc()
	reconcileDuration.WithLabelValues(controller, result).Observe(duration.Seconds())
}

func recordReconcileError(controller, errorType string) {
	reconcileErrors.WithLabelValues(controller, errorType).Inc()
}

func setManagedInstances(controller string, count float64) {
	managedInstances.WithLabelValues(controller).Set(count)
}

func recordJobFailureEvent(controller, reason string) {
	jobFailureEvents.WithLabelValues(controller, reason).Inc()
}

// classifyError maps an error to a fixed enum to prevent unbounded label cardinality.
func classifyError(err error) string {
	if err == nil {
		return errorTypeOther
	}
	if k8serrors.IsNotFound(err) {
		return errorTypeNotFound
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "status") {
		return errorTypeStatusUpdateFailed
	}
	if strings.Contains(msg, "update") || strings.Contains(msg, "patch") {
		return errorTypeUpdateFailed
	}
	if strings.Contains(msg, "config") || strings.Contains(msg, "invalid") || strings.Contains(msg, "missing") {
		return errorTypeConfigInvalid
	}
	return errorTypeOther
}

package evalhub

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/trustyai-explainability/trustyai-service-operator/pkg/tracing"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	metricControllerEvalHub    = "evalhub"
	metricControllerDeletion   = "evalhub_deletion"
	metricControllerJobFailure = "job_failure"

	metricReconcileDuration = "evalhub.controller.reconcile.duration"
	metricReconcileTotal    = "evalhub.controller.reconcile.total"
	metricReconcileErrors   = "evalhub.controller.reconcile.errors"
	metricManagedInstances  = "evalhub.controller.managed_instances"
	metricJobFailureEvents  = "evalhub.controller.job_failure.events"

	errorTypeOther      = "other"
	errorTypeGet        = "get"
	errorTypeValidation = "validation"
	errorTypePlacement  = "placement"
	errorTypeRBAC       = "rbac"
	errorTypeConfigMap  = "configmap"
	errorTypeDeployment = "deployment"
	errorTypeService    = "service"
	errorTypeRoute      = "route"
	errorTypeStatus     = "status"
	errorTypeJobFailure = "job_failure"
	errorTypeNotFound   = "not_found"
	errorTypeConflict   = "conflict"
	errorTypeTimeout    = "timeout"

	failureReasonOther      = "other"
	failureReasonSidecar    = "sidecar"
	failureReasonInit       = "init"
	failureReasonAdapter    = "adapter"
	failureReasonScheduling = "scheduling"
)

type evalHubOTelMetrics struct {
	reconcileDuration metric.Float64Histogram
	reconcileTotal    metric.Int64Counter
	reconcileErrors   metric.Int64Counter
	jobFailureEvents  metric.Int64Counter
}

var (
	evalHubMetrics     evalHubOTelMetrics
	evalHubMetricsOnce sync.Once
	evalHubMetricsErr  error
)

func getEvalHubMetrics() (evalHubOTelMetrics, error) {
	evalHubMetricsOnce.Do(func() {
		meter := tracing.Meter(evalHubTracerName)

		evalHubMetrics.reconcileDuration, evalHubMetricsErr = meter.Float64Histogram(
			metricReconcileDuration,
			metric.WithDescription("Wall-clock duration of EvalHub controller reconcile cycles in seconds"),
			metric.WithUnit("s"),
		)
		if evalHubMetricsErr != nil {
			return
		}

		evalHubMetrics.reconcileTotal, evalHubMetricsErr = meter.Int64Counter(
			metricReconcileTotal,
			metric.WithDescription("Total EvalHub controller reconcile cycles"),
		)
		if evalHubMetricsErr != nil {
			return
		}

		evalHubMetrics.reconcileErrors, evalHubMetricsErr = meter.Int64Counter(
			metricReconcileErrors,
			metric.WithDescription("Total EvalHub controller reconcile errors by type"),
		)
		if evalHubMetricsErr != nil {
			return
		}

		evalHubMetrics.jobFailureEvents, evalHubMetricsErr = meter.Int64Counter(
			metricJobFailureEvents,
			metric.WithDescription("Evaluation job failure events handled by the operator"),
		)
	})
	return evalHubMetrics, evalHubMetricsErr
}

func finishEvalHubReconcile(span trace.Span, controller string, start time.Time, result ctrl.Result, err error) {
	outcome := tracing.FinishReconcileOutcome(span, !result.IsZero(), result.RequeueAfter, err)
	recordEvalHubReconcileMetrics(controller, outcome, err, time.Since(start))
}

func recordEvalHubReconcileMetrics(controller, outcome string, reconcileErr error, duration time.Duration) {
	metrics, err := getEvalHubMetrics()
	if err != nil {
		return
	}

	ctx := context.Background()
	resultAttrs := metric.WithAttributes(
		attribute.String("controller", controller),
		attribute.String("result", outcome),
	)
	metrics.reconcileDuration.Record(ctx, duration.Seconds(), resultAttrs)
	metrics.reconcileTotal.Add(ctx, 1, resultAttrs)

	if reconcileErr != nil && outcome == "error" {
		metrics.reconcileErrors.Add(ctx, 1, metric.WithAttributes(
			attribute.String("controller", controller),
			attribute.String("error_type", classifyReconcileError(reconcileErr)),
		))
	}
}

// ManagedInstanceCounter is a function that returns the current count of managed EvalHub instances.
type ManagedInstanceCounter func(ctx context.Context) (int64, error)

// registerManagedInstancesGauge registers an observable gauge that reports the
// current number of managed EvalHub CRs. The callback queries the cluster on
// each metrics collection, so the value is always accurate regardless of
// operator restarts.
func registerManagedInstancesGauge(counter ManagedInstanceCounter) error {
	meter := tracing.Meter(evalHubTracerName)
	_, err := meter.Int64ObservableGauge(
		metricManagedInstances,
		metric.WithDescription("Current number of EvalHub custom resources managed by the operator"),
		metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
			count, err := counter(ctx)
			if err != nil {
				return nil
			}
			o.Observe(count)
			return nil
		}),
	)
	return err
}

func recordJobFailureEvent(failureReason string) {
	metrics, err := getEvalHubMetrics()
	if err != nil {
		return
	}
	metrics.jobFailureEvents.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String("failure_reason", classifyFailureReason(failureReason)),
	))
}

func classifyReconcileError(err error) string {
	if err == nil {
		return errorTypeOther
	}
	if apierrors.IsNotFound(err) {
		return errorTypeNotFound
	}
	if apierrors.IsConflict(err) {
		return errorTypeConflict
	}
	if apierrors.IsTimeout(err) || apierrors.IsServerTimeout(err) || apierrors.IsTooManyRequests(err) {
		return errorTypeTimeout
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "rbac"), strings.Contains(msg, "serviceaccount"), strings.Contains(msg, "rolebinding"), strings.Contains(msg, "clusterrole"):
		return errorTypeRBAC
	case strings.Contains(msg, "configmap"):
		return errorTypeConfigMap
	case strings.Contains(msg, "deployment"):
		return errorTypeDeployment
	case strings.Contains(msg, "service"):
		return errorTypeService
	case strings.Contains(msg, "route"):
		return errorTypeRoute
	case strings.Contains(msg, "status"):
		return errorTypeStatus
	case strings.Contains(msg, "database"), strings.Contains(msg, "validation"), strings.Contains(msg, "spec."):
		return errorTypeValidation
	case strings.Contains(msg, "placement"), strings.Contains(msg, "tenant namespace"):
		return errorTypePlacement
	case strings.Contains(msg, "job"):
		return errorTypeJobFailure
	case strings.Contains(msg, "get "):
		return errorTypeGet
	default:
		return errorTypeOther
	}
}

func classifyFailureReason(reason string) string {
	lower := strings.ToLower(reason)
	switch {
	case strings.Contains(lower, initContainerName):
		return failureReasonInit
	case strings.Contains(lower, adapterContainerName):
		return failureReasonAdapter
	case strings.Contains(lower, sidecarContainerName):
		return failureReasonSidecar
	case strings.Contains(lower, "unschedulable"), strings.Contains(lower, "scheduling"):
		return failureReasonScheduling
	default:
		return failureReasonOther
	}
}

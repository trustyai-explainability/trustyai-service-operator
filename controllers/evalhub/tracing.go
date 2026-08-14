package evalhub

import (
	"context"
	"time"

	"github.com/trustyai-explainability/trustyai-service-operator/pkg/tracing"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	ctrl "sigs.k8s.io/controller-runtime"
)

const evalHubTracerName = "evalhub-controller"

const (
	spanReconcile           = "evalhub.reconcile"
	spanReconcileDeletion   = "evalhub.reconcile.deletion"
	spanReconcileRBAC       = "evalhub.reconcile.rbac"
	spanReconcileConfigMap  = "evalhub.reconcile.configmap"
	spanReconcileDeployment = "evalhub.reconcile.deployment"
	spanReconcileService    = "evalhub.reconcile.service"
	spanReconcileRoute      = "evalhub.reconcile.route"
	spanReconcileMCP        = "evalhub.reconcile.mcp"
	spanReconcileStatus     = "evalhub.reconcile.status"
	spanJobFailureReconcile = "evalhub.job_failure_reconcile"
)

func evalHubReconcileAttrs(namespace, name string, generation int64) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("k8s.namespace", namespace),
		attribute.String("evalhub.name", name),
		attribute.Int64("reconcile.generation", generation),
	}
}

func startEvalHubReconcileSpan(ctx context.Context, spanName, namespace, name string, generation int64) (context.Context, trace.Span) {
	return tracing.StartReconcileSpan(ctx, evalHubTracerName, spanName, evalHubReconcileAttrs(namespace, name, generation)...)
}

func finishEvalHubReconcileSpan(span trace.Span, controller string, start time.Time, result ctrl.Result, err error) {
	finishEvalHubReconcile(span, controller, start, result, err)
}

const maxFailureReasonRunes = 512

func truncateFailureReason(msg string) string {
	runes := []rune(msg)
	if len(runes) <= maxFailureReasonRunes {
		return msg
	}
	return string(runes[:maxFailureReasonRunes]) + "…"
}

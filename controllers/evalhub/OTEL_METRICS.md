# EvalHub controller OTLP metrics

This document describes how to enable OpenTelemetry (OTEL) **metrics** export for the **TrustyAI Service Operator** when running the EvalHub controller.

**Related work:** [RHAI-241](https://redhat.atlassian.net/browse/RHAI-241) (controller tracing, PR #877), [RHAI-240](https://redhat.atlassian.net/browse/RHAI-240) (Prometheus metrics on `:8080` — separate delivery path).

---

## Operator OTLP metrics vs other observability signals

| Layer | Signal | Delivery | Configuration |
| ----- | ------ | -------- | ------------- |
| **Operator controller** (this document) | OTLP metrics | Push to OTLP collector | `OTEL_EXPORTER_OTLP_METRICS_*` env vars on operator Deployment |
| **Operator controller** | OTLP traces | Push to OTLP collector | `OTEL_EXPORTER_OTLP_TRACES_*` env vars — see [OTEL_TRACING.md](OTEL_TRACING.md) |
| **Operator controller** | Prometheus metrics | Scrape `:8080/metrics` | controller-runtime defaults (RHAI-240 scope) |
| **EvalHub server** (workload) | OTLP traces/metrics/logs | Push from EvalHub process | `spec.otel` on EvalHub CR |

Traces and OTLP metrics can be enabled independently. Configuring `spec.otel` on an EvalHub instance does **not** enable operator controller metrics.

---

## Enabling operator OTLP metrics

Metrics export is **opt-in**. By default the operator uses a noop meter and exports nothing. Metrics are pushed outbound to an OTLP collector; no new ports are opened on the operator pod.

### Required configuration

Set at least one of these on the operator manager container:

| Variable | Required | Description |
| -------- | -------- | ----------- |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Yes* | Shared OTLP collector host:port (e.g. `otel-collector.openshift-operators.svc:4317`) |
| `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT` | Yes* | Metrics-specific endpoint; takes precedence over `OTEL_EXPORTER_OTLP_ENDPOINT` when set |

\*Metrics export stays disabled when neither endpoint variable is set.

### Optional configuration

| Variable | Default | Description |
| -------- | ------- | ----------- |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `grpc` | Default protocol for OTLP exporters |
| `OTEL_EXPORTER_OTLP_METRICS_PROTOCOL` | falls back to `OTEL_EXPORTER_OTLP_PROTOCOL` | Metrics-specific protocol (`grpc` or `http/protobuf`) |
| `OTEL_SERVICE_NAME` | `trustyai-service-operator` | `service.name` resource attribute |
| `OTEL_SDK_DISABLED` | unset | Set to `true` or `1` to disable metrics and traces |

### Example: patch the operator Deployment

```bash
kubectl patch deployment trustyai-service-operator-controller-manager -n opendatahub --type='json' -p='[
  {"op": "add", "path": "/spec/template/spec/containers/0/env/-", "value": {
    "name": "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
    "value": "otel-collector.openshift-operators.svc:4317"
  }},
  {"op": "add", "path": "/spec/template/spec/containers/0/env/-", "value": {
    "name": "OTEL_SERVICE_NAME",
    "value": "trustyai-service-operator"
  }}
]'
```

To export both traces and metrics to the same collector, set `OTEL_EXPORTER_OTLP_ENDPOINT` once (or set trace- and metrics-specific endpoints separately).

---

## Exported metrics

Instrumentation scope: `evalhub-controller`.

| Instrument | Type | Attributes | When recorded |
| ---------- | ---- | ---------- | ------------- |
| `evalhub.controller.reconcile.duration` | Histogram (seconds) | `controller`, `result` | End of each reconcile cycle (includes initial resource fetch) |
| `evalhub.controller.reconcile.total` | Counter | `controller`, `result` | End of each reconcile cycle (includes initial resource fetch) |
| `evalhub.controller.reconcile.errors` | Counter | `controller`, `error_type` | Reconcile cycles with `result=error` |
| `evalhub.controller.managed_instances` | Gauge | — | Each metrics collection (lists EvalHub CRs with active finalizers) |
| `evalhub.controller.job_failure.events` | Counter | `failure_reason` | After successful EvalHub failure POST |

### Controller labels

| `controller` value | Source |
| ------------------ | ------ |
| `evalhub` | Main `EvalHubReconciler` reconcile loop |
| `evalhub_deletion` | EvalHub finalizer cleanup |
| `job_failure` | `EvalHubEvaluationJobFailureReconciler` |

### Result labels

`success`, `requeue`, `error`, `validation_error`, `invalid_placement` (explicit outcomes preserved from tracing).

### Error type labels (fixed enum)

`get`, `validation`, `placement`, `rbac`, `configmap`, `deployment`, `service`, `route`, `status`, `job_failure`, `not_found`, `conflict`, `timeout`, `other`.

### Failure reason labels (bounded)

`init`, `adapter`, `sidecar`, `scheduling`, `other`.

---

## Technical implementation

| File | Role |
| ---- | ---- |
| [`pkg/tracing/tracing.go`](../../pkg/tracing/tracing.go) | Shared OTEL bootstrap for traces and metrics |
| [`controllers/evalhub/metrics.go`](metrics.go) | EvalHub OTEL instruments and recording helpers |
| [`controllers/evalhub/tracing.go`](tracing.go) | Shared reconcile completion (`finishEvalHubReconcileSpan`) |
| [`controllers/evalhub/evalhub_controller.go`](evalhub_controller.go) | Reconcile timing and managed-instance tracking |
| [`controllers/evalhub/evaluation_job_failure_reconciler.go`](evaluation_job_failure_reconciler.go) | Job failure event counter |

### Managed instances counter

The managed-instances gauge uses an observable callback that lists EvalHub CRs with the finalizer present on each metrics collection interval. This always reports the accurate current count regardless of operator restarts — no delta tracking or baseline initialization required.

### Tests

```bash
go test ./pkg/tracing/... ./controllers/evalhub/... -run 'Metric|Tracing|Reconcile|Record|Classify'
```

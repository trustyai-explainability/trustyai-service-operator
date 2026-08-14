# EvalHub controller OTLP metrics

This document describes how OpenTelemetry (OTEL) **metrics** are exported for the **TrustyAI Service Operator** when running the EvalHub controller.

**Related work:** [RHAI-241](https://redhat.atlassian.net/browse/RHAI-241) (controller tracing, PR #877), [RHAI-240](https://redhat.atlassian.net/browse/RHAI-240) (Prometheus metrics on `:8080`), PR #878 (OTLP metrics).

---

## Operator OTLP metrics vs other observability signals

| Layer | Signal | Delivery | Configuration |
| ----- | ------ | -------- | ------------- |
| **Operator controller** (this document) | EvalHub OTEL metrics → Prometheus | Scrape existing `:8080/metrics` via OTEL Prometheus bridge | Enabled by default; `OTEL_METRICS_PROMETHEUS_DISABLED=true` to disable |
| **Operator controller** | EvalHub OTEL metrics → OTLP | Push to OTLP collector | `OTEL_EXPORTER_OTLP_METRICS_*` env vars |
| **Operator controller** | OTLP traces | Push to OTLP collector | `OTEL_EXPORTER_OTLP_TRACES_*` env vars — see [OTEL_TRACING.md](OTEL_TRACING.md) |
| **Operator controller** | controller-runtime metrics | Scrape `:8080/metrics` | Built-in (workqueue, webhook, etc.) |
| **EvalHub server** (workload) | OTLP traces/metrics/logs | Push from EvalHub process | `spec.otel` on EvalHub CR |

EvalHub controller metrics are recorded via the OTEL SDK and exposed to Prometheus through the [OTEL Prometheus exporter](https://pkg.go.dev/go.opentelemetry.io/otel/exporters/prometheus), registered with the controller-runtime metrics registry. The existing ServiceMonitor on port 8080 scrapes them at `/metrics` with no manifest changes.

---

## Prometheus bridge (default)

The OTEL Prometheus bridge is **on by default**. EvalHub reconcile metrics appear on the operator's existing `:8080/metrics` endpoint alongside controller-runtime metrics. No OTLP collector is required for Prometheus scraping.

To disable the bridge (OTLP-only or fully disabled metrics):

```bash
OTEL_METRICS_PROMETHEUS_DISABLED=true
```

When `OTEL_SDK_DISABLED=true`, both the Prometheus bridge and OTLP export are disabled.

---

## OTLP push (optional)

Set at least one of these on the operator manager container to **also** push metrics to an OTLP collector (in addition to Prometheus scrape when the bridge is enabled):

| Variable | Required | Description |
| -------- | -------- | ----------- |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Yes* | Shared OTLP collector host:port |
| `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT` | Yes* | Metrics-specific endpoint; takes precedence over `OTEL_EXPORTER_OTLP_ENDPOINT` when set |

\*OTLP push stays disabled when neither endpoint variable is set.

### Optional OTLP configuration

| Variable | Default | Description |
| -------- | ------- | ----------- |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `grpc` | Default protocol for OTLP exporters |
| `OTEL_EXPORTER_OTLP_METRICS_PROTOCOL` | falls back to `OTEL_EXPORTER_OTLP_PROTOCOL` | Metrics-specific protocol (`grpc` or `http/protobuf`) |
| `OTEL_SERVICE_NAME` | `trustyai-service-operator` | `service.name` resource attribute |
| `OTEL_SDK_DISABLED` | unset | Set to `true` or `1` to disable metrics and traces |
| `OTEL_METRICS_PROMETHEUS_DISABLED` | unset | Set to `true` or `1` to disable Prometheus bridge on `:8080/metrics` |

### Example: OTLP push only (no Prometheus bridge)

```bash
kubectl patch deployment trustyai-service-operator-controller-manager -n opendatahub --type='json' -p='[
  {"op": "add", "path": "/spec/template/spec/containers/0/env/-", "value": {
    "name": "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
    "value": "otel-collector.openshift-operators.svc:4317"
  }},
  {"op": "add", "path": "/spec/template/spec/containers/0/env/-", "value": {
    "name": "OTEL_METRICS_PROMETHEUS_DISABLED",
    "value": "true"
  }}
]'
```

### Example: both Prometheus scrape and OTLP push

Leave `OTEL_METRICS_PROMETHEUS_DISABLED` unset and set `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT` (or shared `OTEL_EXPORTER_OTLP_ENDPOINT`). Metrics are scraped from `:8080/metrics` and pushed to the collector.

---

## Exported metrics

Instrumentation scope: `evalhub-controller`.

Prometheus names are derived from OTEL instrument names by the exporter (dots become underscores, e.g. `evalhub_controller_reconcile_total`).

| OTEL instrument | Type | Attributes | When recorded |
| ----------------- | ---- | ---------- | ------------- |
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
| [`pkg/tracing/tracing.go`](../../pkg/tracing/tracing.go) | OTEL bootstrap: Prometheus bridge on `metrics.Registry` + optional OTLP push |
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

# EvalHub controller distributed tracing

This document describes how to enable OpenTelemetry (OTEL) tracing for the **TrustyAI Service Operator** when running the EvalHub controller, and how the instrumentation is implemented.

**Related work:** [RHAI-241](https://redhat.atlassian.net/browse/RHAI-241) (controller tracing), [OTEL_METRICS.md](OTEL_METRICS.md) (controller OTLP metrics), sibling [RHAI-240](https://redhat.atlassian.net/browse/RHAI-240) (Prometheus metrics on `:8080`).

---

## Operator tracing vs EvalHub workload tracing

These are separate concerns:

| Layer | What it traces | How it is configured |
| ----- | -------------- | -------------------- |
| **Operator controller** (this document) | EvalHub reconcile loops and evaluation job failure handling in the operator process | Environment variables on the **operator Deployment** (`controller-manager`) |
| **EvalHub server** (managed workload) | EvalHub API, jobs, sidecars | `spec.otel` on the **EvalHub CR** → rendered into the instance `config.yaml` |

Configuring `spec.otel` on an EvalHub instance does **not** enable operator reconcile tracing. Both can be enabled independently.

---

## Enabling operator tracing

Tracing is **opt-in**. By default the operator uses a noop tracer and exports nothing. No new ports are opened on the operator pod; spans are pushed outbound to an OTLP collector.

### Required configuration

Set at least one of these on the operator manager container:

| Variable | Required | Description |
| -------- | -------- | ----------- |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Yes* | OTLP collector URL (e.g. `http://otel-collector.openshift-operators.svc:4317` for in-cluster plaintext gRPC; use `https://` for TLS — see below) |
| `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` | Yes* | Trace-specific endpoint; takes precedence over `OTEL_EXPORTER_OTLP_ENDPOINT` when set (for OTLP/HTTP use the full path, e.g. `http://otel-collector.openshift-operators.svc:4318/v1/traces`) |

\*Tracing stays disabled when neither endpoint variable is set.

### Optional configuration

| Variable | Default | Description |
| -------- | ------- | ----------- |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `grpc` | Use `http/protobuf` or `http` for OTLP/HTTP |
| `OTEL_SERVICE_NAME` | `trustyai-service-operator` | `service.name` resource attribute in exported traces |
| `OTEL_SDK_DISABLED` | unset | Set to `true` or `1` to force tracing off even when an endpoint is configured |

Standard [OTEL SDK environment variables](https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/) for headers, TLS, and timeouts are also respected by the OTLP exporters.

**TLS endpoints:** When the collector requires TLS, use `https://` in the endpoint URL and set `OTEL_EXPORTER_OTLP_CERTIFICATE` to the path of the CA bundle (e.g. a mounted Secret or the OpenShift service-CA). For mTLS, also set `OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE` and `OTEL_EXPORTER_OTLP_CLIENT_KEY`.

### Example: patch the operator Deployment

After deploying the operator (for example into `opendatahub`):

```bash
kubectl patch deployment trustyai-service-operator-controller-manager -n opendatahub --type='json' -p='[
  {"op": "add", "path": "/spec/template/spec/containers/0/env/-", "value": {
    "name": "OTEL_EXPORTER_OTLP_ENDPOINT",
    "value": "http://otel-collector.openshift-operators.svc:4317"
  }},
  {"op": "add", "path": "/spec/template/spec/containers/0/env/-", "value": {
    "name": "OTEL_SERVICE_NAME",
    "value": "trustyai-service-operator"
  }}
]'
```

The deployment restarts automatically. Adjust the collector address and namespace for your cluster (Tempo, Jaeger, SigNoz, OpenShift cluster monitoring, etc.).

### Example: kustomize overlay snippet

Add to the manager container env in your overlay (do not commit a hardcoded production collector unless that is intentional for your environment):

```yaml
# config/manager/manager.yaml (or overlay patch)
spec:
  template:
    spec:
      containers:
        - name: manager
          env:
            - name: OTEL_EXPORTER_OTLP_ENDPOINT
              value: "http://otel-collector.example.svc:4317"
            - name: OTEL_SERVICE_NAME
              value: "trustyai-service-operator"
            # For OTLP/HTTP collectors:
            # - name: OTEL_EXPORTER_OTLP_PROTOCOL
            #   value: "http/protobuf"
            # - name: OTEL_EXPORTER_OTLP_ENDPOINT
            #   value: "http://otel-collector.example.svc:4318"
            # For TLS (https://) collectors, configure certificate trust:
            # - name: OTEL_EXPORTER_OTLP_ENDPOINT
            #   value: "https://otel-collector.example.svc:4317"
            # - name: OTEL_EXPORTER_OTLP_CERTIFICATE
            #   value: "/var/run/secrets/otel/ca.crt"
```

### Verifying traces

1. Ensure the operator pod has the env vars: `kubectl describe pod -l control-plane=trustyai-service-operator -n <namespace>`.
2. Create or update an EvalHub CR to trigger reconciliation.
3. In your trace backend, search for spans named `evalhub.reconcile` with `service.name=trustyai-service-operator` (or your `OTEL_SERVICE_NAME` override).
4. To confirm the noop path, remove the endpoint env vars and verify the operator still runs normally.

---

## What gets traced

### EvalHub reconciler (`EvalHubReconciler`)

**Parent spans**

| Span name | When |
| --------- | ---- |
| `evalhub.reconcile` | Normal reconcile after the EvalHub CR is fetched |
| `evalhub.reconcile.deletion` | Finalizer cleanup when `deletionTimestamp` is set |

**Parent attributes**

- `k8s.namespace` — EvalHub instance namespace
- `evalhub.name` — EvalHub CR name
- `reconcile.generation` — CR `metadata.generation`
- `reconcile.outcome` — `success`, `error`, `requeue`, `validation_error`, or `invalid_placement`
- `reconcile.requeue_after` — present when the reconcile requeues with a delay

**Child phase spans** (under `evalhub.reconcile`)

| Span name | Reconcile phase |
| --------- | --------------- |
| `evalhub.reconcile.rbac` | ServiceAccounts, tenant namespace bindings, single-tenancy roles |
| `evalhub.reconcile.configmap` | Instance ConfigMap, service CA, provider/collection ConfigMaps |
| `evalhub.reconcile.deployment` | EvalHub Deployment |
| `evalhub.reconcile.service` | Service, metrics Service, ServiceMonitor |
| `evalhub.reconcile.route` | OpenShift Route (errors are logged; span does not fail the reconcile) |
| `evalhub.reconcile.mcp` | MCP server resources (only when MCP is enabled) |
| `evalhub.reconcile.status` | Status update from deployment readiness |

A failing phase span is marked with OTEL error status and `RecordError` so SREs can see which sub-reconciler failed without reading controller logs.

### Evaluation job failure reconciler (`EvalHubEvaluationJobFailureReconciler`)

| Span name | `evalhub.job_failure_reconcile` |
| --------- | ------------------------------- |

**Attributes**

- `k8s.namespace` — job namespace
- `evalhub.job.name` — Kubernetes Job name
- `evalhub.job.failure_reason` — operator-detected failure message (truncated to 512 characters)
- `evalhub.job.exit_code` — first non-zero exit code from init/adapter/sidecar containers, when available
- `evalhub.job_failure.action` — `skip`, `post`, or `delete`

---

## Technical implementation

### Architecture

```golang
cmd/main.go
  └── tracing.Setup()          # global TracerProvider (OTLP or noop)
        └── EvalHub controllers
              ├── evalhub_controller.go     # parent + phase spans via tracing.WithPhase
              └── evaluation_job_failure_reconciler.go
```

### Key packages and files

| File | Role |
| ---- | ---- |
| [`pkg/tracing/tracing.go`](../../pkg/tracing/tracing.go) | Shared OTEL bootstrap, `StartReconcileSpan`, `WithPhase`, outcome helpers |
| [`cmd/main.go`](../../cmd/main.go) | Calls `tracing.Setup()` at startup; `Shutdown` on exit |
| [`controllers/evalhub/tracing.go`](tracing.go) | EvalHub span name constants and reconcile attribute helpers |
| [`controllers/evalhub/evalhub_controller.go`](evalhub_controller.go) | Phase instrumentation in `Reconcile()` |
| [`controllers/evalhub/evaluation_job_failure_reconciler.go`](evaluation_job_failure_reconciler.go) | Job failure span and exit-code extraction |

### Bootstrap behaviour

1. On startup, `tracing.Setup()` checks `OTEL_SDK_DISABLED` and whether an OTLP endpoint env var is set.
2. If disabled or unset → installs `noop.NewTracerProvider()` (negligible overhead per reconcile).
3. If enabled → creates an OTLP trace exporter (gRPC by default, HTTP when `OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf`), batches spans, and sets resource attributes (`service.name`, `service.version` from operator build constants).
4. Instrumentation scope for EvalHub spans: `evalhub-controller`.

### Design notes

- **No new CLI flags** — configuration follows the standard OTEL environment-variable pattern used by other platform components.
- **No new exposed ports** — traces egress to the collector; nothing listens for trace ingestion on the operator pod.
- **Reusable for other controllers** — `pkg/tracing` is controller-agnostic; TAS/LMES can adopt the same helpers in future work.
- **Distinct from `spec.otel`** — workload OTEL remains configured per EvalHub instance via the CR and [`configmap.go`](configmap.go).

### Tests

Unit tests (no cluster required):

```bash
go test ./pkg/tracing/... ./controllers/evalhub/... -run 'Tracing|TestEvalHubReconcile|TestExitCode|TestTruncate|TestJobFailure|TestSetup|TestWithPhase|TestSetReconcile'
```

Tests use an in-memory `tracetest` exporter to assert span names, attributes, and error propagation on failed phases.

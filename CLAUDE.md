# CLAUDE.md — trustyai-service-operator

## Overview

Multi-service Kubernetes operator managing 6 service types via dynamic controller registration. Built with kubebuilder and controller-runtime v0.17.0, Go 1.23.

**Module:** `github.com/trustyai-explainability/trustyai-service-operator`

## Build & Run

```bash
# Build
make build                          # Binary → bin/manager
make docker-build IMG=<image>       # Container image (includes tests)
make docker-buildx                  # Multi-arch build

# Run locally (requires kubeconfig)
make run                            # Default: TAS only
ENABLED_SERVICES=TAS,LMES,EVALHUB make run

# Code generation (run after changing api/ types or RBAC markers)
make manifests                      # CRDs, RBAC, webhooks → config/
make generate                       # DeepCopy methods → zz_generated.deepcopy.go

# Formatting & linting
make fmt                            # go fmt
make vet                            # go vet
```

## Test

```bash
make test                           # All tests with envtest (cover.out)
```

- **Framework:** Ginkgo v2 + Gomega + controller-runtime envtest
- **K8s version:** 1.29.0 (envtest binaries auto-downloaded)
- **CRDs loaded from:** `config/crd/bases/`
- **Timeouts:** 10s default, 250ms polling
- Tests are co-located with controllers: `controllers/<service>/*_test.go`
- Each controller has a `suite_test.go` that bootstraps envtest

Run a specific controller's tests:
```bash
go test -v ./controllers/evalhub/...
go test -v ./controllers/lmes/... -run TestValidation
```

## Deploy

```bash
make install                        # Install CRDs into cluster
make deploy IMG=<image>             # Deploy operator via kustomize
make undeploy                       # Remove operator

# Generate release bundle
make manifest-gen NAMESPACE=opendatahub OPERATOR_IMAGE=quay.io/trustyai/trustyai-service-operator:v1.39.0
oc apply -f release/trustyai_bundle.yaml -n opendatahub
```

## Debug

- Logger: zap in development mode (structured JSON, verbose by default)
- Health: `:8081/healthz`, `:8081/readyz`
- Metrics: `:8080/metrics` (Prometheus)
- Flags:
  - `--enable-services TAS,LMES,EVALHUB` — which controllers to start
  - `--configmap <name>` — operator settings ConfigMap (default: `trustyai-service-operator-config`)
  - `--leader-elect` — HA mode
  - Zap flags via `--zap-log-level`, `--zap-devel`, etc.
- Cache disabled for ConfigMap, Secret, Pod, Service (prevents OOM from cluster-wide informers)

## Architecture

### Service Registration

Controllers register via `init()` in `controllers/<service>.go`:
```go
func init() {
    registerService(evalhub.ServiceName, setupEvalHubController)
}
```

`cmd/main.go` calls `controllers.SetupControllers(enabledServices, mgr, ...)` which invokes each registered setup function.

**Service names** (use with `--enable-services`):

| Name | Controller | CRD |
|------|-----------|-----|
| `TAS` | `controllers/tas/` | `TrustyAIService` (v1alpha1, v1) |
| `LMES` | `controllers/lmes/` | `LMEvalJob` (v1alpha1) |
| `EVALHUB` | `controllers/evalhub/` | `EvalHub` (v1alpha1) |
| `GORCH` | `controllers/gorch/` | `GuardrailsOrchestrator` (v1alpha1) |
| `NEMO_GUARDRAILS` | `controllers/nemo_guardrails/` | `NemoGuardrail` (v1alpha1) |
| `JOB_MGR` | `controllers/job_mgr/` | (watches external jobs) |

### Project Structure

```
cmd/
  main.go                           # Entrypoint: scheme registration, manager setup, flag parsing
  lmes_driver/                      # LMES driver job entrypoint
api/                                # CRD type definitions (kubebuilder markers)
  tas/v1alpha1/, tas/v1/            # TrustyAIService (with version conversion)
  lmes/v1alpha1/                    # LMEvalJob
  evalhub/v1alpha1/                 # EvalHub
  gorch/v1alpha1/                   # GuardrailsOrchestrator
  nemo_guardrails/v1alpha1/         # NemoGuardrail
  common/                           # Shared types (Condition, CABundle)
controllers/
  controllers.go                    # Service registry + SetupControllers()
  <service>.go                      # Registration shim (calls registerService in init())
  <service>/                        # Reconciler + helpers + tests
    *_controller.go                 # Main Reconcile() loop
    *_test.go                       # Unit tests (envtest)
    suite_test.go                   # Test environment bootstrap
    constants.go                    # Service-specific constants
  constants/                        # Shared constants
  utils/                            # Shared helpers (namespace detection, etc.)
  metrics/                          # Prometheus metrics
  dsc/                              # DataScienceCluster integration
  job_mgr/                          # Job lifecycle management
config/
  base/                             # Base kustomization (namePrefix, labels, params)
  crd/bases/                        # Generated CRD YAML (from make manifests)
  rbac/                             # ClusterRoles, ServiceAccounts, RoleBindings
    evalhub/                        # EvalHub-specific static ClusterRoles
  manager/                          # Operator Deployment manifest
  prometheus/                       # ServiceMonitor definitions
  configmaps/                       # Provider/collection ConfigMaps
  samples/                          # Example CRs for each service
```

### Reconciliation Pattern

All controllers follow the standard loop:

1. Fetch CR instance (handle NotFound → cleanup)
2. Check `DeletionTimestamp` → run finalizer cleanup
3. Add finalizer if missing
4. Reconcile desired state (create/update sub-resources)
5. Update `Status` (Phase, Conditions, Ready)
6. Return result: `{Requeue: true, RequeueAfter: duration}` or `nil`

**Key patterns:**
- Owner references via `controllerutil.SetControllerReference()` for GC
- Finalizers via `controllerutil.AddFinalizer()` / `RemoveFinalizer()`
- Status updates via `r.Status().Update(ctx, instance)`
- Events via `recorder.Event(instance, EventType, reason, message)`
- Sub-reconcilers (TAS pattern): separate functions with signature `func(ctx, req, instance) (*Result, error)`

### Scheme Registration (cmd/main.go init())

Registers all API groups in order:
- Kubernetes core (`clientgoscheme`)
- TrustyAI: `tasv1alpha1`, `tasv1`, `lmesv1alpha1`, `evalhubv1alpha1`, `gorchv1alpha1`, `nemoguardrailsv1alpha1`
- External: `monitoringv1` (Prometheus), `kservev1alpha1`/`v1beta1`, `routev1` (OpenShift), `apiextensionsv1`, `kueuev1beta1`

### Key Dependencies

| Dependency | Version | Purpose |
|-----------|---------|---------|
| `controller-runtime` | v0.17.0 | Operator framework |
| `client-go` | v0.29.2 | Kubernetes client |
| `kserve` | v0.12.1 | InferenceService types |
| `kueue` | v0.6.2 | Job queueing |
| `prometheus-operator` | v0.64.1 | ServiceMonitor types |
| `openshift/api` | — | Route types |
| `ginkgo/v2` | v2.23.3 | Test framework |
| `gomega` | v1.37.0 | Test assertions |
| `viper` | v1.19.0 | Configuration |

## Code Generation

```bash
make manifests      # controller-gen rbac:roleName=manager-role crd webhook paths="./..."
make generate       # controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./..."
```

- **controller-gen:** v0.20.0 (auto-downloaded to `bin/`)
- **kustomize:** v3.8.7
- **Generated files:** `config/crd/bases/*.yaml`, `config/rbac/*.yaml`, `api/**/zz_generated.deepcopy.go`
- **RBAC markers** in controller files drive ClusterRole generation (e.g. `//+kubebuilder:rbac:groups=...`)

## Container Image

- **Build stage:** `registry.access.redhat.com/ubi8/go-toolset:1.23`
- **Runtime stage:** `registry.access.redhat.com/ubi8/ubi-minimal:latest`
- CGO disabled, distroless-style
- Runs as non-root (65532:65532)
- Build tool: `podman` (configurable via `BUILD_TOOL`)

## CI/CD

GitHub Actions in `.github/workflows/`:
- `controller-tests.yaml` — runs `make test`
- `lint-yaml.yaml` — yamllint on `config/**/*.yaml`
- `gosec.yaml` — security scanning (SARIF output)
- `smoke.yaml` — smoke tests
- `.dependabot.yml` — auto-updates Go deps

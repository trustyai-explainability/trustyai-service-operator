# TrustyAI Create-Time Logger Webhook Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans (inline execution is selected here). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Inject the TrustyAI logger into eligible KServe InferenceServices during CREATE when a TrustyAIService already exists, so KServe builds the serving topology with the logger while preserving the existing reconciliation fallback and its customer-controlled upgrade behavior.

**Architecture:** Add a TAS-owned, namespace-scoped mutating admission handler that reads the InferenceService CREATE request, lists TrustyAIService objects in the same namespace, and returns a JSON patch only for RawDeployment/Standard services without an existing logger. The TAS reconciler owns the `trustyai.opendatahub.io/enabled` namespace label used by the webhook selector. Register the handler on the existing controller-runtime webhook server with an Ignore failure policy and a short timeout; retain the current reconciler for pre-existing services and later topology changes. Install the webhook configuration beside the TAS resources and ensure the manager certificate volume is available in every overlay that enables TAS webhooks.

**Tech Stack:** Go 1.26, controller-runtime v0.22.5 admission API, KServe v0.19.0 API types, Kubernetes admissionregistration.k8s.io/v1, Kustomize, Ginkgo/Gomega.

**Spec:** `docs/TrustyAI_sendinference_investigation.md` in the opendatahub-tests workspace, especially the live create-time versus late-Logger comparison and the operator issue #647 recommendation.

## Global Constraints

- The webhook is CREATE-only and must never modify an existing InferenceService or force a Deployment rollout for an unchanged serving topology.
- Existing Logger specifications, explicit logger URLs, unsupported deployment modes, and namespaces without a TrustyAIService must be preserved unchanged.
- Admission failures must not block KServe users; the webhook uses `failurePolicy: Ignore` and the handler logs decode/read/marshal failures before allowing the request through.
- Keep the current TrustyAI reconciliation fallback for existing InferenceServices and for services created before the TrustyAIService.
- Scope admission with `trustyai.opendatahub.io/enabled=true`; add the label while a TrustyAIService is active and remove it after the last service is gone.
- Do not add a Go module dependency; use controller-runtime and Kubernetes packages already in `go.mod`.
- Do not commit or alter unrelated worktrees or files.

---

### Task 1: Add failing webhook behavior tests

**Files:**
- Create: `controllers/tas/inferenceservice_webhook_test.go`

**Interfaces:**
- The tests will call `NewInferenceServiceLoggerWebhook(reader, scheme)` and its `Handle(context.Context, admission.Request) admission.Response` method.
- The tests will use a controller-runtime fake client containing KServe and TrustyAI API objects.

- [x] **Step 1: Write the failing tests**

Add table-driven tests covering: one RawDeployment or Standard InferenceService plus one same-namespace TrustyAIService receives one `/spec/predictor/logger` add patch with mode `all` and the generated HTTPS logger URL; no TrustyAIService receives no patch; a service in another namespace receives no patch; an existing logger receives no patch; an unsupported deployment mode receives no patch; a missing namespace receives no patch; and a TrustyAIService list failure fails open. Build admission requests with `admissionv1.Create`, a raw serialized `kservev1beta1.InferenceService`, and a deterministic UID.

- [x] **Step 2: Run the focused tests and verify the expected failure**

Run:

```bash
go test ./controllers/tas -run TestInferenceServiceLoggerWebhook -count=1
```

Expected result: compilation fails because the webhook constructor and handler do not yet exist. Fix only test setup errors before proceeding; do not add production code in this step.

### Task 2: Implement the admission handler and register it

**Files:**
- Create: `controllers/tas/inferenceservice_webhook.go`
- Modify: `cmd/main.go`

**Interfaces:**
- Produce `const InferenceServiceLoggerWebhookPath string` for the Kubernetes webhook path.
- Produce `func NewInferenceServiceLoggerWebhook(reader client.Reader, scheme *runtime.Scheme) admission.Handler`.

- [x] **Step 1: Implement the minimal handler**

Decode the request as `kservev1beta1.InferenceService`. Allow non-CREATE operations, existing Logger specifications, and deployment modes other than `RawDeployment` and `Standard`. Resolve the namespace from the object and admission request, reject a mismatch by allowing without a patch, and list active `TrustyAIService` objects in that namespace. When exactly one service exists, generate the same HTTPS URL used by `patchKServe`, construct `LoggerSpec{Mode: "all", URL: &url}`, marshal only that field, and return a single `/spec/predictor/logger` JSON patch. Avoid a full typed-object round-trip so fields unknown to this operator are preserved. When more than one active service exists, allow without mutation to avoid selecting an ambiguous destination; the existing reconciler remains authoritative for that case. Log decode, API read, and marshal failures and allow the request through so KServe is never blocked by an optional TrustyAI integration.

- [x] **Step 2: Register the handler only when TAS is enabled**

In the existing `serviceTAS` webhook setup in `cmd/main.go`, register `&ctrlwebhook.Admission{Handler: controllers/tas.NewInferenceServiceLoggerWebhook(mgr.GetAPIReader(), mgr.GetScheme())}` at `InferenceServiceLoggerWebhookPath` after the TrustyAIService conversion webhook is registered. Keep EvalHub and the TrustyAIService conversion registration unchanged.

- [x] **Step 3: Run the focused tests and verify they pass**

Run:

```bash
gofmt -w controllers/tas/inferenceservice_webhook.go controllers/tas/inferenceservice_webhook_test.go cmd/main.go
go test ./controllers/tas -run TestInferenceServiceLoggerWebhook -count=1
```

Expected result: all webhook behavior tests pass.

### Task 3: Install the webhook and preserve TLS coverage

**Files:**
- Create: `config/components/tas/inferenceservice_logger_webhook.yaml`
- Create: `config/components/webhook/patches/manager_webhook_config.yaml`
- Modify: `config/components/tas/kustomization.yaml`
- Modify: `config/components/evalhub/kustomization.yaml`
- Modify: `config/overlays/evalhub-only/kustomization.yaml`
- Delete: `config/components/evalhub/patches/manager_webhook_config.yaml`
- Synchronize: `trustyai-operator-module/config/manifests-template/` via `hack/sync-trustyai-module-manifests.sh`

**Interfaces:**
- The TAS component will install a `MutatingWebhookConfiguration` named `inference-service-logger-injector.trustyai.opendatahub.io` with path `InferenceServiceLoggerWebhookPath`, `failurePolicy: Ignore`, `timeoutSeconds: 2`, `sideEffects: None`, `matchPolicy: Equivalent`, CREATE-only KServe rules, and OpenShift service-CA injection.
- The shared webhook component will patch the manager Deployment with port 9443 and the `webhook-server-cert` volume/mount.

- [x] **Step 1: Add the static webhook resource**

Reference the resource from the TAS component. Use service `webhook-service` in namespace `system`, port 443, `admissionReviewVersions: ["v1"]`, and a namespace selector requiring `trustyai.opendatahub.io/enabled=true`. The handler’s same-namespace TrustyAIService check remains the authoritative scope gate.

- [x] **Step 2: Move the certificate volume patch to the shared webhook component**

Move the existing manager webhook patch from EvalHub to the shared webhook component so TAS-only overlays also mount the service CA-issued certificate. Keep EvalHub’s existing conversion webhook functional by retaining the shared component in every overlay that enables EvalHub; add that component to `evalhub-only`.

- [x] **Step 3: Render all affected overlays**

Run:

```bash
for overlay in config/overlays/testing config/overlays/odh config/overlays/rhoai config/overlays/odh-kueue config/overlays/evalhub-only; do
  kustomize build "$overlay" >/dev/null || exit 1
done
```

Then verify that each TAS-enabled output contains exactly one manager webhook port/mount and one TrustyAI mutating webhook configuration.

- [x] **Step 4: Synchronize the module manifest template**

Run `./hack/sync-trustyai-module-manifests.sh` and confirm `diff -qr config trustyai-operator-module/config/manifests-template` reports no differences.

### Task 4: Scope the webhook with the TAS namespace label

**Files:**
- Create: `controllers/tas/namespace_label.go`
- Create: `controllers/tas/namespace_label_test.go`
- Modify: `controllers/tas/trustyaiservice_controller.go`
- Modify: `config/components/tas/rbac/manager-rbac.yaml`

- [x] **Step 1: Add failing namespace-label lifecycle tests**

Test that reconciling an active TrustyAIService adds the label, deleting one of multiple services keeps it, and deleting the last service removes it.

- [x] **Step 2: Implement label ownership and cleanup**

Add least-privilege namespace get/update/patch RBAC markers and call the label helper for active CRs and the cleanup helper during finalizer removal. Record ownership with an annotation, preserve an existing label value, and remove only the operator’s `true` value.

- [x] **Step 3: Regenerate and synchronize RBAC manifests**

Run `./hack/extract-component-rbac.sh tas trustyaiservices controllers/tas > config/components/tas/rbac/manager-rbac.yaml` followed by `./hack/sync-trustyai-module-manifests.sh`.

### Task 5: Regression and repository verification

**Files:**
- Modify: `controllers/tas/inferenceservice_webhook_test.go` if additional edge-case assertions are needed.

- [x] **Step 1: Run the complete TAS Go package tests**

Run `go test ./controllers/tas/... -count=1` and resolve only regressions caused by this change.

- [x] **Step 2: Run command package tests and static checks**

Run `go test ./cmd/... -count=1`, `go vet ./controllers/tas/... ./cmd/...`, and `git diff --check`.

- [x] **Step 3: Review the complete diff**

Confirm the diff contains only the webhook handler, tests, registration, manifest installation, TLS component wiring, and this plan. Record test commands and outcomes in the final report; do not commit.

- [x] **Step 4: Close review coverage gaps**

Add regression cases for multiple and deleting TrustyAIService objects, non-CREATE admission operations, malformed input, namespace mismatches, cross-namespace service isolation, field-level patching, and preservation of pre-existing namespace-label values. Rerun the complete `controllers/tas/...` package suite, the focused race tests, full-suite vet, policy checks, and component validation after the additions.

### Plan review result

The plan was reviewed after implementation against the upgrade policy and the live create-time versus late-Logger comparison. The review corrected full-object patch field-loss risk, deleting-service and namespace-mismatch handling, namespace-label ownership, the selector’s false-value behavior, and the webhook Service namespace. The create-time webhook is scoped to CREATE requests, preserves existing logger specifications, fails open, and does not force a rollout for an unchanged Deployment. The plan explicitly retains the existing reconciler for older services and records that late Logger or Batcher topology changes remain outside this operator-only mitigation. No new dependency or canonical `kserve/kserve` change is required for this draft.

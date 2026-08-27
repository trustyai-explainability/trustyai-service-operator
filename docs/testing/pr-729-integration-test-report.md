# PR #729 Integration Test Report

**PR:** [#729 — fix(lmeval): auto-inject cluster CA bundle for HTTPS endpoints; re-run on spec change](https://github.com/trustyai-explainability/trustyai-service-operator/pull/729)
**Date:** 2026-05-11
**Cluster:** OpenShift (`api.sudsinha-latest.e4rv.p3.openshiftapps.com`)
**Method:** Operator run locally via `go run ./cmd/main.go --enable-services LMES` against a remote OpenShift cluster

---

## Bugs Under Test

### Bug 1: Missing CA Bundle for HTTPS Model Endpoints

When an `LMEvalJob` specifies `base_url: https://...` in `modelArgs`, the eval pod gets an `SSLCertVerificationError` because the cluster CA bundle is not mounted. On RHOAI clusters, the CA bundle is available in the `odh-trusted-ca-bundle` ConfigMap (injected by the RHOAI operator into managed namespaces).

**Fix:** The controller now auto-detects HTTPS `base_url` in `modelArgs`, looks up the `odh-trusted-ca-bundle` ConfigMap in the job's namespace, and mounts it as a volume with the `REQUESTS_CA_BUNDLE` environment variable so Python's `requests` library trusts the cluster CA. Injection is skipped when:
- `base_url` uses plain HTTP
- The user has explicitly set `verify_certificate` in `modelArgs` (indicating manual TLS management)
- The CA bundle ConfigMap does not exist in the namespace

### Bug 2: No Re-run After Spec Change

Editing a completed `LMEvalJob`'s spec (e.g. changing `modelArgs`) has no effect — the job stays `Complete` and the user must delete and recreate it.

**Fix:** The controller now records `metadata.Generation` in the annotation `trustyai.opendatahub.io/last-scheduled-generation` when a pod is first scheduled. On each reconcile of a completed job, it compares the current generation against the stored value. If the generation has increased (meaning the spec was edited), the controller resets the job status to `New`, deletes the old pod, and lets the reconcile loop re-create it.

---

## Environment Setup

### Prerequisites

| Component | Detail |
|-----------|--------|
| Go | 1.24.0 (from pre-commit cache) |
| Cluster | OpenShift, logged in via `oc login` |
| Operator | Run locally with `go run ./cmd/main.go --enable-services LMES` |
| Namespace | `test-lmes` (created for testing) |

### Cluster Preparation

1. **Install CRD** directly (the standard `make install` failed because `make manifests` relocated CRD files to `config/components/*/crd/`):
   ```bash
   oc apply -f config/components/lmes/crd/trustyai.opendatahub.io_lmevaljobs.yaml
   ```

2. **Create test namespace:**
   ```bash
   oc create namespace test-lmes
   ```

3. **Create operator ConfigMap** in `default` namespace (locally-run operator resolves namespace to `default` since `/var/run/secrets/.../namespace` doesn't exist):
   ```bash
   oc apply -f - <<EOF
   apiVersion: v1
   kind: ConfigMap
   metadata:
     name: trustyai-service-operator-config
     namespace: default
   data:
     lmes-pod-image: "quay.io/trustyai/ta-lmes-job:latest"
     lmes-driver-image: "quay.io/trustyai/ta-lmes-driver:latest"
     lmes-pod-checking-interval: "10s"
     lmes-image-pull-policy: "Always"
     lmes-max-batch-size: "24"
     lmes-default-batch-size: "8"
     lmes-detect-device: "true"
     lmes-allow-online: "true"
     lmes-allow-code-execution: "true"
     lmes-driver-port: "18080"
   EOF
   ```

4. **Create CA bundle ConfigMap** in `test-lmes` (simulates what RHOAI operator injects):
   ```bash
   oc apply -f - <<EOF
   apiVersion: v1
   kind: ConfigMap
   metadata:
     name: odh-trusted-ca-bundle
     namespace: test-lmes
   data:
     ca-bundle.crt: |
       -----BEGIN CERTIFICATE-----
       MIIDxTCCAq2gAwIBAgIQAqxcJmoLQJuPC3nyrkYldzANBgkqhkiG9w0BAQUFAMDt
       ZXN0IENBMB4XDTA1MDcxNDE3MTk1NVoXDTI1MDcxNDE3MTk1NVowgYsxCzAJBg==
       -----END CERTIFICATE-----
   EOF
   ```

5. **Temporary patch to `cmd/main.go`** — added a namespace fallback for local execution (reverted after testing, not committed):
   ```go
   ns, err := utils.GetNamespace()
   if err != nil {
       setupLog.Error(err, "unable to operator's namespace")
       ns = "default"
       setupLog.Info("falling back to namespace", "namespace", ns)
   }
   ```

### Workarounds

- **Pre-existing metrics bug:** A label cardinality mismatch in `controllers/metrics/metrics.go:86` (`expected 5 label values but got 4`) crashes the operator on the second `LMEvalJob` creation in the same session. This is a pre-existing bug on `main`, unrelated to PR #729. Workaround: restart the operator between test cases.
- **Finalizer hangs on delete:** When the operator crashes (due to the metrics bug), `oc delete lmevaljob` hangs because finalizers cannot be removed. Workaround: `oc patch lmevaljob <name> --type=merge -p '{"metadata":{"finalizers":[]}}'` before deleting.

---

## Phase 1: Reproduce Bugs (from `main` branch)

Operator started from `main` branch to confirm both bugs exist.

### Test 1a: HTTPS Job — No CA Bundle Injection

**LMEvalJob applied:**
```yaml
apiVersion: trustyai.opendatahub.io/v1alpha1
kind: LMEvalJob
metadata:
  name: test-ca-bug
  namespace: test-lmes
spec:
  model: hf
  modelArgs:
    - name: pretrained
      value: google/flan-t5-base
    - name: base_url
      value: "https://model.example.com"
  taskList:
    taskNames: [task1]
  allowOnline: true
  allowCodeExecution: true
```

**Verification commands:**
```bash
# Check pod volumes
oc get pod test-ca-bug -n test-lmes -o jsonpath='{.spec.volumes[*].name}'

# Check env vars
oc get pod test-ca-bug -n test-lmes -o jsonpath='{.spec.containers[0].env[*].name}'
```

**Result:** No `odh-ca-bundle` volume. No `REQUESTS_CA_BUNDLE` env var. **Bug reproduced.**

### Test 1b: Completed Job — No Re-run After Spec Change

**LMEvalJob applied:**
```yaml
apiVersion: trustyai.opendatahub.io/v1alpha1
kind: LMEvalJob
metadata:
  name: test-rerun-bug
  namespace: test-lmes
spec:
  model: hf
  modelArgs:
    - name: pretrained
      value: google/flan-t5-base
  taskList:
    taskNames: [task1]
  allowOnline: true
  allowCodeExecution: true
```

**Steps:**
1. Wait for job to reach `Scheduled` state
2. Manually mark as `Complete` via status subresource patch:
   ```bash
   oc patch lmevaljob test-rerun-bug -n test-lmes --type=merge --subresource=status \
     -p '{"status":{"state":"Complete","reason":"Succeeded"}}'
   ```
3. Edit spec to bump `metadata.Generation`:
   ```bash
   oc patch lmevaljob test-rerun-bug -n test-lmes --type=merge \
     -p '{"spec":{"modelArgs":[{"name":"pretrained","value":"google/flan-t5-small"}]}}'
   ```
4. Check status after 3 seconds

**Result:** Status remains `Complete`. No re-run triggered. **Bug reproduced.**

---

## Phase 2: Verify Fixes (from PR branch `fix/lmeval-https-ssl-rerun`)

Operator started from the PR branch. Required an additional build fix: `controllers/job_mgr/job_mgr_controller.go` had not been updated for the new `CreatePod` signature (added `caBundle *corev1.ConfigMap, caBundleKey string` parameters). Fixed by passing `nil, ""` — committed as a separate fix.

### Test 2a: HTTPS Job — CA Bundle IS Injected

**Same LMEvalJob spec as Test 1a** (name: `test-ca-fix`).

**Verification:**
```bash
# Volume present and sourced from correct ConfigMap
oc get pod test-ca-fix -n test-lmes \
  -o jsonpath='{.spec.volumes[?(@.name=="odh-ca-bundle")].configMap.name}'
# → odh-trusted-ca-bundle

# Volume mount at correct path
oc get pod test-ca-fix -n test-lmes \
  -o jsonpath='{.spec.containers[0].volumeMounts[?(@.name=="odh-ca-bundle")].mountPath}'
# → /etc/ssl/certs/odh-ca-bundle.crt

# REQUESTS_CA_BUNDLE env var set
oc get pod test-ca-fix -n test-lmes \
  -o jsonpath='{.spec.containers[0].env[?(@.name=="REQUESTS_CA_BUNDLE")].value}'
# → /etc/ssl/certs/odh-ca-bundle.crt
```

**Result:** Volume `odh-ca-bundle` present, mount path correct, `REQUESTS_CA_BUNDLE` set. **Fix verified.**

### Test 2b: HTTP Job — No CA Bundle (Negative)

**LMEvalJob with `base_url: http://...`** (name: `test-ca-http`).

**Result:** No `odh-ca-bundle` volume, no `REQUESTS_CA_BUNDLE` env var. **Correct behavior — no injection for plain HTTP.**

### Test 2c: HTTPS + `verify_certificate` — No CA Bundle (Negative)

**LMEvalJob with `base_url: https://...` AND `verify_certificate: "false"`** (name: `test-ca-verify`).

**Result:** No `odh-ca-bundle` volume, no `REQUESTS_CA_BUNDLE` env var. **Correct behavior — user explicitly manages TLS.**

### Test 2d: Completed Job — Re-run After Spec Change

**Same flow as Test 1b** (name: `test-rerun-fix`).

**Additional verification — annotation set:**
```bash
oc get lmevaljob test-rerun-fix -n test-lmes \
  -o jsonpath='{.metadata.annotations.trustyai\.opendatahub\.io/last-scheduled-generation}'
# → 1
```

**After marking Complete and editing spec:**

**Result:** Status reset from `Complete` → `New` → `Scheduled`. A new pod was created. **Fix verified.**

### Test 2e: Completed Job — No Spurious Re-run When Spec Unchanged (Negative)

**LMEvalJob created, waited for `Scheduled`, marked `Complete` without editing spec** (name: `test-no-rerun`).

**Result:** Status stays `Complete` after 5 seconds. **Correct behavior — no spurious re-run.**

---

## Results Summary

| # | Test Case | Branch | Expected | Actual | Status |
|---|-----------|--------|----------|--------|--------|
| 1a | HTTPS job pod has CA volume/mount/env | `main` | NO | NO | Bug reproduced |
| 1b | Completed job resets on spec change | `main` | NO | NO | Bug reproduced |
| 2a | HTTPS job pod has CA volume/mount/env | PR | YES | YES | Fix verified |
| 2b | HTTP job pod has CA volume/mount/env | PR | NO | NO | Correct |
| 2c | HTTPS + verify_certificate has CA | PR | NO | NO | Correct |
| 2d | Completed job resets on spec change | PR | YES | YES | Fix verified |
| 2e | Completed job stays Complete when unchanged | PR | YES | YES | Correct |
| — | Generation annotation set on Scheduled | PR | YES | YES | Correct |

**All tests passed.** Both bugs were reproduced on `main` and confirmed fixed on the PR branch.

---

## Additional Finding: Build Break in `job_mgr_controller.go`

During Phase 2, the operator failed to build from the PR branch because `controllers/job_mgr/job_mgr_controller.go:123` was not updated for the new `CreatePod` function signature. The call was missing the `caBundle *corev1.ConfigMap` and `caBundleKey string` parameters. This was fixed by passing `nil, ""` (the job manager doesn't perform CA bundle injection) and committed as:

```
fix: update job_mgr CreatePod call for new CA bundle parameters
```

## Pre-existing Issue: Metrics Label Cardinality Panic

A pre-existing bug in `controllers/metrics/metrics.go:86` causes a panic on the second `LMEvalJob` creation in the same operator session:

```
inconsistent label cardinality: expected 5 label values but got 4
```

This is present on both `main` and the PR branch and is unrelated to the changes in PR #729. It was worked around by restarting the operator between test cases.

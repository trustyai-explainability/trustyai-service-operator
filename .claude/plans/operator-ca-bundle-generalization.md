# Operator: Generalize LMEval CA Bundle Injection

## Context

PR [#729](https://github.com/trustyai-explainability/trustyai-service-operator/pull/729) (merged) fixed RHOAIENG-60487 by auto-injecting cluster CA bundles into LMEval pods **conditionally** — only when `base_url` uses `https://` and `verify_certificate` is absent. [RHOAIENG-60453](https://redhat.atlassian.net/browse/RHOAIENG-60453) (status: New) identifies the same root cause but proposes a more general fix: always mount `odh-trusted-ca-bundle` unconditionally, matching the pattern already used by TrustyAI Service and NemoGuardrails controllers.

This document specifies how to replace PR #729's CA injection logic with the general approach while preserving its independent fixes (re-run support, metrics label cardinality).

## What to keep from PR #729

These changes are independent of the CA injection approach and must be preserved:

1. **Re-run after spec change** (`Reconcile`, `recordScheduledGeneration`, `getLastScheduledGeneration`, `LastScheduledGenerationAnnotation`): detects spec edits on completed jobs, resets status, creates new pod.
2. **Metrics label cardinality fix** (`createJobCreationMetrics`): always initializes `model_name` and `base_url` labels to empty strings.
3. **RBAC**: `create;update` verbs for ConfigMaps (still needed if we create any ConfigMap, but can be dropped if we mount existing ones directly).

## What to remove from PR #729

All conditional CA injection logic:

| Symbol | File | Why remove |
|---|---|---|
| `resolveCABundle()` | `lmevaljob_controller.go` | Replaced by unconditional mount |
| `findAndMergeCABundle()` | `lmevaljob_controller.go` | No per-job merged ConfigMap needed |
| `hasHTTPSBaseURL()` | `lmevaljob_controller.go` | No scheme-sniffing needed |
| `hasExplicitVerifyCertificate()` | `lmevaljob_controller.go` | No verify_certificate gating needed |
| `errNoCAData` | `lmevaljob_controller.go` | Sentinel for findAndMergeCABundle |
| `MergedCAConfigMapSuffix` | `constants.go` | No per-job ConfigMap |
| `MergedCABundleKey` | `constants.go` | No per-job ConfigMap |
| `ServiceCAConfigMapName` | `constants.go` | Not mounting openshift-service-ca.crt separately |
| `ServiceCAKey` | `constants.go` | Not mounting openshift-service-ca.crt separately |
| CA bundle params on `CreatePod()` | `lmevaljob_controller.go` | Revert signature to no CA params |
| `caBundle`/`caBundleKey` plumbing in `handleNewCR`, `handleResume` | `lmevaljob_controller.go` | Remove resolveCABundle calls |
| RBAC `create;update` for ConfigMaps | `manager-rbac.yaml` | No longer creating ConfigMaps (revert to `get;watch;list`) |

Also remove from tests:
- `Test_hasHTTPSBaseURL`, `Test_hasExplicitVerifyCertificate` (unit tests for removed functions)
- `Test_CreatePodWithCABundle`, `Test_CreatePodWithoutCABundle` (unit tests for removed CreatePod params)
- `Describe("LMEvalJob CA bundle injection", ...)` suite tests (integration tests for removed behavior)
- The `"updates the merged CA ConfigMap when an HTTPS job is re-run"` integration test

Keep the re-run integration tests (`Describe("LMEvalJob re-run after spec change", ...)`).

## What to add: unconditional CA mount

### Design

In `CreatePod()`, unconditionally add:

1. A **Volume** referencing `odh-trusted-ca-bundle` with `Optional: true`:
   ```go
   corev1.Volume{
       Name: "odh-trusted-ca",
       VolumeSource: corev1.VolumeSource{
           ConfigMap: &corev1.ConfigMapVolumeSource{
               LocalObjectReference: corev1.LocalObjectReference{
                   Name: "odh-trusted-ca-bundle",
               },
               Optional: pointer.Bool(true),
           },
       },
   }
   ```

2. A **VolumeMount** on the main container:
   ```go
   corev1.VolumeMount{
       Name:      "odh-trusted-ca",
       MountPath: "/etc/pki/tls/custom-certs/ca-bundle.crt",
       SubPath:   "ca-bundle.crt",
       ReadOnly:  true,
   }
   ```

3. **Environment variables** — but only when `odh-trusted-ca-bundle` exists. At reconcile time (`handleNewCR` and `handleResume`), check:
   ```go
   odhCM := &corev1.ConfigMap{}
   err := r.Get(ctx, types.NamespacedName{
       Namespace: job.Namespace,
       Name:      DefaultCABundleConfigMapName,
   }, odhCM)
   caBundleExists := err == nil
   ```
   Then pass `caBundleExists` to `CreatePod()`. If true, add:
   ```go
   corev1.EnvVar{Name: "SSL_CERT_FILE", Value: "/etc/pki/tls/custom-certs/ca-bundle.crt"},
   corev1.EnvVar{Name: "REQUESTS_CA_BUNDLE", Value: "/etc/pki/tls/custom-certs/ca-bundle.crt"},
   ```
   If false, skip the env vars. The volume + mount are always present (Optional: true handles missing ConfigMap gracefully), but without the env vars, Python uses its default trust store — no regression.

### Why this design

- **Volume is always present**: `Optional: true` means the pod starts even if the ConfigMap doesn't exist. The mount path is simply empty.
- **Env vars are conditional on ConfigMap existence**: `REQUESTS_CA_BUNDLE` replaces Python's default trust store entirely. Pointing it to a non-existent file breaks all HTTPS. So we only set it when we know the ConfigMap (and thus the mounted file) exists.
- **Both `SSL_CERT_FILE` and `REQUESTS_CA_BUNDLE`**: `REQUESTS_CA_BUNDLE` covers `requests` library. `SSL_CERT_FILE` covers Python's `ssl` module (used by `urllib3`, `httpx`, `aiohttp`, etc.). PR #729 only set `REQUESTS_CA_BUNDLE`.
- **No scheme-sniffing**: the trust store is available to all HTTPS connections from the pod, not just the model endpoint. This also helps HuggingFace downloads, dataset fetches, and any other outbound HTTPS call that might need the cluster CA.
- **No per-job ConfigMap**: eliminates lifecycle management (owner references, GC, create/update RBAC).

### What about `openshift-service-ca.crt`?

PR #729 merged the service-serving CA (`openshift-service-ca.crt` ConfigMap, `service-ca.crt` key) for `*.svc.cluster.local` HTTPS services. The general approach drops this.

This is acceptable because:
- `odh-trusted-ca-bundle` with the OpenShift injection label (`config.openshift.io/inject-trusted-cabundle: "true"`) includes the cluster CA that signs external routes AND the service-serving CA. The RHOAI operator creates this ConfigMap with that label.
- In the rare case where the service-serving CA is NOT in `odh-trusted-ca-bundle`, the user can set `verify_certificate: False` in modelArgs (this is a Python-level lm-eval setting, unrelated to the trust store mount).

### Constants to keep/update in `constants.go`

```go
// Keep (unchanged):
DefaultCABundleConfigMapName = "odh-trusted-ca-bundle"
CABundleVolumeName           = "odh-trusted-ca"          // renamed from "odh-ca-bundle"
LastScheduledGenerationAnnotation = "trustyai.opendatahub.io/last-scheduled-generation"

// Add:
CABundleMountPath = "/etc/pki/tls/custom-certs/ca-bundle.crt"
CABundleKey       = "ca-bundle.crt"

// Remove:
// MergedCAConfigMapSuffix, MergedCABundleKey, ServiceCAConfigMapName, ServiceCAKey
```

### `CreatePod()` signature change

```go
// PR #729 (current):
func CreatePod(svcOpts *serviceOptions, job *lmesv1alpha1.LMEvalJob,
    permConfig *PermissionConfig, caBundle *corev1.ConfigMap, caBundleKey string,
    log logr.Logger) *corev1.Pod

// Generalized:
func CreatePod(svcOpts *serviceOptions, job *lmesv1alpha1.LMEvalJob,
    permConfig *PermissionConfig, caBundleExists bool,
    log logr.Logger) *corev1.Pod
```

All call sites (`handleNewCR`, `handleResume`, `job_mgr_controller.go:PodSets`) update accordingly. `PodSets` passes `false` (conservative default for Kueue integration).

## Test plan for the operator repo

### Unit tests (`lmevaljob_controller_test.go`)

| Test | What it verifies |
|---|---|
| `Test_CreatePodWithCABundle` (rewrite) | When `caBundleExists=true`: pod has `odh-trusted-ca` volume with Optional:true, mount at `/etc/pki/tls/custom-certs/ca-bundle.crt` with SubPath `ca-bundle.crt`, both `SSL_CERT_FILE` and `REQUESTS_CA_BUNDLE` env vars |
| `Test_CreatePodWithoutCABundle` (rewrite) | When `caBundleExists=false`: pod has `odh-trusted-ca` volume with Optional:true and mount (volume is always present), but NO `SSL_CERT_FILE` or `REQUESTS_CA_BUNDLE` env vars |
| All existing `Test_Simple/Custom/Secrets/...Pod` | Update `CreatePod` calls to pass `false` for `caBundleExists` |

### Integration tests (`lmevaljob_controller_suite_test.go`)

| Test | What it verifies |
|---|---|
| **CA env vars when ConfigMap exists** | Create `odh-trusted-ca-bundle` ConfigMap, then create LMEvalJob. Pod should have both `SSL_CERT_FILE` and `REQUESTS_CA_BUNDLE` set, volume present. |
| **No CA env vars when ConfigMap absent** | Create LMEvalJob without `odh-trusted-ca-bundle` in namespace. Pod should have volume (Optional:true) but NO `SSL_CERT_FILE` or `REQUESTS_CA_BUNDLE`. |
| **Works for HTTP base_url too** | Create `odh-trusted-ca-bundle`, create job with `http://` base_url. Pod still gets CA volume and env vars (unconditional). |
| Re-run tests | Keep existing re-run tests unchanged. |

### E2E tests (opendatahub-tests repo — adjust current changes)

The current tests in opendatahub-tests will need to be adjusted to match the general approach:

- **Keep**: `test_lmeval_vllm_emulator_https_ca_bundle` — validates the fix works end-to-end for the original bug. Update `validate_ca_bundle_injected` to check for `SSL_CERT_FILE` in addition to `REQUESTS_CA_BUNDLE`, and update volume/mount path constants.
- **Keep**: `test_lmeval_rerun_after_spec_change` — independent of CA approach.
- **Remove**: `test_lmeval_vllm_emulator_http_no_ca_bundle` — CA injection is no longer scheme-dependent; HTTP jobs also get the CA mount.
- **Remove**: `test_lmeval_https_verify_certificate_no_ca_bundle` — no longer relevant since injection isn't gated on verify_certificate.
- **Add**: `test_lmeval_vllm_emulator_http_has_ca_bundle` — verify that even HTTP jobs get the CA volume and env vars when `odh-trusted-ca-bundle` exists. This is the positive counterpart to the removed negative test.

### Constants to update in opendatahub-tests

```python
# Update:
CA_BUNDLE_VOLUME_NAME = "odh-trusted-ca"
CA_BUNDLE_MOUNT_PATH = "/etc/pki/tls/custom-certs/ca-bundle.crt"
CA_BUNDLE_KEY = "ca-bundle.crt"  # renamed from MERGED_CA_BUNDLE_KEY

# Remove:
# MERGED_CA_CONFIGMAP_SUFFIX (no per-job ConfigMap)
```

## Sequencing

1. **Operator PR** (trustyai-service-operator): implement the changes above.
2. **Test PR** (opendatahub-tests): adjust current changes to match.
3. **Jira**: close RHOAIENG-60453 as resolved by the operator PR. Update RHOAIENG-60487 if needed.

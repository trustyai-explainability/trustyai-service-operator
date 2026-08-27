# RHOAIENG-60487: SSL Certificate Verification Failure Investigation

**Jira**: [RHOAIENG-60487](https://redhat.atlassian.net/browse/RHOAIENG-60487)
**Date**: 2026-06-24
**Author**: Sudip Sinha

## Summary

LMEvalJob pods fail with `ssl.SSLCertVerificationError` when the model is exposed via an external HTTPS route on clusters that use a non-public CA (self-signed or internal) for the OpenShift ingress. The same error affects EvalHub adapter pods. PR #729 added conditional CA bundle injection for LMEvalJob, but users report the error persists even with the fix applied.

## Timeline

| Date | Event |
|------|-------|
| 2025-12-08 | First report from Aaruni Aggarwal on RHOAI 3.0 (Slack) |
| 2026-01-09 | PR #729 merged (conditional CA injection for LMEvalJob) |
| 2026-01-17 | Build with fix released |
| 2026-01-30 | Aaruni reports same error on RHOAI 3.3 (post-fix build) |
| 2026-02-04 | Second user (aymk) reports same error on RHOAI 3.2 |
| 2026-04-09 | Aaruni reports same error on RHOAI 3.4-ea2 |
| 2026-04-29 | Aaruni tests re-run workaround (patching status to "New") - doesn't work |
| 2026-06-08 | Same error reported via EvalHub adapter pods (different code path) |
| 2026-06-19 | Latest report - PR #729 fix IS applied (confirmed via pod describe), but SSL still fails |

## Root Cause Analysis

### The original bug

When a model is deployed with "Make model deployment available through an external route", OpenShift creates an HTTPS route using the cluster's ingress certificate. On-premise and IBM Cloud clusters typically use a self-signed or internal CA for this. The lm-eval pod has no cluster CA bundle, so Python's `ssl` module rejects the certificate.

### What PR #729 does

PR #729 added conditional CA bundle injection to the LMEvalJob controller:
1. Checks if `base_url` in modelArgs starts with `https://`
2. Checks if `verify_certificate` is NOT explicitly set
3. If both conditions met, reads `odh-trusted-ca-bundle` ConfigMap from the job's namespace
4. Optionally merges with `openshift-service-ca.crt`
5. Creates a per-job merged ConfigMap (e.g., `evaljob-vllm-ca-bundle`)
6. Mounts it and sets `REQUESTS_CA_BUNDLE` env var

### Why the error persists despite the fix being applied

From the June 19 pod describe, the fix IS working mechanically:

```
Environment:
  REQUESTS_CA_BUNDLE:  /etc/ssl/certs/odh-ca-bundle.crt
Mounts:
  /etc/ssl/certs/odh-ca-bundle.crt from odh-ca-bundle (ro,path="merged-ca-bundle.crt")
Volumes:
  odh-ca-bundle:
    Type:      ConfigMap
    Name:      evaljob-vllm-ca-bundle
    Optional:  false
```

The CA volume is mounted. `REQUESTS_CA_BUNDLE` is set. The pod starts. But SSL verification still fails. This means:

**The merged CA bundle does not contain the certificate that signed the ingress route.**

Possible reasons:
1. **`odh-trusted-ca-bundle` doesn't contain the ingress CA** - The `config.openshift.io/inject-trusted-cabundle` annotation injects the proxy/additional trust bundle, which may not include the ingress controller's signing CA if it was configured separately from the cluster proxy config.
2. **The ConfigMap data key is wrong** - The ConfigMap has `DATA: 2` entries. If the merge logic selects the wrong key, the mounted file could contain incomplete data.
3. **The ConfigMap is empty or has placeholder data** - The ConfigMap exists but the OpenShift config operator hasn't populated it (race condition, or the cluster proxy config doesn't have a `trustedCA` configured).

### Pending verification

We asked the reporter to run diagnostic commands but haven't received results yet:

```bash
# Check merged ConfigMap contents
oc get configmap evaljob-vllm-ca-bundle -n gpt-project -o jsonpath='{.data.merged-ca-bundle\.crt}'

# Check route cert issuer
echo | openssl s_client -connect gpt2-gpt-project.apps.ocpz-standard-1.a314lp49.lnxero1.boe:443 2>/dev/null | openssl x509 -noout -issuer

# Check source ConfigMap keys and sizes
oc get configmap odh-trusted-ca-bundle -n gpt-project -o json | python3 -c "import sys,json; d=json.load(sys.stdin)['data']; [print(f'{k}: {len(v)} bytes') for k,v in d.items()]"

# Test if the source CA can verify the route
curl --max-time 5 -v --cacert <(oc get configmap odh-trusted-ca-bundle -n gpt-project -o jsonpath='{.data.ca-bundle\.crt}') https://gpt2-gpt-project.apps.ocpz-standard-1.a314lp49.lnxero1.boe/v1/completions 2>&1
```

## Additional Issue: EvalHub Adapter Pods

EvalHub evaluation jobs exhibit the same SSL error but through a different code path.

**Key difference**: EvalHub adapter pods are created by the **eval-hub service** (external microservice), not by the trustyai-service-operator. The operator only watches these jobs. Therefore, PR #729's fix does not apply.

Pod evidence (June 8 report):
- Containers: `adapter` (main) + `sidecar` (init) - EvalHub pattern, not LMEvalJob
- Volume `evalhub-service-ca` mounted (for internal cluster services) but no `odh-trusted-ca-bundle`
- No `SSL_CERT_FILE` or `REQUESTS_CA_BUNDLE` env vars on the adapter container
- Sidecar successfully uses service CA for internal communication to `evalhub.redhat-ods-applications.svc.cluster.local`

Fix requires changes in the **eval-hub repository** (`internal/eval_hub/runtimes/k8s/job_builders.go`) to mount `odh-trusted-ca-bundle` and set CA env vars on adapter pods.

## Additional Issue: Re-run After Spec Change

Users cannot fix a failed job by editing the spec (e.g., adding `verify_certificate: "False"`). The operator ignores spec changes on completed jobs. Workarounds like `oc patch lmevaljob --type=merge -p '{"status": {"state": "New"}}'` also don't work because the status subresource requires a status update, not a regular patch.

PR #729 includes a fix for this (generation annotation tracking), but it wasn't present in earlier releases the users tested.

## Reproduction

**Cannot reproduce on ROSA/AWS clusters** - These use Let's Encrypt certificates for ingress routes, which are publicly trusted. Python verifies them without a custom CA bundle.

**Requires** a cluster with a self-signed or internal CA for the ingress controller (typical of on-prem, IBM Cloud, or clusters with custom ingress certificates).

**Alternative**: Deploy a service with a custom self-signed certificate behind a passthrough route on any cluster.

## Generalized Fix (In Progress)

Branch `fix/lmeval-https-ssl-rerun` implements an unconditional CA bundle injection approach, replacing PR #729's conditional logic:

| Aspect | PR #729 (Conditional) | Generalized (New) |
|--------|----------------------|-------------------|
| Volume mount | Only when `base_url` is HTTPS | Always (with `Optional: true`) |
| Env vars | `REQUESTS_CA_BUNDLE` only | Both `SSL_CERT_FILE` and `REQUESTS_CA_BUNDLE` |
| Env var condition | HTTPS scheme + no explicit verify_certificate | ConfigMap exists in namespace |
| CA source | Per-job merged ConfigMap (from odh-trusted-ca-bundle + service CA) | Direct mount of odh-trusted-ca-bundle |
| RBAC | ConfigMap create/update needed | Read-only (get/watch/list) |
| Scope | Only model endpoint calls | All HTTPS from the pod |

All operator tests pass on this branch.

### Why the generalized fix alone may not resolve the reporter's issue

If `odh-trusted-ca-bundle` doesn't contain the ingress CA, neither approach works. The fix depends on the ConfigMap having the right certificates. The real question is: **does `odh-trusted-ca-bundle` with the `config.openshift.io/inject-trusted-cabundle` annotation include the ingress CA on these clusters?**

## Recommendations

### Immediate (unblock users)

1. **Get diagnostic results** from the reporter to confirm whether the CA content is the issue.

   Run these on the affected cluster (replace `NAMESPACE`, `JOB_NAME`, `ROUTE_HOST` with actual values):

   ```bash
   # What's in the merged ConfigMap that the pod actually uses?
   oc get configmap <JOB_NAME>-ca-bundle -n <NAMESPACE> -o jsonpath='{.data.merged-ca-bundle\.crt}'

   # What CA signed the route's certificate?
   echo | openssl s_client -connect <ROUTE_HOST>:443 2>/dev/null | openssl x509 -noout -issuer

   # What keys exist in the source ConfigMap and how large are they?
   oc get configmap odh-trusted-ca-bundle -n <NAMESPACE> -o json | python3 -c "import sys,json; d=json.load(sys.stdin)['data']; [print(f'{k}: {len(v)} bytes') for k,v in d.items()]"

   # Can the source CA bundle actually verify the route?
   curl --max-time 5 -v --cacert <(oc get configmap odh-trusted-ca-bundle -n <NAMESPACE> -o jsonpath='{.data.ca-bundle\.crt}') https://<ROUTE_HOST>/v1/completions 2>&1
   ```

   For the June 19 reporter specifically:

   ```bash
   oc get configmap evaljob-vllm-ca-bundle -n gpt-project -o jsonpath='{.data.merged-ca-bundle\.crt}'

   echo | openssl s_client -connect gpt2-gpt-project.apps.ocpz-standard-1.a314lp49.lnxero1.boe:443 2>/dev/null | openssl x509 -noout -issuer

   oc get configmap odh-trusted-ca-bundle -n gpt-project -o json | python3 -c "import sys,json; d=json.load(sys.stdin)['data']; [print(f'{k}: {len(v)} bytes') for k,v in d.items()]"

   curl --max-time 5 -v --cacert <(oc get configmap odh-trusted-ca-bundle -n gpt-project -o jsonpath='{.data.ca-bundle\.crt}') https://gpt2-gpt-project.apps.ocpz-standard-1.a314lp49.lnxero1.boe/v1/completions 2>&1
   ```

2. **Document the workaround**: add `verify_certificate: "False"` to modelArgs when using external routes with self-signed certs (this bypasses SSL verification entirely).

   ```yaml
   spec:
     modelArgs:
       - name: verify_certificate
         value: "False"
       # ... other args
   ```

3. **Request UI change**: add a `verify_certificate` option to the RHOAI Dashboard evaluation form (users currently cannot set this from the UI).

### Short-term (operator fix)

1. **Merge the generalized CA injection** (branch `fix/lmeval-https-ssl-rerun`) - cleaner approach, covers more HTTPS use cases, sets both `SSL_CERT_FILE` and `REQUESTS_CA_BUNDLE`.

2. **Add operator logging** when the CA ConfigMap is missing or empty, so the root cause is visible in operator logs.

   Verify the fix is applied correctly on a cluster with self-signed ingress:

   ```bash
   # Confirm the operator image contains the fix
   oc get deployment trustyai-service-operator-controller-manager -n redhat-ods-applications \
     -o jsonpath='{.spec.template.spec.containers[0].image}'

   # After creating an LMEvalJob, verify the pod has CA injection
   oc get pod <JOB_NAME> -n <NAMESPACE> -o jsonpath='{.spec.containers[0].env}' | python3 -c "import sys,json; evs=json.load(sys.stdin); [print(e['name'],e.get('value','<from-ref>')) for e in evs if e['name'] in ('SSL_CERT_FILE','REQUESTS_CA_BUNDLE')]"

   # Verify the volume is present
   oc get pod <JOB_NAME> -n <NAMESPACE> -o jsonpath='{.spec.volumes}' | python3 -c "import sys,json; vols=json.load(sys.stdin); [print(v['name'],v.get('configMap',{}).get('name','')) for v in vols if v['name']=='odh-trusted-ca']"
   ```

### Medium-term (eval-hub fix)

1. **Update eval-hub job builder** to mount `odh-trusted-ca-bundle` and set CA env vars on adapter pods.

2. **Operator**: ensure `odh-trusted-ca-bundle` exists in tenant namespaces (with injection annotation).

   Verify EvalHub pods have CA injection after the fix:

   ```bash
   # Find the EvalHub evaluation pod
   oc get pods -n <NAMESPACE> -l app=evalhub,component=evaluation-job

   # Check if CA volume and env vars are present on the adapter container
   oc get pod <POD_NAME> -n <NAMESPACE> -o jsonpath='{.spec.containers[?(@.name=="adapter")].env}' | python3 -c "import sys,json; evs=json.load(sys.stdin); [print(e['name'],e.get('value','<from-ref>')) for e in evs if e['name'] in ('SSL_CERT_FILE','REQUESTS_CA_BUNDLE')]"

   # Check odh-trusted-ca-bundle exists in the tenant namespace
   oc get configmap odh-trusted-ca-bundle -n <NAMESPACE> -o jsonpath='{.metadata.annotations}'
   ```

### Investigation needed

1. **Confirm CA content** - verify whether `odh-trusted-ca-bundle` actually contains the ingress CA on the affected clusters (IBM Cloud, on-prem).

   ```bash
   # List all certificates in the bundle by subject
   oc get configmap odh-trusted-ca-bundle -n <NAMESPACE> -o jsonpath='{.data.ca-bundle\.crt}' | \
     openssl crl2pkcs7 -nocrl -certfile /dev/stdin | \
     openssl pkcs7 -print_certs -noout -text 2>/dev/null | grep "Subject:"

   # Compare against the route's certificate issuer
   echo | openssl s_client -connect <ROUTE_HOST>:443 2>/dev/null | openssl x509 -noout -issuer

   # Check the cluster proxy config for trustedCA
   oc get proxy cluster -o jsonpath='{.spec.trustedCA.name}'

   # If a trustedCA ConfigMap is configured, check its contents
   oc get configmap <TRUSTED_CA_NAME> -n openshift-config -o json | python3 -c "import sys,json; d=json.load(sys.stdin)['data']; [print(f'{k}: {len(v)} bytes') for k,v in d.items()]"
   ```

2. **If `odh-trusted-ca-bundle` doesn't contain the ingress CA**, determine if the cluster proxy config has `trustedCA` set, or if the ingress CA needs to be added manually.

   ```bash
   # Check the ingress controller's default certificate
   oc get ingresscontroller default -n openshift-ingress-operator -o jsonpath='{.spec.defaultCertificate.name}'

   # If a custom cert is set, extract its CA
   oc get secret <CERT_SECRET_NAME> -n openshift-ingress -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -noout -issuer

   # To manually add the ingress CA to the proxy trust bundle:
   # 1. Get the ingress CA
   # 2. Add it to a ConfigMap in openshift-config
   # 3. Reference it in the proxy config:
   oc get proxy cluster -o yaml
   # spec.trustedCA.name should point to a ConfigMap in openshift-config
   # that contains the ingress CA in its ca-bundle.crt key
   ```

3. **If `odh-trusted-ca-bundle` does contain the ingress CA**, investigate why the merged ConfigMap still fails.

   ```bash
   # Compare the source and merged ConfigMap sizes
   oc get configmap odh-trusted-ca-bundle -n <NAMESPACE> -o json | python3 -c "import sys,json; d=json.load(sys.stdin)['data']; [print(f'{k}: {len(v)} bytes') for k,v in d.items()]"
   oc get configmap <JOB_NAME>-ca-bundle -n <NAMESPACE> -o json | python3 -c "import sys,json; d=json.load(sys.stdin)['data']; [print(f'{k}: {len(v)} bytes') for k,v in d.items()]"

   # Count certificates in each bundle
   oc get configmap odh-trusted-ca-bundle -n <NAMESPACE> -o jsonpath='{.data.ca-bundle\.crt}' | grep -c 'BEGIN CERTIFICATE'
   oc get configmap <JOB_NAME>-ca-bundle -n <NAMESPACE> -o jsonpath='{.data.merged-ca-bundle\.crt}' | grep -c 'BEGIN CERTIFICATE'

   # Check the operator logs for errors during CA bundle merge
   oc logs deployment/trustyai-service-operator-controller-manager -n redhat-ods-applications | grep -i -E 'ca\.bundle|ca-bundle|resolveCA|mergeCA|certificate'
   ```

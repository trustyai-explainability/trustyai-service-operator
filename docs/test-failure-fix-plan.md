# TrustyAI Operator Bug Fix Plan

**Date:** 2026-08-18  
**Reference:** [TrustyAI Test Failures Detailed Report](../../opendatahub-tests/docs/trustyai_test_failures_detailed_report.md)

## Executive Summary

This plan addresses operator bugs causing 11 test failures (14% of test suite) in TrustyAI Service integration tests. The failures stem from two critical operator configuration issues:

1. **Route Misconfiguration** (P0) - 10 test failures
2. **CRD Conversion Webhook Missing** (P1) - 1 test failure

**Expected Impact:**
- Current pass rate: 18% (14/78 tests)
- After fixes: 32% (25/78 tests) → 58-64% with dependency resolution

## Critical Findings from Test Report

### Issue #1: Route Points to Wrong Service (P0 - CRITICAL)

**Affected Test Clusters:** 2, 3, 5 (10 failures total)

**Root Cause:**
Routes created by operator point to `trustyai-service-tls` (port 8443, kube-rbac-proxy only) instead of `trustyai-service` (port 4443, API endpoints).

**Evidence:**
```bash
# Current (BROKEN):
$ oc get route trustyai-service -o jsonpath='{.spec.to.name}'
trustyai-service-tls  # ← Port 8443, NO API endpoints

$ curl -k https://<route>/info
{"detail":"Not Found"}  # ← 404 error

# Internal service call (WORKS):
$ curl https://trustyai-service.svc.cluster.local/info
{"gaussian-credit-model": {...}}  # ← Valid response
```

**Code Location:**
```go
// controllers/tas/route.go:21 (CURRENT - WRONG)
routeConfig := utils.RouteConfig{
    ServiceName: instance.Name + "-tls",  // ← Points to wrong service
    PortName:    KubeRBACProxyServicePortName,
}
```

**Impact:**
- Internal observability works (predictor → TrustyAI via cluster DNS)
- External API completely broken (all route-based access returns 404)
- Affects ALL TrustyAI deployments via operator

**Test Failures:**
- Cluster 2: 6 DATABASE storage tests (drift metrics)
- Cluster 3: 2 Fairness tests (both storage modes)
- Cluster 5: 2 Upload endpoint tests

### Issue #2: CRD Conversion Webhook Not Applied (P1 - HIGH)

**Affected Test Cluster:** 7 (1 failure)

**Root Cause:**
TrustyAIService CRD has:
- ✅ Multiple versions (v1, v1alpha1)
- ✅ Conversion methods (`ConvertTo`/`ConvertFrom` in api/tas/v1alpha1/conversion.go)
- ✅ Webhook patch file (`config/crd/patches/webhook_in_trustyaiservices.yaml`)
- ❌ But `conversion.strategy: None` in deployed CRD

**Evidence:**
```bash
$ oc get crd trustyaiservices.trustyai.opendatahub.io -o jsonpath='{.spec.conversion.strategy}'
None  # ← Expected: Webhook
```

**Impact:**
- Violates Kubernetes best practices for multi-version CRDs
- Prevents smooth API version migration
- Test validation failure

## Detailed Fix Plan

### Phase 1: Root Cause Investigation

**Task 1: Analyze Route Generation Bug**
- Trace why `-tls` suffix was added
- Understand service architecture (internal vs TLS services)
- Review service template files
- Document intended vs actual behavior

**Task 4: Investigate CRD Conversion Configuration**
- Check kustomization build process
- Verify why webhook patch isn't applied
- Determine if webhook service exists
- Review component vs deprecated kustomization structure

### Phase 2: Implementation

**Task 2: Fix route.go**
```go
// controllers/tas/route.go:21 (PROPOSED FIX)
routeConfig := utils.RouteConfig{
    ServiceName: instance.Name,  // ✅ Points to main service with API
    PortName:    "https",        // Target port 4443
}
```

**Task 3: Update route_test.go**
```go
// controllers/tas/route_test.go:28 (CHANGE)
Expect(route.Spec.To.Name).To(Equal(instance.Name))  // Remove -tls suffix

// controllers/tas/route_test.go:39 (CHANGE)
ServiceName: instance.Name,  // Remove -tls suffix
```

**Task 5: Enable CRD Conversion Webhook**
- Add webhook patch to component kustomization, OR
- Ensure webhook service is deployed, OR
- Fix kustomize build configuration

### Phase 3: Verification

**Task 6: Unit Tests**
```bash
make test
go test -v ./controllers/tas/... -run TestRoute
```
Expected: All route tests pass with new assertions

**Task 7: Cluster Testing**
```bash
# Deploy test instance
oc create namespace test-route-fix
# ... deploy TrustyAIService ...

# Verify route
oc get route trustyai-service -o jsonpath='{.spec.to.name}'
# Expected: trustyai-service (NOT trustyai-service-tls)

# Verify API access
curl -k https://<route>/info -H "Authorization: Bearer $TOKEN"
# Expected: Valid JSON (NOT 404)
```

**Task 8: Verify CRD Conversion**
```bash
# Check conversion strategy
oc get crd trustyaiservices.trustyai.opendatahub.io -o jsonpath='{.spec.conversion.strategy}'
# Expected: Webhook

# Test v1alpha1 → v1 conversion
oc apply -f <v1alpha1-cr>
oc get trustyaiservice <name> -o yaml | grep "apiVersion: trustyai.opendatahub.io/v1"
```

**Task 9: QE Test Suite**
Run affected test clusters to verify fixes:

```bash
# Cluster 2: DATABASE storage (6 tests)
uv run pytest tests/ai_safety/trustyai_service/drift/test_drift.py -k "db-storage"

# Cluster 3: Fairness (2 tests)
uv run pytest tests/ai_safety/trustyai_service/fairness/

# Cluster 5: Upload endpoints (2 tests)
uv run pytest tests/ai_safety/trustyai_service/service/test_trustyai_service.py \
  -k "upload_data_to_trustyai_service_with_db_storage or trustyai_service_db_migration"

# Cluster 7: CRD conversion (1 test)
uv run pytest tests/ai_safety/trustyai_service/service/test_trustyai_service.py \
  -k "crd_conversion_strategy_is_webhook"
```

### Phase 4: Documentation & Pull Requests

**Task 10: Route Fix PR**
- Title: `fix(route): point routes to main service instead of TLS-only service`
- Files: `controllers/tas/route.go`, `controllers/tas/route_test.go`
- Evidence: Link to test failure report, manual verification results
- Impact: Fixes 10 P0 test failures

**Task 11: CRD Conversion PR** (if code changes needed)
- Title: `fix(crd): enable conversion webhook for TrustyAIService multi-version support`
- Files: Kustomization configs or webhook deployment
- Evidence: Test failure, Kubernetes best practices
- Impact: Fixes 1 P1 test failure

**Task 12: Multi-Namespace Investigation**
- Document Cluster 4 findings (complex issue)
- Create follow-up issue for KServe investigation
- Not P0 since route fix should help significantly

## Service Architecture Clarification

TrustyAI operator creates TWO services:

### Service 1: `trustyai-service` (MAIN - HAS API)
```yaml
ports:
  - name: http
    port: 80 → targetPort: 8080
  - name: https
    port: 443 → targetPort: 4443  # ← API endpoints HERE
```

### Service 2: `trustyai-service-tls` (TLS PROXY - NO API)
```yaml
ports:
  - name: https
    port: 443 → targetPort: 8443  # ← kube-rbac-proxy ONLY
```

**Routes MUST point to Service 1**, not Service 2.

## Expected Test Improvements

| Fix Applied | Tests Fixed | New Pass Rate |
|-------------|-------------|---------------|
| Current state | - | 18% (14/78) |
| + Route fix | +10 | 31% (24/78) |
| + CRD conversion | +1 | 32% (25/78) |
| **With dependency resolution** | **+20-25** | **58-64% (45-50/78)** |

**Note:** Many tests are currently skipped due to upstream failures. Once route fix resolves those upstream tests, dependent tests will run and likely pass.

## Files to Modify

### Primary Changes
1. `controllers/tas/route.go` - Line 21
2. `controllers/tas/route_test.go` - Lines 28, 39
3. Kustomization file for CRD conversion (TBD after investigation)

### Files to Review (No Changes Expected)
- `controllers/tas/templates/service/service-internal.tmpl.yaml` - Main service definition
- `controllers/tas/templates/service/service-tls.tmpl.yaml` - TLS service definition
- `controllers/tas/templates/service/route.tmpl.yaml` - Route template
- `api/tas/v1alpha1/conversion.go` - Conversion methods (already implemented)
- `config/crd/patches/webhook_in_trustyaiservices.yaml` - Webhook patch (already exists)

## Verification Checklist

Before marking as complete:

- [ ] Route points to `trustyai-service` (not `trustyai-service-tls`)
- [ ] External API accessible via route (no 404 errors)
- [ ] Unit tests pass for route reconciliation
- [ ] Cluster testing shows routes work in fresh namespaces
- [ ] CRD has `conversion.strategy: Webhook`
- [ ] CRD version conversion works (v1alpha1 ↔ v1)
- [ ] QE test suite improvements verified:
  - [ ] Cluster 2: DATABASE storage tests pass
  - [ ] Cluster 3: Fairness tests pass
  - [ ] Cluster 5: Upload endpoint tests pass
  - [ ] Cluster 7: CRD conversion test passes
- [ ] PRs created with test evidence
- [ ] Multi-namespace investigation documented for follow-up

## Next Steps

1. **Start with Task 1** - Analyze route generation root cause
2. Follow the systematic debugging approach:
   - Phase 1: Understand root cause fully before coding
   - Phase 2: Implement minimal fixes
   - Phase 3: Verify at each level (unit → integration → e2e)
   - Phase 4: Document and create PRs

3. **Run tasks in dependency order** as shown in the task list

## Additional Context

**Why this matters:**
- Route bug affects EVERY operator deployment in production
- External API access completely broken for all users
- Tests reveal a critical production issue, not just test fragility

**Service code status:**
- TrustyAI service code is working correctly
- Only 1/8 test failure clusters (Cluster 1) was a service regression
- That service regression already fixed in PR #295
- These operator fixes are independent and required

## References

- Test Failure Report: `/Users/sudsinha/Repositories/work/opendatahub-tests/docs/trustyai_test_failures_detailed_report.md`
- Test Repository: `/Users/sudsinha/Repositories/work/opendatahub-tests`
- Operator Repository: `/Users/sudsinha/Repositories/work/trustyai-service-operator`

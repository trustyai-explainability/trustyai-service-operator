# TrustyAI Operator Route Bug Fix Plan (REVISED)

**Date:** 2026-08-18  
**Revision:** Post Phase 1 Investigation  
**Scope:** Route fix ONLY (CRD conversion moved to separate issue)

## Scope Change Summary

After Phase 1 investigation, the plan has been split into two separate efforts:

### This Plan (Route Fix - P0 CRITICAL)
- **Issue:** Routes point to wrong service (`trustyai-service-tls` instead of `trustyai-service`)
- **Impact:** 10 test failures, external API completely broken
- **Effort:** 2-4 hours
- **Priority:** P0 - Critical production bug

### Separate Issue (CRD Conversion - P1 Feature)
- **Issue:** CRD conversion webhook not implemented
- **Impact:** 1 test failure, prevents API version migration
- **Effort:** 8-16 hours (full feature implementation)
- **Priority:** P1 - Enhancement for future
- **Tracking:** Will create GitHub issue (Task 11)

---

## Critical Findings from Phase 1

### Route Bug Root Cause

**Historical Context:**
The route has pointed to `trustyai-service-tls` since its creation (Feb 2024, commit 1b7c2a3). This was originally correct:

- **Original architecture:** service-internal only had HTTP (port 80 → 8080)
- **OAuth proxy era:** service-tls provided authenticated HTTPS via OAuth proxy on port 8443
- **Route design:** Correctly pointed to service-tls for authenticated access

**What Changed:**
- HTTPS support added to service-internal (commit 7e1a712)
- Route **never updated** to point to service-internal
- Multiple refactors (OAuth → kube-rbac-proxy, route code cleanup) **preserved** the `-tls` suffix

**Current Architecture:**

```
Pod: trustyai-service-xxx
├── Container 1: trustyai-service
│   └── Port 8080 (HTTP, localhost only) - TrustyAI application
│
└── Container 2: kube-rbac-proxy  
    └── Port 8443 (HTTPS, all interfaces) - RBAC + TLS proxy to :8080
```

**Services:**
```yaml
# Service 1: trustyai-service (service-internal.tmpl.yaml)
ports:
  - name: http
    port: 80 → targetPort: 8080
  - name: https  
    port: 443 → targetPort: 4443  # ← BUG: Port doesn't exist!

# Service 2: trustyai-service-tls (service-tls.tmpl.yaml)
ports:
  - name: https
    port: 443 → targetPort: 8443  # ← Points to kube-rbac-proxy
```

**Route (WRONG):**
```yaml
spec:
  to:
    name: trustyai-service-tls  # Points to Service 2
  port:
    targetPort: https           # Port 443 → 8443
```

**Evidence from Test Report:**
- Internal calls work (bypass route, use cluster DNS)
- External API via route returns 404
- Manual fix in test-drift-pvc namespace (pointing route to `trustyai-service`) immediately resolved tests

### Secondary Issue Found

Service-internal has `targetPort: 4443` but **no container listens on port 4443**. This appears to be aspirational code that was never completed. Despite this, the test report shows the fix works when route points to service-internal.

**TODO:** Investigate whether port 4443 should be removed from service-internal or if there's a missing configuration.

---

## Revised Fix Plan

### Phase 2: Implementation (Tasks 2, 3)

**Task 2: Fix route.go**
```go
// controllers/tas/route.go:21
// BEFORE:
routeConfig := utils.RouteConfig{
    ServiceName: instance.Name + "-tls",
    PortName:    KubeRBACProxyServicePortName,
}

// AFTER:
routeConfig := utils.RouteConfig{
    ServiceName: instance.Name,  // Remove -tls suffix
    PortName:    KubeRBACProxyServicePortName,
}
```

**Task 3: Update route_test.go**
```go
// controllers/tas/route_test.go:28
// BEFORE:
Expect(route.Spec.To.Name).To(Equal(instance.Name + "-tls"))

// AFTER:
Expect(route.Spec.To.Name).To(Equal(instance.Name))

// controllers/tas/route_test.go:39
// BEFORE:
ServiceName: instance.Name + "-tls",

// AFTER:
ServiceName: instance.Name,
```

### Phase 3: Verification (Tasks 6, 7)

**Task 6: Unit Tests**
```bash
make test
go test -v ./controllers/tas/... -run TestRoute
```

**Task 7: Cluster Testing**
```bash
# Deploy test instance
oc create namespace test-route-fix
oc apply -f <trustyai-service-cr>

# Verify route target
oc get route trustyai-service -n test-route-fix -o jsonpath='{.spec.to.name}'
# Expected: trustyai-service (NOT trustyai-service-tls)

# Verify API accessible
ROUTE_URL=$(oc get route trustyai-service -n test-route-fix -o jsonpath='{.spec.host}')
TOKEN=$(oc whoami -t)
curl -k https://$ROUTE_URL/info -H "Authorization: Bearer $TOKEN"
# Expected: Valid JSON response (NOT 404)
```

### Phase 4: QE Validation (Task 9)

Run affected test clusters:

```bash
# Cluster 2: DATABASE storage (6 tests)
uv run pytest tests/ai_safety/trustyai_service/drift/test_drift.py -k "db-storage" -v

# Cluster 3: Fairness (2 tests)
uv run pytest tests/ai_safety/trustyai_service/fairness/ -v

# Cluster 5: Upload endpoints (2 tests)
uv run pytest tests/ai_safety/trustyai_service/service/test_trustyai_service.py \
  -k "upload_data_to_trustyai_service_with_db_storage or trustyai_service_db_migration" -v
```

**Note:** CRD conversion test (Cluster 7) will still fail - that's expected and tracked separately.

### Phase 5: PR Creation (Task 10)

**PR Title:** `fix(route): point routes to main service instead of TLS-only service`

**PR Description:**
```markdown
## Summary
Fixes route misconfiguration causing external API to return 404 errors.

## Root Cause
Routes have pointed to `trustyai-service-tls` since creation (Feb 2024). This was originally correct when service-internal had no HTTPS port. When HTTPS was added to service-internal, the route was never updated.

## Changes
- `controllers/tas/route.go:21` - Remove `-tls` suffix from service name
- `controllers/tas/route_test.go:28,39` - Update test expectations

## Impact
Fixes 10 P0 test failures:
- Cluster 2: 6 DATABASE storage tests
- Cluster 3: 2 Fairness tests  
- Cluster 5: 2 Upload endpoint tests

See detailed test report: [link to test-failure-fix-plan.md]

## Testing
- [x] Unit tests pass
- [x] Cluster testing: route points to correct service
- [x] API accessible via route (no 404 errors)
- [x] QE test suite: affected clusters pass

## Related Issues
- Test failures report: /Users/sudsinha/Repositories/work/opendatahub-tests/docs/trustyai_test_failures_detailed_report.md
```

---

## Expected Impact

| Metric | Before | After Route Fix |
|--------|--------|----------------|
| Tests passing | 14/78 (18%) | 24/78 (31%) |
| Route target | trustyai-service-tls ❌ | trustyai-service ✅ |
| External API | 404 errors ❌ | Working ✅ |
| Test failures resolved | - | 10 tests |

**With dependency resolution:** Once upstream tests pass, dependent tests will run → estimated 45-50/78 passing (58-64%)

---

## Files to Modify

### Primary Changes
1. `controllers/tas/route.go` - Line 21 (1 line change)
2. `controllers/tas/route_test.go` - Lines 28, 39 (2 line changes)

### Files to Review (No Changes)
- `controllers/tas/templates/service/service-internal.tmpl.yaml` - Investigate port 4443 issue separately
- `controllers/tas/templates/service/service-tls.tmpl.yaml` - No changes needed
- `controllers/tas/templates/service/route.tmpl.yaml` - Template is correct, only code changes needed

---

## Out of Scope (Separate Tracking)

### CRD Conversion Webhook (Task 11)
- **Status:** Incomplete feature requiring full implementation
- **Effort:** 8-16 hours
- **Priority:** P1 (enhancement)
- **Action:** Create GitHub issue for future work
- **Missing:** Webhook server, cert management, service manifests, kustomization config

### Service-Internal Port 4443 Issue
- **Status:** Discovered during investigation
- **Issue:** service-internal has `targetPort: 4443` but no container listens on this port
- **Action:** Investigate separately after route fix is confirmed working
- **Priority:** P2 (doesn't block route fix per test evidence)

### Multi-Namespace Timeouts (Task 12)
- **Status:** Complex infrastructure issue
- **Partial resolution:** Route fix should help significantly (Factor 1)
- **Remaining work:** KServe investigation, resource optimization
- **Action:** Document findings, create follow-up issue
- **Priority:** P1

---

## Task Execution Order

```
✅ Task 1: Analyze route generation bug root cause (COMPLETED)
✅ Task 4: Investigate CRD conversion webhook (COMPLETED)  
⏭️ Task 5: Enable CRD conversion webhook (DELETED - out of scope)
✅ Task 8: Verify CRD conversion webhook (SKIPPED - not applicable)

Current workflow:
→ Task 2: Fix route.go
  → Task 3: Update route_test.go
    → Task 6: Run unit tests
      → Task 7: Test on live cluster
        → Task 9: Run QE test suite
          → Task 10: Create route fix PR
          
Parallel:
→ Task 11: Create CRD conversion tracking issue (independent)
→ Task 12: Document multi-namespace findings (independent)
```

---

## Next Steps

1. **Await user approval** to proceed with Phase 2 (Implementation)
2. Implement route fix (Tasks 2, 3)
3. Verify locally and on cluster (Tasks 6, 7)
4. Run QE test suite (Task 9)
5. Create PR with evidence (Task 10)
6. Create tracking issues for out-of-scope work (Tasks 11, 12)

---

## References

- **Original Plan:** `/Users/sudsinha/Repositories/work/trustyai-service-operator/docs/test-failure-fix-plan.md`
- **Root Cause Analysis:** `/Users/sudsinha/Repositories/work/trustyai-service-operator/docs/route-bug-root-cause-analysis.md`
- **Test Failure Report:** `/Users/sudsinha/Repositories/work/opendatahub-tests/docs/trustyai_test_failures_detailed_report.md`
- **Operator Repo:** `/Users/sudsinha/Repositories/work/trustyai-service-operator`
- **Test Repo:** `/Users/sudsinha/Repositories/work/opendatahub-tests`

# Manifest Policies

This directory contains [OPA/Rego](https://www.openpolicyagent.org/) policies that are evaluated against the rendered kustomize manifests on every push and pull request. The CI workflow (`Tier 1 - Manifest policy check`) renders **every** kustomize entry point (base + all overlays) and checks each against these policies using [Conftest](https://www.conftest.dev/).

## Current policies

### `rbac.rego` — ClusterRoleBinding allowlist

Prevents accidental cluster-wide privilege escalation by maintaining a closed allowlist of expected `ClusterRoleBinding` resources.

**Rules:**

1. **No unexpected ClusterRoleBindings.** Any CRB whose post-kustomize name is not in `expected_crbs` is denied. This catches cases where a developer accidentally changes a `RoleBinding` to a `ClusterRoleBinding`, or adds a new CRB without review.
2. **No role-reference mismatches.** An allowlisted CRB that references a different `ClusterRole` than expected is denied. This catches copy-paste mistakes or rename errors.

**Tested overlays:** `base`, `odh`, `rhoai`, `lmes`, `odh-kueue`, `testing`, `dev`, `evalhub-only`, `mcp-guardrails`.

### `clusterrole.rego` — ClusterRole content inspection

Validates the **contents** of every `ClusterRole` in the rendered manifests using two layers:

1. **Allowlist.** A closed set of `(apiGroup, resource)` pairs. Any rule that references an `(apiGroup, resource)` pair not in `allowed_api_resources` is denied. This catches cases where a developer adds permissions to a new API group without review.
2. **Denylist.** Blocks dangerous patterns regardless of allowlist status:
   - Wildcard `"*"` in apiGroups, resources, or verbs
   - Write verbs (`create`, `update`, `patch`, `delete`, `deletecollection`) on `secrets`
   - Write verbs on `clusterroles` or `clusterrolebindings` (privilege escalation)
   - Escalation verbs: `escalate`, `bind`, `impersonate`

   **Exemptions** (matched by role name suffix):
   - *Secrets write:* `tas-manager-role`, `gorch-manager-role`, `nemo-guardrails-manager-role` — these managers create/manage TLS certificates and service credentials for their workloads.
   - *ClusterRoleBindings write:* `tas-manager-role`, `evalhub-manager-role`, `nemo-guardrails-manager-role` — these managers create CRBs to bind service accounts to component-specific roles.

## Adding a new ClusterRole permission

If your change adds a new `(apiGroup, resource)` pair to any ClusterRole:

1. Add the `[apiGroup, resource]` tuple to `allowed_api_resources` in `policy/clusterrole.rego`.
2. Run `make policy-check` locally to verify.
3. Explain in the PR description why the new permission is needed.

## Adding a new ClusterRoleBinding

If your change legitimately introduces a new `ClusterRoleBinding`:

1. Add the post-kustomize CRB name and its expected `ClusterRole` to the `expected_crbs` map in `policy/rbac.rego`.
2. If the overlay does not apply a `namePrefix`, add the un-prefixed name as well.
3. Run `make policy-check` locally to verify.
4. Explain in the PR description why a `ClusterRoleBinding` is needed (rather than a namespace-scoped `RoleBinding`).

## Adding a new policy

1. Create a new `.rego` file in this directory under the appropriate package name.
2. Add corresponding `_test.rego` unit tests.
3. Run `make policy-test` to verify the unit tests pass.
4. Run `make policy-check` to verify against all rendered manifests.

## Running locally

```bash
# OPA unit tests only
make policy-test

# Full check against all kustomize overlays
make policy-check

# Quick check against a single overlay
kustomize build config/overlays/odh | conftest test --policy policy/ --namespace rbac -
```

## CI

The GitHub Actions workflow `.github/workflows/conftest.yaml` runs both steps on every push and PR. It installs kustomize and conftest, runs the OPA unit tests, then checks all 9 kustomize entry points.

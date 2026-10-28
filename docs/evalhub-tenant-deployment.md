# Draft: namespace-trusted EvalHub self-service

This design treats all project editors and workloads within a namespace as equally
trusted. It does not provide isolation between users sharing a project.

## Permissions

The EvalHub component aggregates `evalhub-instance-manager` into the standard
`edit` and `admin` ClusterRoles. Existing namespace bindings to those roles then
allow members to create, read, update, and delete any EvalHub CR in that namespace.
No per-instance administrator action is required. View-only members get no new
permissions. Cluster-wide bindings to edit/admin also inherit the grant; audit
those bindings before rollout.

Evaluation REST API and dashboard authorization remain separate. This change
provides CR lifecycle access, not a complete dashboard onboarding workflow.
It does not change existing runtime service-account bindings.

## Enforced tenancy boundary

The component installs a fail-closed ValidatingAdmissionPolicy and Deny binding.
On CREATE and UPDATE of EvalHub CRs (all served versions), it requires explicit
`spec.tenancy: single`, unless the requesting identity has the cluster-scoped
virtual permission `use` on `trustyai.opendatahub.io/evalhubmultitenancy`.
The existing CRD default remains multi for compatibility; ordinary project users
must explicitly request single, otherwise admission rejects their request.
Removing tenancy or switching to multi is rejected too. Status subresource writes
are excluded. Ordinary namespace RoleBindings cannot grant the cluster-scoped
bypass checked by admission.

The non-aggregated `evalhub-multitenancy-manager` ClusterRole carries only that
virtual permission. The operator gets a ClusterRoleBinding to it so finalizer
updates on existing multi-tenant instances still pass admission. Cluster admins
already satisfy the check through wildcard privileges. Other platform identities
that create/update multi-tenant CRs (including GitOps service accounts) need an
explicit cluster-level grant plus their separate EvalHub lifecycle permissions.
Do not bind this bypass to project users or namespace service-account groups.

## Prerequisites and rollout

Requires Kubernetes 1.30+ with admissionregistration.k8s.io/v1 admission policies
supported and enabled. Verify support on every targeted OpenShift release.
The policy and its binding must be active before granting aggregated lifecycle
permissions. A bulk manifest apply is not an atomic security rollout: installation,
upgrade, and uninstall ordering need validation before this draft ships.
On uninstall, remove delegated access before removing enforcement; review retained
CRs and aggregation-controller convergence as part of that procedure.

Inventory existing multi-tenant CRs and their managers before upgrade. Ensure
operator and platform automation bypass bindings exist before enforcement starts.
Existing multi-tenant instances are not automatically converted. Unprivileged
managers will lose the ability to update those CRs, including metadata. DELETE
is not blocked by this policy; lifecycle RBAC still governs deletion.

## Security assumptions and outstanding verification

Single tenancy skips the operator's cross-namespace tenant provisioning path.
It does not prove all remaining configuration is safe. Before production rollout:

- Audit instance and MCP service-account privileges against configurable images,
  environment variables, and secret references; same-namespace access is accepted,
  but cross-namespace access or privileged execution is not.
- Verify the auth-reviewer cluster grant is minimal and cleaned up on deletion.
- Audit users who can edit policy objects, bypass bindings, or parent aggregated
  roles; those are platform-level privileges.
- Verify resource quotas, limits, repeated creation, and multiple-instance behavior.
- Test that project editors cannot create multi, remove tenancy, switch modes,
  or acquire the virtual bypass; viewers cannot create CRs.
- Test authorized multi creation, controller finalizer updates, status updates,
  deletion cleanup, and real non-admin dashboard/API use.
- Render every deployment overlay and confirm policyName, roleRef, and operator
  service-account references transform correctly in both installation paths.

Unit tests compile the policy with Kubernetes' admission CEL compiler and exercise
the tenancy predicate with allow/deny authorization results. They do not establish
real API-server admission behavior, RBAC aggregation, namespace isolation, or
installation ordering. This remains a tentative design until those checks pass.

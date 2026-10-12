# LMEvalJob execution NetworkPolicies

The LMES controller reconciles one combined `networking.k8s.io/v1` NetworkPolicy
for each persisted LMEvalJob in its namespace. It covers the primary network of
the evaluation Pod, including init and companion containers.

## Compatibility profile

- Explicit `policyTypes: [Ingress, Egress]`.
- No ingress allow rules. The driver management server binds to loopback; the
  controller polls/shuts it down through Kubernetes `pods/exec`, not Pod TCP
  ingress. Outbound connection replies do not need an inbound grant.
- Explicit **AllowAll egress**, exactly `egress: [{}]`, with controller-authored
  rationale and residual-risk annotations from the shared utilities.

Supported jobs can use arbitrary in-cluster, cross-namespace or external model
endpoints and TCP ports. Libraries can infer tokenizer, model-weight, dataset,
metric, Git, Hugging Face and redirected CDN/object-storage dependencies. Offline
S3 preparation and OCI exports can have Secret-backed endpoints, and companion
containers can have additional dependencies. No model-URL parser, Secret read,
fixed port allowlist or one-time DNS resolution can establish a complete policy
for these execution modes.

**Outbound communication is unrestricted. This profile provides no exfiltration
protection and can broaden customer NetworkPolicies: ordinary grants are
additive, so a narrower policy cannot reduce this grant.** Review the full
selecting-policy union before rollout. Higher-order administrative policies may
have different precedence. Application `allowOnline`/offline flags and
`allowCodeExecution` are not network-firewall settings; they do not select a
DenyAll or Restricted network profile.

Remote access to companion-container listeners is not provided by this profile.
All containers share the same network boundary. Adding inbound listeners or a
Restricted/fully local execution profile needs a separately qualified contract.
NetworkPolicy also does not replace application authentication or provide
intra-Pod, host-network or secondary-network isolation.

## Identity and launch ordering

The shared policy identity uses the LMEvalJob group/kind/UID, component `lmes`,
role `evaluation` and purpose `execution`. Collision-safe names change when the
owner UID changes. The exact selector requires these reserved labels:

- `trustyai.opendatahub.io/component`
- `trustyai.opendatahub.io/role`
- `trustyai.opendatahub.io/owner-uid`
- `trustyai.opendatahub.io/lmevaljob-uid` (retained upgrade compatibility)

The launch path sets reserved labels after user metadata merging. Existing Pods
are repaired only after replacement coverage exists and only when their
same-namespace controller group/kind/name/UID matches the job. Unrelated labels
are preserved. These labels are selection metadata, not admission authorization
against a user with permission to label arbitrary Pods.

Policy validation/reconciliation, identity repair and policy-readiness status
writes gate initial, resumed and rerun launches. Policy API convergence does not
prove immediate CNI realization. Drift/deletion watches provide eventual repair,
not continuous fail-closed packet filtering during the recovery window.

## Status and lifecycle

`status.conditions[type=NetworkPolicyReady]` is independent of evaluation
`state`, `reason`, progress and results. It is True only after the desired policy
and any existing owned Pod identity converge, including legacy cleanup. False
reports recoverable reconciliation failure without publishing raw endpoints or
API errors in job status. Successful reconciliation clears the failure. Condition
writes use optimistic concurrency and do not create status-only write loops.

Polling and safe completion/cancellation/suspension/deletion still proceed during
policy failure; the error is returned for retry. New launches fail closed. The
controller verifies immutable Pod ownership before repair, deletion and exec;
Pod patches and deletion carry concurrency/precondition protection. Kubernetes
exec is name-addressed and has no UID-precondition parameter, so the owner check
is not an atomic authorization guarantee against a concurrent replacement.

Policies are retained with retained complete, cancelled or suspended CRs, and
remain available through artifact export and Pod termination. Same-namespace
controller ownership enables garbage collection after CR deletion. There are no
cross-namespace owner references or namespace-prefix cleanup sweeps.

For upgrade from the former ingress-only implementation, replacement policy is
created before relabeling. Cleanup targets only the deterministic old per-UID
name with verified controller/manager/UID metadata and exact legacy ingress-deny
selector. Deletion uses the UID and resourceVersion from that provenance check;
foreign, ownerless, changed or replaced objects are not adopted/deleted. Existing
running jobs with no pre-upgrade policy have a coverage gap that cannot be
retroactively eliminated.

## Deployment and validation boundaries

Generated LMES permissions include NetworkPolicy get/list/watch/create/update/
patch/delete and Pod patch. The module manager receives matching delegated
NetworkPolicy permissions to satisfy Kubernetes RBAC escalation checks. Workload
manifests, additive condition schema and deepcopy output are synchronized into
the module source tree. Job ServiceAccount permissions are not broadened.

Unit/fake-client tests cover identity, launch failures, error/status preservation,
conflicts, migration ordering, reserved-label watches and drift convergence.
Envtest exercises actual API/controller watches for policy deletion/spec drift
and reserved-label repair on a retained completed execution. Envtest does not
run a CNI, kubelet or garbage collector.

Real-cluster qualification remains necessary for reachable positive-controlled
ingress probes, arbitrary endpoint/download paths, unrestricted-egress behavior,
S3/OCI/sidecars, effective delegated permissions, Kueue, garbage collection,
install/upgrade and module removal/drain. Aggregate component/module/DSC reporting
of job policy health remains a cross-component follow-up; the per-job condition
alone is not a claim of platform-level health propagation or contract acceptance.

Use the sanitized evidence template in [the shared integration guide](network-policy-foundations.md).
Never report an unrun/skipped check as passing or baseline images as candidate
qualification. Keep the tracking issue open until the applicable acceptance
criteria and integration follow-ups are satisfied.

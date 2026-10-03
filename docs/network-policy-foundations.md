# Operand NetworkPolicy foundations

The helpers in `controllers/utils/network_policy_*.go` are opt-in building blocks.
They do not install policies, grant RBAC, modify component controllers, change
public CRDs, or establish complete operand isolation. Component integrations
must qualify their traffic, ownership, status, migration and deployment paths.
Tracking: [#956](https://github.com/trustyai-explainability/trustyai-service-operator/issues/956).

## Identity and labels

`NetworkPolicyIdentity` requires owner group/kind/UID, component and role. The
reserved target labels are `trustyai.opendatahub.io/component`, `/role` and
`/owner-uid` (all with the same domain prefix). Policy names hash the full
identity and purpose; display-name normalization, long input, owner recreation,
kind and role differences cannot silently collapse to a truncated name. Names
are stable across API version changes. Long/non-label-safe UIDs use a bounded
hash as their label representation. Hashes are identifiers, not credentials.

Every target selector must contain the exact reserved `MatchLabels`; additional
valid constraints are allowed. Peer selectors must be valid and positively
constrained by nonempty MatchLabels/In values or an `Exists` expression.
Namespace-only ingress peers are rejected unless the controller
supplies an exact `NetworkPolicyNamespaceOnlyPeerIntent` through
`NetworkPolicyIngressIntent`, with nonempty rationale and residual risk. The
namespace selector must exactly match an actual peer; unused/mismatched
exceptions are rejected. This explicit opt-in does not infer trust from a
namespace label, authorize empty selectors, or allow namespace-only egress peers.
The typed intent is recorded in a managed annotation; annotation edits alone do
not authorize a peer. Cross-namespace AND constraints belong in one peer;
separate peers are OR grants. CIDRs cannot be universal, and exceptions must be
strict contained subprefixes. Allow rules need explicit validated ports; named
ports and numeric ranges use Kubernetes rules.

Labels alone do not authorize tenants, namespace membership or policy adoption.
Controllers must protect reserved-label authority and use live workload owners.
For `LabelOwnedNetworkPolicyDeployment`, pass an owner with its GVK set, verify
it is current, and authorize the placement before invoking the helper. The helper
checks same-namespace direct controller identity, overwrites reserved template
labels after user merges, and leaves immutable selectors unchanged. Conflicting
immutable selectors cause an error without mutation.

## Direction and egress intent

`ValidateWorkloadNetworkPolicy` validates one policy; it can validate an
ingress-only object, but that is not explicit workload egress coverage.
`ValidateWorkloadNetworkPolicySet` additionally requires both directions for the
same exact target selector and namespace and rejects conflicting egress modes
across the supplied policies. It does not enumerate customer or
platform policies and cannot prove that every running Pod is covered.

For any Egress policy, pass a controller-supplied `NetworkPolicyEgressIntent`:

| Mode | Required rules and evidence |
|---|---|
| `DenyAll` | Explicit Egress direction; no allow rules. Nil and empty rule slices have identical semantics after API serialization. Establish that no required outbound dependency exists. |
| `Restricted` | Nonempty rules with explicit constrained destinations and ports. Include DNS/API/init/sidecar dependencies where needed; do not guess infrastructure addresses or resolve hostnames once into static IP rules. |
| `AllowAll` | Exactly one empty rule (`egress: [{}]`), nonblank rationale and residual-risk text. No additional restricted rules may misleadingly accompany the permissive rule. |

Reconciliation writes enumerated controller-owned egress mode, rationale,
residual-risk and unrestricted-behavior annotations, and records any explicitly
authorized namespace-only ingress peer intent. These are records of typed
intent, never permission to bypass validation. Altering an annotation alone
cannot authorize AllowAll or a broad ingress peer. No component is automatically
assigned AllowAll. It permits unrestricted outbound traffic and may broaden
pre-existing customer restrictions: NetworkPolicy grants are additive, and
another restrictive policy cannot narrow this allowance. Unrestricted ingress
is never enabled.

## Reconciliation, authority and cleanup

`ReconcileWorkloadNetworkPolicy` requires an explicit controller-authorized
namespace set and a live same-namespace local controller owner. For tenant
execution, use a verified local owner only after the tenant authority contract
is established. Cross-namespace CR owner references are not supported. No
namespace-label lookup is implicitly treated as authorization.

The desired policy supplies name, namespace and spec. Its server metadata,
labels and annotations are not written: managed metadata is generated from
trusted arguments, and callers cannot inject owner references. Existing
unrelated metadata/non-controller references are preserved. Only these keys
are owned by this helper:

- Target identity labels and `trustyai.opendatahub.io/network-policy-managed-by`.
- `trustyai.opendatahub.io/network-policy-egress-mode`.
- `trustyai.opendatahub.io/network-policy-egress-rationale`.
- `trustyai.opendatahub.io/network-policy-egress-residual-risk`.
- `trustyai.opendatahub.io/network-policy-egress-behavior`.
- `trustyai.opendatahub.io/network-policy-ingress-namespace-only-peers`.

Conflicting controller owners, managed owner labels or managers cause rejection
before a spec write. Ownerless policies are refused unless the caller supplies
`VerifiedAdoptionUID` from independently checked provenance. Do not populate it
simply by reading a same-name policy or trusting forgeable labels. Policies with
removed owner references therefore fail safely until provenance is verified.

Missing policies are recreated; drift is repaired; normalized empty arrays and
default TCP protocol do not cause repeated writes. API errors, permission errors,
AlreadyExists races and resourceVersion conflicts are returned for controller
retry, not interpreted as absence or automatically suppressed.

`DeleteOwnedWorkloadNetworkPolicy` requires exact name/namespace, recorded policy
UID and matching controller owner; it never adopts an ownerless policy. Delete
uses UID and resourceVersion preconditions. A replaced owner/policy cannot be
swept by prefix or name. Cleanup permits a deleting live owner so finalizers can
finish, but cannot operate after the owner disappears. Components needing
post-owner orphan cleanup must define a separately verified lifecycle contract.

## Integration sequence and status

1. Validate the complete desired set and placement/owner authority.
2. Reconcile policies before launching a new workload where supported.
3. Stamp reserved labels only on verified owned templates/Pods. For a rolling
   migration, create replacement policies first and retain legacy protection.
4. Wait for old replicas/Pods to drain before UID-guarded legacy policy deletion.
   Do not change immutable Deployment selectors. New narrower policies cannot
   counteract old broad grants; migration is incomplete until those grants go.
5. Prefer `ReconcileWorkloadNetworkPolicySet`: it validates the desired set
   before writes, stops on the first reconciliation failure, and invokes status
   once, with success only after every policy converges. Sets require unique
   explicit policy names. `ReportNetworkPolicyResult` also supports custom
   orchestrators; report the **complete-set** result, not success after the first
   object. The callback persists component status and
   returns any status-write error. Both reconcile and status errors are retained.
   Component adapters must suppress false Ready, propagate DSC failure/recovery,
   and preserve evaluation outcomes; this callback alone does not do that wiring.
6. Retain reconcilers and effective RBAC until approved drain/preservation and
   real garbage collection complete. Do not delete module controllers/RBAC first.

Launch ordering and API reconciliation do not prove atomic CNI propagation or
continuous fail-closed behavior during drift/deletion. No RBAC is added by these
helpers: each controller must add/generate and test the verbs it actually uses.
Module teardown, admission/namespace authority and aggregate status must be
implemented and tested in their integrations before claiming completion.

## Validation and common evidence format

Unit/fake-client tests verify logic, not watches, garbage collection or packet
filtering. `TestNetworkPolicyAPI` uses local envtest API server/etcd (not the
current kubeconfig), verifies serialization/defaulting/no-op reconciliation and
UID-guarded deletion, and explicitly skips when `KUBEBUILDER_ASSETS` is unset.
It still provides neither garbage-collection nor CNI evidence.

```sh
KUBEBUILDER_ASSETS=/path/to/envtest/assets go test ./controllers/utils -count=1
KUBEBUILDER_ASSETS=/path/to/envtest/assets go test -race ./controllers/utils -count=1
go vet ./controllers/utils
```

For each component positive/negative traffic or lifecycle case, use this common
record (a template, not an automated traffic harness):

```yaml
case_id: component-direction-scenario
result: blocked # passed | failed | blocked | skipped; never infer a pass
source_revision: <immutable commit>
images: [<image@sha256:digest>]
variant: <component/ODH/RHOAI/modular and version>
platform_cni: <OpenShift/Kubernetes and CNI version>
feature_mode: <auth/route/storage/tenant/provider mode>
source: <namespace, Pod/role/UID; sanitized>
destination: <namespace, Pod/role/UID or approved external endpoint; sanitized>
protocol_pod_port_service_path: <actual target listener and Service mapping>
selectors_and_policy_union: <all selecting policies, not only operator policies>
other_enforcement: <administrator/baseline controls>
effective_permissions: <ServiceAccount and actual authorization checks>
probe: <exact bounded command without credentials>
positive_control: <reachable listener and approved successful path>
expected: <network/TLS/auth/application outcome>
observed: <actual outcome, including prerequisite failures>
timestamps: <start/end>
restoration_cleanup: <actual results and retained resources>
limitations: <unrun cases and accepted exceptions, if any>
evidence: <approved artifact location>
```

An HTTP 401/403, CA error, closed listener, no Pods, all-skipped tests or timeout
without controls is not proof of network isolation. Aggregate component evidence
before claiming install/upgrade, effective RBAC, module removal or security
qualification. Never include credentials, Secret contents, raw evaluation data
or restricted architecture material in public artifacts.

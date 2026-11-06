# EvalHub operand NetworkPolicies

The EvalHub controller reconciles a NetworkPolicy for each persisted `EvalHub`
before it creates or updates the corresponding Deployment. Policies are owned by
the `EvalHub` CR, select exactly its reserved component, role and owner-UID
labels, and are recreated after deletion. These labels are selection metadata,
not application authorization.

Two workloads are covered, each with its own object and its own selector:

| Workload | Role label | Created when |
| --- | --- | --- |
| EvalHub API Deployment (`<name>`) | `role=api` | always |
| EvalHub MCP Deployment (`<name>-mcp`) | `role=mcp` | `spec.mcp.enabled: true` |

## Traffic contract

All listed ports are TCP destination **Pod** ports, after Service translation.

| Caller | Required source identity | Pod ports |
| --- | --- | --- |
| OpenShift router | Namespace `network.openshift.io/policy-group=ingress` | 8443 (API), 8443 (MCP) |
| Dashboard `eval-hub-ui` proxy | Applications namespace AND Pod `deployment=eval-hub-ui`, `app.kubernetes.io/part-of=odh-dashboard` | 8443 (API) |
| Operator manager (failure events) | Applications namespace AND Pod `control-plane=trustyai-service-operator` | 8443 (API) |
| Evaluation Jobs | Same namespace, or a tenant namespace, AND the instance's `app.kubernetes.io/instance` Pod labels | 8443 (API) |
| Prometheus | Namespace `network.openshift.io/policy-group=monitoring` AND Pod `app.kubernetes.io/name=prometheus`, `app.kubernetes.io/component=prometheus` | 8081 (API metrics) |
| MCP server | Pod identity `component=evalhub`, `role=mcp` for this instance | 8443 (API) |

The MCP server has no metrics endpoint of its own and no in-cluster callers in
the supported contract: clients reach it through its Route, and it calls the
EvalHub API outbound. Its policy therefore admits only the Route peer.

Router ingress deliberately uses a namespace-only peer: no stable supported router
Pod identity is assumed. Every Pod in a namespace carrying that label can reach
the pod ports above. The reconciled annotation records this rationale and risk.

Health probes need no ingress rule: `NetworkPolicy` always allows traffic from the
Pod's own node, which is where kubelet probes originate. The kube-rbac-proxy health
listener on 9443 and the loopback-only upstream remain unadmitted, and no claim of
enforceable kubelet isolation is made.

Other direct in-cluster callers are not authorized. Before adding a client,
qualify its source identity and provide a separately managed, narrowly scoped
ingress policy; NetworkPolicy rules are additive, so a separate broad grant can
defeat these restrictions.

## Egress

Both policies declare `Egress` in `policyTypes` with an explicit allow-all rule and
a recorded rationale, as ODH-ADR-Operator-0016 requires for every operand
workload. EvalHub reaches the Kubernetes API server, tenant namespaces, model and
judge endpoints, MLflow and object storage whose addresses are supplied at
evaluation time, so no complete static allowlist exists. This is a compatibility
posture, not exfiltration protection: Kubernetes permits narrower outbound rules
but cannot express hostnames, and an allow-all grant cannot be narrowed by any
other policy. A restricted profile remains a follow-up decision.

The managed annotations record the mode, rationale and residual risk on each
policy.

## Ownership, repair and removal

The controller writes the policy before the Deployment is created or updated, so a
Pod enters the selector only after its allow rules have converged in the API.
Policy objects carry a controller reference to the `EvalHub` CR, which makes
Kubernetes garbage collection remove them with the CR. Both policies are watched:
deleting one, or editing its rules, enqueues a reconcile that restores the desired
state. Unrelated labels and annotations on the object are preserved.

Disabling `spec.mcp.enabled` deletes the MCP policy rather than leaving an object
that selects no workload. The delete path refuses to adopt or remove a same-name
object the controller does not own.

## Scope limits

### Interaction with the platform applications-namespace baseline

NetworkPolicies are additive and isolation is decided per direction: a Pod's
ingress is isolated as soon as one selecting policy declares `Ingress`, and the
traffic then allowed is the **union** of the rules from every selecting policy
that declares `Ingress`. There is no policy that can subtract from another and no
API-level conflict, but a broad policy can only widen what a narrow one allows.

On a platform-managed cluster, `opendatahub-operator` reconciles a namespace-wide
policy named after the applications namespace, selecting every Pod except the
dashboard redirect proxy, declaring `Ingress` only, with six namespace-only peers
and no ports. It is the only platform-created policy that selects EvalHub Pods:
the operator's other two policies select the gateway auth proxy (`app:
<proxy-name>`) and the dashboard redirect proxy (`app: dashboard-redirect`), and
no module policy in ODH targets EvalHub labels. Both EvalHub workloads are
selected by the baseline, so today's effective
API and MCP ingress is the union of that policy and the rules above: any Pod in
an `opendatahub.io/generated-namespace=true` namespace, a user application
namespace, an ingress namespace, the host-network namespace, or the monitoring
namespaces can reach them on **any port**. The ingress rules in these objects are
therefore not the effective control until the platform baseline is narrowed.

What these objects still provide in that situation: the explicit egress posture
(the baseline declares no `Egress`, so nothing else grants or restricts it), the
ownership and repair lifecycle, and the per-workload control that takes effect
the moment the baseline is narrowed or removed. They cannot widen any other
workload: each selects only its own Pods, and the allow-all egress rule is scoped
to them.

### Single-tenancy

With `spec.tenancy: single` the served namespace is the instance's own namespace:
jobs run there, cross-namespace tenant provisioning is skipped, and the
`evalhub-tenant-admin` / `evalhub-user` convenience Roles are created in that
namespace. Neither Role grants NetworkPolicy verbs, so tenant users cannot author
or widen these policies.

Nothing in the policy set changes for single tenancy. Job callbacks are covered by
the same-namespace evaluation-job peer, which is the peer that applies when jobs
run in the instance namespace. The tenant-namespace peer remains in the object but
selects nothing, because it also requires this instance's evaluation-job Pod
labels and a tenant-labelled namespace, which single-tenancy mode never uses.

Single-tenant instances may be placed in any namespace. When the instance is not
in the applications namespace, the platform baseline described above does not
select it, so these ingress rules are the effective control rather than a layer
underneath a broad allow. The caveat above applies only to instances deployed in
the applications namespace.

Coverage is limited to the EvalHub namespace, which is the only namespace the
controller is authorized to write policies in. Tenant namespaces contribute
callback sources; they are never authored by this reconciler. Evaluation Job Pods
created by the EvalHub service carry their own policy, written by that service.
Clusters using a CNI without NetworkPolicy enforcement accept these objects
without isolating traffic, and policy creation does not prove that a CNI has
programmed the rules.

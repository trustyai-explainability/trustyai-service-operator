# NeMo Guardrails operand NetworkPolicies

The controller always reconciles one operand `NetworkPolicy` per `NemoGuardrails`
instance. There is no opt-out: a namespace or cluster default-deny baseline must
not leave the workload uncovered. The policy object implements
[ODH-ADR-Operator-0016](https://github.com/opendatahub-io/architecture-decision-records/blob/main/architecture-decision-records/operator/ODH-ADR-Operator-0016-networkpolicy-platform-contract.md)
for this operand.

## Where the policy lives, and who owns it

The policy is created in the **workload namespace**, exactly where the
controller places the Deployment, Service and Route, and nowhere else:

- Default placement: the `NemoGuardrails` custom resource's own namespace.
- Opt-in placement (`spec.aiGatewayMode: true`): the shared AI Gateway infra
  namespace — `redhat-ai-gateway-infra` when the operator runs in
  `redhat-ods-applications`, `odh-ai-gateway-infra` when it runs in
  `opendatahub`, and the custom resource namespace when the operator namespace is
  not one of those. In that placement the namespace must exist and carry
  `trustyai.opendatahub.io/nemo-guardrails-workload=true`.

The policy therefore follows the workload for the lifetime of the custom
resource, including a placement change between the two.

Kubernetes rejects an owner reference that crosses namespaces, so one of two
ownership records is used:

- Same namespace: a controller owner reference on the `NemoGuardrails` custom
  resource, which garbage-collects the policy with the operand.
- Workload namespace differs from the custom resource namespace: the
  `trustyai.opendatahub.io/owner-uid` label plus `owner-name` / `owner-namespace`
  annotations. The controller watches the policy through an explicit mapping and
  **deletes it during operand cleanup**; nothing else garbage-collects it.

In both cases the policy is reconciled declaratively: it is created, repaired
after drift, recreated after deletion, updated when the desired rules change, and
removed when the operand is removed. The controller refuses to adopt an object at
its policy name that it did not author (the
`trustyai.opendatahub.io/network-policy-managed-by: trustyai-service-operator`
marker is required).

## Target selector

`spec.podSelector` is exactly the reserved identity labels the controller stamps
on the Deployment's Pod template:

```yaml
trustyai.opendatahub.io/component: nemo-guardrails
trustyai.opendatahub.io/role: service
trustyai.opendatahub.io/owner-uid: <custom resource UID>
```

Only the Deployment template is labelled; the immutable Deployment selector is
never modified and running Pods are never edited. An existing Deployment therefore
rolls once to pick the labels up. Until that rollout completes, Pods carrying only
the previous labels are not selected by this policy (they are not denied traffic),
so an upgrade cannot strand the server.

## Ingress

Every rule names explicit peers and explicit ports. No rule allows all ports and
no rule admits an empty peer.

Ingress depends on the placement the policy was authored for, because the caller
sets are not the same:

| Profile | Placement | What is admitted |
| --- | --- | --- |
| Compatibility | Default, same namespace as the custom resource | The workload namespace on the Service's published listeners, plus the documented peers below |
| Restricted | Opt-in shared AI Gateway namespace (`spec.aiGatewayMode: true`) | The documented peers below only |

The compatibility profile exists because the default placement is the historical
one: the server shares its namespace with the workloads that call it, and those
callers are chosen by the user and cannot be enumerated from the custom
resource. Its namespace peer is recorded on the policy with its rationale and
residual risk
(`trustyai.opendatahub.io/network-policy-ingress-namespace-only-peers`), and it
grants the workload namespace the listeners the Service publishes — the public
listener and, when `security.opendatahub.io/enable-auth: "true"`, the
authenticated admin listener as well, so an upgrade does not silently remove an
endpoint that was reachable before this policy existed. It does not grant any
other namespace.

The restricted profile is the more restrictive choice and has no co-located
callers by construction: it grants the public listener only, and never the admin
listener. Selecting it means enabling `spec.aiGatewayMode`, which is the
supported administrator control for a narrower profile for this operand.

The public listener is **TCP 8000** without authentication or **TCP 8443** with
kube-rbac-proxy; the admin listener is **TCP 8444**.

Built-in peers, all documented traffic sources, in both profiles:

| Peer | Selector | Why |
| --- | --- | --- |
| MaaS payload processing | namespace `openshift-ingress` + Pods with `app in (payload-processing, payload-pre-processing)` | The AI Gateway payload-processing ext_proc Deployment is the component that posts guardrail checks to this server, and it opens matching egress to the NeMo Service. |
| MaaS payload processing, co-located | Pods with the same `app` selector, no namespace selector (restricted profile only; the compatibility profile already admits the workload namespace) | Some topologies run payload processing in the shared workload namespace itself. |
| Gateway data plane | namespace `openshift-ingress` + Pods with `gateway.istio.io/managed=istio.io-gateway-controller` | Istio-managed gateway Pods that call the server directly. This mirrors MaaS's payload-processing policy contract. |
| OpenShift router | namespace selector `network.openshift.io/policy-group=ingress` (namespace-only, with recorded rationale and residual risk) | `spec.exposeRoute` Route traffic is sourced by router Pods, for which no stable supported Pod selector exists. The peer is removed when Route exposure is disabled. |

`openshift-ingress` is the default MaaS gateway namespace
(`maas-controller --gateway-namespace`). A tenant Gateway, or payload processing,
in any other namespace is **not** in the default set and needs an explicit peer:

```yaml
spec:
  networkPolicy:
    additionalIngress:
      - namespaceSelector:
          matchLabels:
            kubernetes.io/metadata.name: redhat-ai-gateway-tenant-a
        podSelector:
          matchLabels:
            app: payload-processing
```

Peers are additive. Namespace and Pod selectors are ANDed in one peer and both
must positively identify callers: a nonempty `matchLabels` value, an `Exists`
expression, or an `In` expression with a nonempty value. Empty and
exclusion-only selectors are rejected by the CRD validation rules at admission,
as is a namespace selector that relies on the universal `Exists` form of
`kubernetes.io/metadata.name`. The selector collections are bounded
(`maxProperties: 32`, `maxItems: 32`) because the API server rejects a CEL rule
that iterates an unbounded collection under `additionalIngress` as over its cost
budget. The controller revalidates the same contract on every reconcile and
still fails closed - writing no policy and reporting
`NetworkPolicyReconcileFailed` on the custom resource - for anything that reaches
it, including a selector stored before this validation existed. Namespace and
Pod labels are trust inputs, not cryptographic identities; application
authentication remains necessary.

`allowedConsumers` authorises AIGuardrail attachments, not network sources, and is
deliberately not translated into peers.

Kubelet probes are node-originated. Standard `NetworkPolicy` cannot select a node
peer, so probe traffic is left to the CNI's node exemption rather than compensated
for with a broad ingress rule.

## Egress

Egress is the documented **AllowAll** compatibility profile:

```yaml
policyTypes:
- Egress
egress:
- {}
```

Outbound destinations come entirely from user-mounted configuration (model
providers, rails services, action and tool endpoints, optional telemetry), so no
complete allowlist can be derived from the custom resource. This is not
exfiltration protection, and because NetworkPolicy grants are additive it can
broaden existing customer restrictions. A `Restricted` profile requires
CR-driven destination inputs and remains a follow-up decision with platform and
security owners.

Both intents are recorded on the object, so the accepted risk is auditable where
the policy runs rather than only in the controller source:
`trustyai.opendatahub.io/network-policy-egress-mode`, `-egress-rationale`,
`-egress-residual-risk`, `-egress-behavior`, and
`trustyai.opendatahub.io/network-policy-ingress-namespace-only-peers` for the
namespace-only peer record. They are regenerated on every reconcile, so a hand
edit to them is repaired like any other drift.

## Status, validation and limits

- A policy creation or repair failure is reported as a `NetworkPolicy`
  condition (`NetworkPolicyReconcileFailed`) and marks the operand not ready
  instead of failing silently; a successful reconcile clears it.
- Deleting the policy triggers a reconcile that recreates it. Deleting the
  operand removes the policy in every namespace the controller may have used: by
  garbage collection in the same-namespace placement, and by explicit
  controller-driven deletion in the shared AI Gateway placement, where
  Kubernetes does not permit the owner reference that garbage collection needs.
- Unit tests cover peer construction for both placements and profiles,
  authentication-dependent ports, Route exposure changes, drift repair,
  deletion/recreation, namespace authority, ownership records and cleanup.
  `make policy-test` and `make policy-check` validate the RBAC contract for the
  rendered manifests.
- A `NetworkPolicy` object does not prove CNI enforcement: live qualification of
  gateway, payload-processing, router and monitoring paths in each supported
  profile and placement is still required, as is recording the additive grants
  that other policies may contribute.
- An upgrade from a release without this policy adds ingress restrictions to a
  running server. Callers that reach it from another namespace — and are not one
  of the documented peers — need `spec.networkPolicy.additionalIngress`.
- Egress restrictions on payload-processing are owned by the AI Gateway
  controller, which opens egress to the NeMo Service namespace and Pod selector;
  this document only covers the NeMo side of that path.

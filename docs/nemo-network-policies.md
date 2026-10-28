# NeMo Guardrails NetworkPolicies

The controller always reconciles an identity-scoped policy before selecting its
workload Pods. There is no opt-out. A policy API object does not prove CNI
propagation or effective isolation; live qualification remains necessary.

## Ingress

- With Route exposure enabled, OpenShift ingress-policy-group namespaces may
  reach the public listener. This is a documented namespace-only exception.
- Independently of Route exposure, Istio-managed gateway Pods in
  `openshift-ingress` may reach that listener. The peer ANDs
  `kubernetes.io/metadata.name=openshift-ingress` on the namespace with
  `gateway.istio.io/managed=istio.io-gateway-controller` on the Pod, matching
  MaaS's payload-processing NetworkPolicy contract.
- Custom callers require explicit namespace AND Pod selectors:

```yaml
spec:
  networkPolicy:
    additionalIngress:
      - namespaceSelector:
          matchLabels:
            kubernetes.io/metadata.name: custom-gateway
        podSelector:
          matchLabels:
            app.kubernetes.io/name: my-client
```

Peers are additive. The controller fixes destination ports to TCP 8000 without
authentication or TCP 8443 with authentication; it does not grant admin-listener
access. Selectors must satisfy the shared policy validator: they must positively
identify callers, not match everything using empty or exclusion-only selectors.
Namespace and Pod labels are trust inputs, not cryptographic identities.
Application authentication remains necessary.

`allowedConsumers` authorizes AIGuardrail resource attachments; it does not
identify network sources and is deliberately not translated into peers.

## Upgrade and compatibility

Supported managed gateway and router paths are permitted by default. Arbitrary
existing Service callers are NOT automatically preserved: administrators must
configure additionalIngress before upgrading. Disabling Route exposure removes
only the router exception, not gateway or custom caller access.

Egress remains a documented AllowAll compatibility profile because model, rails,
action and tool endpoints come from user configuration. This is not exfiltration
protection and can broaden existing customer restrictions: NetworkPolicy grants
are additive.

## Outstanding integration gate

PR #973 is unmerged and changes workload namespace, naming, ownership and
migration. This branch targets current main's same-namespace deployment model.
It must NOT be advertised as compatible with #973 until reconciliation, Pod
watches, owner verification, policy authority and migration cleanup are adapted
to that PR's actual workload identity and covered by integration tests. A
cross-namespace ownerReference to the namespaced NeMo CR is invalid; changing
only the NetworkPolicy namespace is not a valid integration.

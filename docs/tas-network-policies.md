# TrustyAIService operand NetworkPolicy

The TAS controller always reconciles a NetworkPolicy for each persisted
TrustyAIService before creating or updating its Deployment. The policy is owned
by that CR and selects exactly its reserved component, role and owner-UID labels.
These labels are selection metadata, not application authorization.

## Traffic contract

All listed ports are TCP destination **Pod** ports, after Service translation.

| Caller | Required source identity | Pod ports |
| --- | --- | --- |
| OpenShift router | Namespace `network.openshift.io/policy-group=ingress` | 8443 |
| Dashboard backend | Operator applications namespace AND Pod `deployment=odh-dashboard`, `app.kubernetes.io/part-of=odh-dashboard`, or the corresponding `rhods-dashboard` pair | 8443 |
| Prometheus | Namespace `network.openshift.io/policy-group=monitoring` AND Pod `app.kubernetes.io/name=prometheus`, `app.kubernetes.io/component=prometheus` | 8443, 8080 |
| ModelMesh | Same namespace as TAS; Pod `modelmesh-service=modelmesh-serving` | 4443 |
| KServe Raw/Standard | Same namespace as TAS; Pod with `serving.kserve.io/inferenceservice` | 4443 |
| KServe Serverless/Knative | Same KServe peer; operator ConfigMap `kServeServerless=enabled` | 8080, in addition to 4443 |

The TLS Service maps 443 to kube-rbac-proxy on 8443. The internal Service maps
443 to the profile-configured internal TLS proxy on 4443 and 80 to 8080. The
application no longer owns the 4443 listener or its serving keys; see
[TAS TLS termination](tas-tls-termination.md) for the consumer/auth boundary and
image qualification requirements. Local ServiceMonitor scraping uses the authenticated
listener; central scraping uses HTTP on 8080. No ingress grant is made for the
proxy health listener or the loopback-only upstream.

Router ingress deliberately uses a namespace-only peer: no stable supported
router Pod identity is assumed. Every Pod in a namespace carrying that label
can reach 8443. The reconciled annotation records this rationale and risk.
Dashboard peers require both labels in the **same** peer as their namespace
selector, not independent OR-ed peers. The namespace is the TAS operator's
configured namespace, matching supported ODH/RHOAI applications placement.

Other direct in-cluster callers are not automatically authorized. Before
upgrading a deployment with custom clients, qualify their source identities and
provide a separately managed, narrowly scoped ingress policy. NetworkPolicy
rules are additive; a separate broad grant can defeat this policy's restrictions.

## Egress

The policy explicitly declares both Ingress and Egress and a documented
AllowAll egress rule. DNS, kube-rbac-proxy TokenReview/SubjectAccessReview calls,
and optional Secret-configured remote databases require outbound connectivity.
A complete stable allowlist has not been established for every supported mode.
The managed annotations record the compatibility rationale and residual risk.
This is not exfiltration protection: all containers share the Pod network and
other ordinary NetworkPolicies cannot narrow an allow-all grant. Restricted
egress needs a separately qualified destination contract.

## Ownership, upgrades and repair

The controller verifies Deployment ownership before reconciling its policy and
refuses foreign or unowned policy objects at the generated name. Existing Pods
receive reserved labels only after policy reconciliation and verification of
the Deployment -> ReplicaSet -> Pod controller-reference names, kinds, groups
and UIDs. Unowned or stale selector-matching Pods are skipped; genuine API
failures are returned for retry. Deployment template labels are reconciled
without changing immutable Deployment selectors.

Owned-policy watches repair rule drift and deletion. Metadata-only Pod label
watches map back to the owning TAS and repair identity drift without introducing
a full-Pod informer cache; ownership lookups use the uncached API reader. Kubernetes garbage collection owns
policy cleanup when the CR is deleted. Policy-before-label ordering establishes
API ordering only; CNI propagation is asynchronous and requires live testing.

## Caller evidence and tests

TAS listener, Service, ServiceMonitor and logger-URL contracts live in
`controllers/tas/templates/service/` and `controllers/tas/inference_services.go`.

Dashboard evidence is pinned to public source revision
[`9070a32842752a08e0b18ecbd2ad1c72c535e0fc`](https://github.com/opendatahub-io/odh-dashboard/tree/9070a32842752a08e0b18ecbd2ad1c72c535e0fc):

- `backend/src/routes/api/service/trustyai/index.ts`: selects the TAS `-tls`
  Service on 443; `backend/src/utils/proxy.ts` builds its direct cluster URL.
- `manifests/base/deployment.yaml`, `manifests/odh/kustomization.yaml`, and
  `manifests/rhoai/{deployment,kustomization}.yaml`: Dashboard Pod identities.
- `dashboard-operator/internal/controller/dashboard_controller.go`: renders
  Dashboard manifests with `WithNamespace(r.ApplicationsNamespace)`.
- `manifests/base/networkpolicy.yaml`: already permits cross-namespace TCP 8443
  egress, so TAS destination-side ingress is the missing half.

The `controllers/tas/testdata/*-dashboard.json` fixtures contain Pod-template
metadata extracted from actual `kustomize build manifests/{odh,rhoai}` output
at that revision. Selector tests match both variants, reject wrong namespaces
and labels, and deny Dashboard access to the unauthenticated/logging listeners.
They pin the reviewed contract offline; refresh fixtures and review selectors
when the caller contract changes.

Unit tests also cover policy shape, serverless logging, persisted identity,
owned-Pod migration, foreign workloads, drift, ordering and Deployment paths.
An envtest manager test covers policy creation, workload selection, Pod label
drift repair through metadata-only events, policy drift repair and recreation. RBAC tests check both generated TAS manifests and module
permission delegation. These tests do not prove CNI packet enforcement or live
ODH/RHOAI connectivity; those remain cluster qualification work.

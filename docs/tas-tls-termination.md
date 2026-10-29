# TAS TLS termination and refresh

## Topology

Both TLS listeners now use the resolved API Server profile through the operator's
kube-rbac-proxy arguments (including versions, applicable ciphers and group IDs):

| Service / consumers | Listener | Certificate | Backend |
|---|---|---|---|
| `<name>-tls:443`: Route, authenticated API clients and local ServiceMonitor | `kube-rbac-proxy:8443` | `<name>-tls` | Loopback application API `127.0.0.1:8081` |
| `<name>:443`: existing internal KServe/ModelMesh HTTPS consumers | `internal-tls-proxy:4443` | `<name>-internal` | Health/consumer server `127.0.0.1:8080` |

Service names, target ports, serving-certificate annotations and logger certificate
mounts are unchanged. The new terminator uses the same configured proxy image as the
external proxy, adding a 50m CPU/32Mi memory request (100m/64Mi limit). NetworkPolicy
already grants the relevant model-serving peers 4443 and monitoring peers 8443; no
new ingress grant is needed. KServe/ModelMesh delivery and cert trust still need
validation in the actual deployment modes.

The application no longer mounts `<name>-internal` serving keys. Operator-owned
`TLS_CERT_FILE`/`TLS_KEY_FILE` point at an unmounted reserved path and
`TLS_POLICY=system`, `TLS_REQUIRED=false` select the service's optional HTTP-backend
mode. This is **not** a missing-certificate fallback for an expected HTTPS listener:
only the sidecars are intended to listen for TLS, and they require mounted serving
secrets and explicit certificate/key paths. Missing/invalid sidecar material must
not result in self-signed or plaintext operation.

The service's existing health/consumer HTTP listener on 8080 and optional serverless
HTTP consumer path remain unchanged. This PR does not claim all plaintext surfaces
are eliminated. The main HTTP API backend remains loopback-only.

## Authentication boundary

The external 8443 proxy remains authenticated/authorized for every path. Local
metrics scraping retains its dedicated token Secret, service-CA ConfigMap, HTTPS
scheme, and `<name>-tls.<namespace>.svc` serverName, without verification bypasses.
The local monitor already targeted the external proxy in the audited source; it
does not need to move to the new internal proxy.

The internal proxy preserves the previously unauthenticated logger contract only
for exact paths `/` (CloudEvents) and `/consumer/kserve/v2`, plus `/q/health/live`
and `/q/health/ready` for kubelet probes. Its upstream is **8080, not the main API on
8081**. No wildcard auth bypass is configured. Other paths retain the existing
resource-based proxy authorization. The old direct internal listener allowed
unauthenticated access; these flags do not relax the external API's authentication.
NetworkPolicy remains part of the internal consumer boundary. Authentication of
logger producers is not newly introduced by this TLS change.

Kubelet HTTPS probes use ordinary kubelet semantics; they are not monitor trust
configuration. No `insecureSkipVerify` is added to any monitor.

## Existing workloads and policy refresh

On bootstrap the operator validates observed policy or the recipe-permitted
Intermediate/NoOpinion fallback before publishing proxy arguments. Transient startup
degradation retains a seeded recovery watcher; authorization/unexpected failures
remain fatal. On a profile/adherence change, the watcher restarts the
manager. Ordinary reconciliation now patches TLS-owned Pod-template fields on
existing TAS Deployments, which was previously missing outside migration/tuning
updates. Changes to proxy arguments trigger a Kubernetes rollout. Effective policy
that produces identical arguments does not need another rollout.

The patch removes native serving-key mounts and stale native-policy inputs, sets
reserved backend TLS/port environment variables, updates external proxy arguments
and creates/refreshes the internal proxy. It preserves the immutable selector,
user rollout annotations, unrelated application environment, storage settings and
application image. Sidecar defaulted probes do not cause repeat updates. Unowned
Deployments are rejected. Migration paths render the same topology directly.

The kube-rbac-proxy ConfigMap now contains only authorization. Previously inert
`upstreamConfig`/`tlsConfig` blocks (including static-RSA cipher settings) are removed;
upstreams and TLS controls are command-line owned. Existing ConfigMaps converge via
the existing reconciliation helper.

## Merge / release gates and separate monitoring hardening

- Deploy a Python service image containing
  [trustyai-service #410](https://github.com/trustyai-explainability/trustyai-service/pull/410),
  especially its correct loopback bind in optional HTTP mode. Older Python images
  can retain Hypercorn's default port when native TLS is absent. Do not roll this
  topology out against an unqualified older image. This PR does not publish or pin
  a new service image.
- Qualify the actual shipped proxy digest for `--ignore-paths`, profile cipher/group
  flags and hybrid group support. Source inspection of the OpenShift proxy confirms
  the flags, but a mutable image tag is not runtime evidence (gap G12 / planned PR 8).
- Verify actual negotiated TLS, certificate rotation, HTTPS logger delivery and
  Prometheus scraping with target images and service-CA on an OpenShift cluster.
- **The shared central ServiceMonitor still scrapes HTTP on 8080.** This is not a
  TLS-profile violation solely because it uses HTTP, and is not a mandatory Strict
  release gate without a separate transport-security requirement. Retiring/migrating
  it and legacy HTTP scrape annotations is optional monitoring hardening. Existing
  HTTPS monitors must still verify CA/SAN and never use verification bypasses.

This PR closes the native-Python profile-enforcement design gap with sidecar
termination and fixes existing-workload TLS rollout wiring. It is not by itself a
complete Strict-release claim; image qualification and runtime acceptance remain
mandatory. Shared plaintext monitoring migration is a separate policy decision,
not an implied TLS-adherence requirement.

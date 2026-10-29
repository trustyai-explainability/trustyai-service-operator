# Managed EvalHub TLS contract

## Supported topology

The operator deploys **proxy-terminated TLS**, not native API/MCP HTTPS:

| Public listener | TLS owner | Backend |
|---|---|---|
| EvalHub API Service, 8443 | kube-rbac-proxy with instance serving certificate | HTTP `127.0.0.1:8080` |
| Optional MCP Service, 8443 | separate kube-rbac-proxy with MCP serving certificate | HTTP `127.0.0.1:8445` |

Both proxies receive the same validated effective arguments from
`pkg/tls.CurrentProxyTLSArguments()`. Cluster profile/adherence resolution, including
Legacy/no-opinion handling and classified startup fallback, is owned by the operator.
The watcher restarts the operator on an effective profile/adherence change; ordinary
API and MCP Deployment reconciliation refreshes their proxy arguments in the Pod
template. Changed arguments cause a rollout; unchanged effective arguments do not
change the Pod template. The existing controller owns/rebuilds these Deployment specs;
this work does not introduce TAS's separate field-limited patch implementation.

Backend HTTP is intentional, not an adherence failure. Do not give a plaintext
backend a fake profile or set required native TLS while a proxy still forwards HTTP.
Application containers do not mount the proxies' serving-key Secrets. Existing trust
CA/client-token mounts are separate from serving-key ownership and remain unchanged.

## Relationship to upstream native TLS (planned work packages 6/7)

[eval-hub/eval-hub#1210](https://github.com/eval-hub/eval-hub/pull/1210) adds generic
native API/MCP TLS controls for separately configured direct-TLS deployments. It does
not create an operator-native-TLS mode, change proxy upstreams or make TLS equivalent
to caller authentication. Cluster API and MCP `rbac-proxy` modes still trust forwarded
identity from an authenticating boundary.

This operator implements work package 7's **proxy-only managed contract**. It does
not expose a new native-HTTPS CR API or propagate native policy inputs into HTTP
backends. Both current and #1210-capable images retain managed HTTP behavior:

- API binds loopback; generated service configuration has explicit empty native
  certificate/key paths and no environment mapping for those fields.
- MCP CLI fixes loopback/config/auth settings for both HTTP transports; generated
  config also has empty serving paths.
- Both application containers explicitly get `TLS_REQUIRED=false` and empty native
  certificate/key environment values. Version/cipher/group controls are **absent**,
  not empty (the upstream implementation rejects empty policy inputs).
- API `EVAL_HUB_CONFIG_DIR` is operator-owned; CR env cannot redirect configuration
  away from the generated mount.

Qualified images must not bake in `TLS_MIN_VERSION`, `TLS_CIPHERS` or `TLS_CURVES`
(or otherwise enable native TLS outside this configuration). Kubernetes omission
cannot unset image `ENV` values; do not claim arbitrary replacement images are safe.
No new image is built/published/pinned by this PR. Native controls are not required
for this managed mode, so an image containing #1210 is not a new prerequisite.

## Unsupported overrides and error handling

These names are operator-owned in `spec.env` and enabled `spec.mcp.env`:

- `TLS_REQUIRED`, `TLS_MIN_VERSION`, `TLS_CIPHERS`, `TLS_CURVES`
- `TLS_CERT_FILE`, `TLS_KEY_FILE`
- `EVALHUB_TLS_CERT_FILE`, `EVALHUB_TLS_KEY_FILE`

Any explicitly supplied reserved entry, including an empty value, `false`, duplicate
entry or `valueFrom` reference, is rejected rather than ignored or resolved from a
Secret/ConfigMap. Disabled MCP configuration is not evaluated until enabled.

Validation occurs before dependent-resource reconciliation and also in Deployment
builders. Reconciliation sets `Ready=False`, reason `UnsupportedNativeTLS`, phase
`Error`, emits a Warning event and returns an error. Existing workloads are not
rewritten on that invalid spec. Remove the override to resume normal reconciliation.
Previously ignored certificate env entries are now a visible validation error;
other protected host/port overrides retain existing filtering semantics. Unrelated
application and outbound-client settings remain available.

Native HTTPS for operator-managed backends would be a separate topology/API feature:
it must define certificates, trusted upstream verification, probes and auth boundary,
convert only representable effective settings for both listeners, and test updates.
It must not be enabled implicitly by an arbitrary CR environment variable.

## Verification and remaining boundaries

Tests cover literal/reference overrides for every reserved name in both scopes;
status reporting with unchanged existing workloads; generated config, loopback,
serving-key isolation and normal env preservation; Intermediate, Legacy, Strict
Modern and Custom proxy settings; both MCP transports; updates to existing API/MCP
Deployments and unchanged-policy Pod-template stability.

The full EvalHub envtest suite exercises Kubernetes objects/controllers, not the
actual containers. Image capability qualification, real negotiated TLS, service-ca
rotation and live traffic/scrape tests remain separate acceptance gates. Metrics
transport/authentication, job-sidecar transport and the paused metrics-hardening work
are unchanged; plaintext metrics are not automatically a TLS-profile violation.

# TLS policy resolution at startup and runtime

The operator reads `apiservers.config.openshift.io/cluster` before creating the
manager or reconciling workloads. Profile and `spec.tlsAdherence` come from one
response, including on dependencies without a typed adherence field. Only validated
TLS options and proxy arguments are returned for publication by the caller.

## Startup error policy

| Read result | Behaviour |
|---|---|
| Success | Resolve/validate both server options and proxy arguments. `APIAvailable=true`, `AdherenceFetched=true`. |
| NoMatch / NotFound | Use validated Intermediate defaults; skip the OpenShift watcher. `APIAvailable=false`, `AdherenceFetched=false`. |
| ServiceUnavailable / TooManyRequests / Timeout / ServerTimeout / deadline exceeded | Retry within the startup budget, then use validated Intermediate/NoOpinion defaults and register the recovery watcher. `APIAvailable=true`, `AdherenceFetched=false`. |
| Forbidden / Unauthorized | Fail startup; verify configuration API RBAC. |
| Cancellation, malformed/unusable settings, or other unexpected errors | Fail startup. Explicit cancellation is not transient degradation. |

This follows the supplied OCP classified-error recipe: transient degradation is an
intentional availability/recovery tradeoff, even if the temporarily unknown cluster
policy is stricter. `APIAvailable` means “observed or potentially available; watch for
recovery,” not proof that bootstrap fetched a resource. `AdherenceFetched` distinguishes
an observed absent/NoOpinion policy from an unresolved read. A successful response
without the adherence field is an observed NoOpinion, not a failed fetch.

There is a **10-second total request/backoff budget**, with at most five
application-level attempts, exponential backoff starting at 250ms, factor 2 and 10%
jitter. Client-go can also retry internally within that context. A bootstrap deadline
is eligible for Intermediate fallback; explicit caller cancellation remains fatal.
Every fallback goes through normal validation and propagates conversion errors.
The resolver never publishes global arguments itself and never mistakes pre-existing
process defaults or a previously published strict value for a freshly observed policy.

Forbidden is fatal under the supplied contract; it is not a non-OpenShift detection
mechanism. Plain Kubernetes generally returns NotFound for the missing resource,
but restricted authorization can return Forbidden before determining API absence.
This behaviour is explicit, not silently weakened for portability.

## Watcher and recovery

On transient startup degradation the initial profile is **explicit Intermediate**,
not nil/zero, with unresolved adherence represented by NoOpinion and
`AdherenceFetched=false`. The existing manager wiring registers the watcher because
`APIAvailable=true`, using a fresh manager context rather than the expired bootstrap
context. The watcher reads both profile and adherence through the dynamic client
installed by `SetupWithManager`; it does not freeze startup adherence.

Runtime transient failures retain the running last-known-good state and requeue after
five seconds. Successful reads compare normalized effective profiles and adherence:
nil, empty and explicit Intermediate profiles are equivalent. Recovery to an unchanged
Intermediate/NoOpinion needs no restart; recovery to Modern, a different Custom spec
or Strict adherence cancels the manager to restart and re-resolve policy. Invalid
profile specs also trigger bootstrap validation rather than being silently ignored.
Every replica watches (`NeedLeaderElection=false`). Forbidden/Unauthorized restart
the process, whose bootstrap read fails if permissions have not recovered.

A changed operand Pod template must actually be reconciled after restart to update
its listeners; watcher registration alone does not prove every workload rolls out.
Image, TLS negotiation and rollout acceptance tests remain separate release gates.

## Operator integration and dependency decision

This operator uses controller-runtime **v0.22.5**, not the v0.17.0 recorded in the
older repository overview. Its current compatibility path is:

- `openshift/api v0.0.0-20260130140113-71e91db96ffc` for typed profile/watch objects.
- `controller-runtime-common v0.0.0-20260204102813-ab84f25b3496` for
  `GetTLSProfileSpec` and `NewTLSConfigFromProfile` conversion helpers.
- A local atomic profile/adherence read, validation layer, proxy-argument resolver
  and restart watcher. The API pin predates typed `TLSAdherencePolicy`, so the
  dynamic read deliberately preserves that field rather than silently dropping it.

Keep this compatibility path for now. The supplied guidance's newer CRC integration
requires controller-runtime v0.23.3 or later; the inspected CRC version
`v0.0.0-20260428152732-64ee174f5e2e` requires v0.23.3 and Kubernetes v0.35.2.
CRC `v0.0.0-20261026124434-bdfc411d93cd` requires controller-runtime v0.25.1.
An upgrade is not a TLS-only dependency bump: `cmd/main.go` has two existing
`NewWebhookManagedBy(mgr).For(&Type{})` call sites affected by the newer webhook API.

Additionally, the inspected newer CRC watcher returns authorization read errors to
the controller for retry, whereas this operator restarts on Forbidden/Unauthorized
to fail closed on the next bootstrap read. Replacing the watcher unconditionally
would change that approved-in-code behaviour. Separate profile/adherence fetch
helpers also do not preserve our single-response snapshot by themselves.

A future full CRC migration must therefore upgrade/test the framework and affected
webhook registrations, retain bounded retries and classified Intermediate fallback
with unresolved adherence/recovery watching, fail closed on authorization/unexpected
errors, and preserve the
single-response profile/adherence snapshot and strict validation, retain proxy
argument generation and Legacy semantics, and preserve watcher behaviour on every
replica (including authorization failures). Merely replacing the Go TLS callback
is not a complete migration. No dependency versions are changed by this integration
refactor, and the owning RHOAI CSV support annotation is not changed here.

### Listener wiring and ALPN

`configureManagerTLS` is the single wiring point for operator metrics and webhooks.
It applies the resolved callbacks to both listeners with independent option slices,
without changing metrics secure-serving, certificate, authentication or authorization
settings. Only TAS/EvalHub enable the conversion webhook; the refactor preserves that
condition and port 9443. API Server types remain registered in the manager scheme.

The profile source is the **API Server**, not the ingress controller: these are
internal listeners, not the first external client-facing TLS endpoint. The operator
itself follows the profile regardless of adherence; operand proxies retain their
existing Intermediate policy under Legacy/no opinion and enforce the profile under
Strict (including unknown future adherence values).

ALPN is independent of the cluster profile. This operator deliberately advertises
`h2` and `http/1.1`. The local `setALPN` callback remains inline because the pinned
CRC predates `SetNextProtos` (PR #21 / `f9e4228`). When upgrading to a compatible
CRC version containing those helpers, use `SetNextProtos(HTTP2NextProtos...)` and
retain the listener tests. HTTP/1.1-only can be an explicit component policy; it is
not automatically a profile violation.

### Installation RBAC regression guard

Source tests check get/list/watch on `config.openshift.io/apiservers` in the
standalone role, modular operator role, and modular manifest-template role. Rendered
tests cover base, ODH, RHOAI, ODH-Kueue, MCP guardrails, testing, EvalHub-only, LMES
and the modular operator default installation. Each rendered installation must have
both the permissions and a ClusterRoleBinding to a ServiceAccount. These assertions guard the currently
shipped RBAC layout; they do not establish that OpenShift permissions belong in
common/non-OCP installs. A separate Recipe 3 migration must move the role/binding
and revise common-versus-OpenShift test expectations together. CI explicitly
runs these tests with the repository's pinned Kustomize binary, so source changes
and regenerated installation resources are checked together. These are structural
manifest checks, not a live-cluster authorization test.

## Focused verification

```sh
go test ./pkg/tls/...
go test -race ./cmd ./pkg/tls/...
make kustomize
KUSTOMIZE_BINARY="$PWD/bin/kustomize" go test ./cmd -run '^TestTLS' -count=1
```

Tests cover classified errors and retry exhaustion, validated fallback in cold/warm
process state, strict recovery, unresolved versus observed adherence, fresh runtime
contexts, normalized profiles, authorization rejection and runtime last-known-good
preservation. These are not live-cluster TLS negotiation/operand-rollout tests.

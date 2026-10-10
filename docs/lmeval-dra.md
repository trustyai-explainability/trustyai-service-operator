# Dynamic Resource Allocation for LMEvalJob

LMEvalJob supports opt-in Kubernetes Dynamic Resource Allocation (DRA) through
`spec.pod.resourceClaims`. These declarations are copied into the evaluation
Pod's `spec.resourceClaims`. The main evaluation container and user-supplied
sidecars opt in separately through their `resources.claims` entries.

## Prerequisites

- A Kubernetes/OpenShift release with the required DRA APIs and features enabled.
- A compatible DRA driver and configured DeviceClass/device resources.
- A ResourceClaimTemplate or ResourceClaim created **in the LMEvalJob namespace**.
- An evaluation or model-server image with the libraries required to use the device.

The operator does not install a driver, create templates, discover devices, or
check template existence. Cluster scheduling/allocation failures remain visible
through the Pod and claim events/status. Referencing a template is not proof that
an allocation succeeded.

## Main-container example

Create a ResourceClaimTemplate named `evaluation-gpu` separately, with a device
request named `gpu` and selectors/constraints appropriate to the installed driver.
Then submit the example in
[`config/samples/lmes_v1alpha1_lmevaljob_dra.yaml`](../config/samples/lmes_v1alpha1_lmevaljob_dra.yaml).
Its allocation-related configuration is:

```yaml
spec:
  pod:
    resourceClaims:
      - name: evaluation-device
        resourceClaimTemplateName: evaluation-gpu
    container:
      resources:
        requests:
          cpu: "1"
          memory: 4Gi
        limits:
          memory: 8Gi
        claims:
          - name: evaluation-device
            request: gpu
```

`evaluation-device` is a **pod-local alias**, not the template name. A container
claim reference must match a declared alias. Omit `request` to expose all requests
from the claim; set it to a request or `request/subrequest` to restrict consumption
when supported by the cluster. The operator validates the reference syntax but
cannot verify request existence without reading the actual claim/template.

To use an existing claim, replace `resourceClaimTemplateName` with
`resourceClaimName`. Exactly one source must be set. The existing claim is managed
separately and is not deleted by this operator. For template-backed claims,
Kubernetes creates the Pod's claim and handles its lifecycle; the template itself
remains independently managed.

## GPU model-server sidecar

For remote-completions evaluation against an in-Pod model server, declare the
claim at the same pod level but put the consumer reference on that sidecar:

```yaml
spec:
  pod:
    resourceClaims:
      - name: model-device
        resourceClaimTemplateName: evaluation-gpu
    sideCars:
      - name: model-server
        image: <your-qualified-model-server-image>
        resources:
          claims:
            - name: model-device
```

Configure the model-server command, ports, volumes, and evaluation `modelArgs`
for that server separately. This fragment only illustrates device placement and
is not a complete model-serving example. It does not allocate a device to the main
evaluation container. The operator's driver-copy init container never receives
these claim references automatically. Two containers may explicitly reference
the same pod-local claim when sharing that allocation is supported.

## Validation, lifecycle, and compatibility

- CR admission rejects duplicate aliases, missing/both sources, and empty source
  names. Controller validation checks DNS names, declared container references,
  and request/subrequest syntax before initial Pod creation and resume.
- Initial controller validation failures are recorded in the existing failed-job
  status path. Invalid resume configuration returns an error and prevents launch;
  correct the suspended job configuration before resuming it.
- Resource declarations and container configuration are deep-copied; generated
  Pod changes do not mutate the input CR.
- Existing evaluation Pods are not patched when claim configuration changes.
  Configure allocation before launch. To change allocation for ongoing work,
  use an appropriate suspend/resume lifecycle or create a new LMEvalJob; any new
  Pod uses the current declarations. Suspending deletes the old Pod and may
  release its template-created claim.
- With no DRA fields, existing CPU/memory and extended-resource GPU settings are
  unchanged. There is no automatic fallback from failed DRA allocation to a
  device-plugin allocation. Do not request the same intended GPU through both
  DRA and extended resources; the operator cannot infer device equivalence.
- This field-propagation change does not qualify Kueue integration. Prefer
  template-backed claims for queued execution and verify the exact supported
  Kueue/platform/driver combination separately.

## Verification boundaries

Unit tests cover JSON/YAML serialization, optional inputs, deep-copy isolation,
main/sidecar propagation, legacy resources, and validation. Envtest checks stored
CR fields, generated Pods, schema rejection, failure status, and preservation of
an existing Pod's claim configuration.

Controller CI runs both the existing Kubernetes 1.29 baseline and a Kubernetes
1.34 DRA baseline. The native-Pod propagation test explicitly skips servers older
than 1.34; serialization, CR schema, validation, and legacy tests still run there.

Envtest does **not** run a scheduler, kubelet, DRA driver, or garbage collector.
Real support qualification must separately prove allocated claims, device
visibility in the intended container, CUDA/accelerator execution, evaluation
results, cleanup, failure diagnostics, and queued execution where applicable.

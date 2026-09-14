# TrustyAI Operator Module

The TrustyAI Operator Module is the platform-facing controller for the TrustyAI
component. It manages the `TrustyAI` cluster-scoped resource used by the
OpenDataHub/RHOAI platform operator and runs the existing TrustyAI workload
operator as the component implementation.

## How it works

When a `TrustyAI` resource named `default-trustyai` has
`spec.managementState: Managed`, the module controller:

1. Checks the required platform dependencies, including Prometheus.
2. Adopts compatible resources created by the in-tree TrustyAI component when
   necessary.
3. Renders and applies the workload-operator manifests bundled in the module
   image. The manifests are the contents of the repository's top-level
   `config/` directory, copied to
   `trustyai-operator-module/config/manifests-template/` at build time.
4. Reconciles the TrustyAI DSC ConfigMap and module status conditions.

Setting `spec.managementState` to `Removed` stops reconciliation and removes
the resources owned by the module. An empty `spec.enabledServices` object
enables all TrustyAI services; individual services can be selected with the
`tas`, `evalHub`, `gorch`, `lmes`, and `nemoGuardrails` fields.

## Installation

The commands below install the module controller into the current Kubernetes
cluster. They require `kubectl` and `kustomize` (or a `kubectl` version that
supports `apply -k`).

Build and publish the module image, replacing the image name with a registry
where the cluster can pull it:

```shell
IMAGE=quay.io/<organization>/trustyai-operator-module-controller:<tag>
make docker-build-tom TRUSTYAI_MODULE_IMG="$IMAGE"
make docker-push-tom TRUSTYAI_MODULE_IMG="$IMAGE"
```

Set the image in the module deployment and apply its manifests:

```shell
make deploy-tom TRUSTYAI_MODULE_IMG="$IMAGE"
```

The module controller is installed in the current `kubectl` namespace because
the base manifests do not set a namespace. After the controller is ready,
create or update the platform component resource
with the platform operator. For a standalone test installation, the equivalent
resource is:

```yaml
apiVersion: components.platform.opendatahub.io/v1alpha1
kind: TrustyAI
metadata:
  name: default-trustyai
spec:
  managementState: Managed
```

Apply it with `kubectl apply -f <file>`. The module controller then deploys the
workload operator and its selected TrustyAI services. Inspect progress with:

```shell
kubectl get trustyais default-trustyai -o yaml
kubectl get pods -A -l app.kubernetes.io/part-of=trustyai-service-operator
```

To remove the module controller and its CRD, run:

```shell
make undeploy-tom
```

Before building the image after changing the workload operator manifests,
refresh the bundled copy with:

```shell
make sync-trustyai-module-manifests
```

CI verifies that this bundled copy remains synchronized with `config/`.

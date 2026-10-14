# Syncing eval-hub providers and collections to the operator

1. Run the script that syncs from [eval-hub](https://github.com/eval-hub/eval-hub)

   ```shell
   ./hack/sync-evalhub-providers.sh
   ```

   This fetches provider and collection YAML files from the [eval-hub upstream config](https://github.com/eval-hub/eval-hub/tree/main/config), wraps each in a ConfigMap manifest, substitutes provider images with kustomize variables, and regenerates `kustomization.yaml`.

1. Update the files for new providers

   If the PR contains new providers then the image definition needs to be updated as described below:

      1. Add an image entry in [kustomization.yaml](../../base/kustomization.yaml) - see the existing entries for an example:

         ```yaml
         - name: evalhub-provider-databench-image
           objref:
             kind: ConfigMap
             name: config
             apiVersion: v1
           fieldref:
             fieldpath: data.evalhub-provider-databench-image
         ```

      1. Add the image to the base `params.env` file

         [../../base/params.env](../../base/params.env)

         ```shell
         evalhub-provider-databench-image=quay.io/evalhub/community-databench:latest
         ```

      1. Add the image for ODH to the `params.env` file (with a pinned version)

         [../../overlays/odh/params.env](../../base/params.env)

         ```shell
         evalhub-provider-databench-image=quay.io/evalhub/community-databench:v0.6.7
         ```

      1. Add the image for RHOAI to the `params.env` file (with a pinned version)

         [../../overlays/rhoai/params.env](../../base/params.env)

         ```shell
         evalhub-provider-databench-image=quay.io/evalhub/community-databench:v0.6.7
         ```

1. Run the script that syncs the resources to the eval-hub module

   This is a temporary script that copies the providers and collections into the eval-hub module of the operator.

   ```shell
   ./hack/sync-trustyai-module-manifests.sh
   ```

# Syncing eval-hub providers and collections to the operator

1. Run the script

```bash
./hack/sync-evalhub-providers.sh
```

This fetches provider and collection YAML files from the [eval-hub upstream config](https://github.com/eval-hub/eval-hub/tree/main/config), wraps each in a ConfigMap manifest, substitutes provider images with kustomize variables, and regenerates `kustomization.yaml`.

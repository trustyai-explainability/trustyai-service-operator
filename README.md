# TrustyAI Kubernetes Operator
[![Controller Tests](https://github.com/trustyai-explainability/trustyai-service-operator/actions/workflows/controller-tests.yaml/badge.svg)](https://github.com/trustyai-explainability/trustyai-service-operator/actions/workflows/controller-tests.yaml)
[![YAML lint](https://github.com/trustyai-explainability/trustyai-service-operator/actions/workflows/lint-yaml.yaml/badge.svg)](https://github.com/trustyai-explainability/trustyai-service-operator/actions/workflows/lint-yaml.yaml)
[![Gosec Security Scan](https://github.com/trustyai-explainability/trustyai-service-operator/actions/workflows/gosec.yaml/badge.svg)](https://github.com/trustyai-explainability/trustyai-service-operator/actions/workflows/gosec.yaml)
[![Go Report Card](https://goreportcard.com/badge/github.com/trustyai-explainability/trustyai-service-operator)](https://goreportcard.com/report/github.com/trustyai-explainability/trustyai-service-operator)


## Overview

The TrustyAI Kubernetes Operator aims at simplifying the deployment and management of the [TrustyAI service](https://github.com/trustyai-explainability/trustyai-explainability/tree/main/explainability-service) on Kubernetes and OpenShift clusters by watching for custom resources of kind `TrustyAIService` in the `trustyai.opendatahub.io` API group and manages deployments, services, and optionally, routes and `ServiceMonitors` corresponding to these resources.

The operator ensures the service is properly configured, is discoverable by Prometheus for metrics scraping (on both Kubernetes and OpenShift), and is accessible via a Route on OpenShift.

## Prerequisites

- Kubernetes cluster v1.19+ or OpenShift cluster v4.6+
- `kubectl` v1.19+ or `oc` client v4.6+

## Installation using pre-built Operator image

This operator is available as an [image on Quay.io](https://quay.io/repository/trustyai/trustyai-service-operator?tab=history). 
To deploy it on your cluster:

1. **Install the Custom Resource Definition (CRD):**

   Apply the CRD to your cluster (replace the URL with the relevant one, if using another repository):

    ```bash
    kubectl apply -f https://raw.githubusercontent.com/trustyai-explainability/trustyai-service-operator/main/config/crd/bases/trustyai.opendatahub.io_trustyaiservices.yaml
    ```

2. **Deploy the Operator:**

   Apply the following Kubernetes manifest to deploy the operator:

    ```yaml
    apiVersion: apps/v1
    kind: Deployment
    metadata:
      name: trustyai-operator
      namespace: trustyai-operator-system
    spec:
      replicas: 1
      selector:
        matchLabels:
          control-plane: trustyai-operator
      template:
        metadata:
          labels:
            control-plane: trustyai-operator
        spec:
          containers:
            - name: trustyai-operator
              image: quay.io/trustyai/trustyai-service-operator:latest
              command:
                - /manager
              resources:
                limits:
                  cpu: 100m
                  memory: 30Mi
                requests:
                  cpu: 100m
                  memory: 20Mi
    ```

   or run

   ```shell
   kubectl apply -f https://raw.githubusercontent.com/trustyai-explainability/trustyai-service-operator/main/artifacts/examples/deploy-operator.yaml   
   ```

## Usage

Once the operator is installed, you can create `TrustyAIService` resources, and the operator will create corresponding TrustyAI deployments, services, and (on OpenShift) routes.

Here's an example `TrustyAIService` manifest:

```yaml
apiVersion: trustyai.opendatahub.io/v1alpha1
kind: TrustyAIService
metadata:
  name: trustyai-service-example
spec:
  storage:
    format: "PVC"
    folder: "/inputs"
    size: "1Gi"
  data:
    filename: "data.csv"
    format: "CSV"
  metrics:
    schedule: "5s"
    batchSize: 5000 # Optional, defaults to 5000
```

You can apply this manifest with 

```shell
kubectl apply -f <file-name.yaml> -n $NAMESPACE
```
to create a service, where `$NAMESPACE` is the namespace where you want to deploy it.


Additionally, in that namespace:

* a `ServiceMonitor` will be created to allow Prometheus to scrape metrics from the service.
* (if on OpenShift) a `Route` will be created to allow external access to the service.

### Custom Image Configuration using ConfigMap
You can specify a custom TrustyAI-service image via adding parameters to the TrustyAI-Operator KFDef, for example:

```yaml
apiVersion: kfdef.apps.kubeflow.org/v1
kind: KfDef
metadata:
  name: trustyai-service-operator
  namespace: opendatahub
spec:
  applications:
  - kustomizeConfig:
      repoRef:
        name: manifests
        path: config
      parameters:
         - name: trustyaiServiceImage
           value: NEW_IMAGE_NAME
    name: trustyai-service-operator
  repos:
  - name: manifests
    uri: https://github.com/trustyai-explainability/trustyai-service-operator/tarball/main
  version: v1.0.0
```
If these parameters are unspecified, the [default image and tag](config/base/params.env) will be used.


If you'd like to change the service image/tag after deploying the operator, simply change the parameters in the KFDef. Any
TrustyAI service deployed subsequently will use the new image and tag. 

### `TrustyAIService` Status Updates

The `TrustyAIService` custom resource tracks the availability of `InferenceServices` and `PersistentVolumeClaims (PVCs)` 
through its `status` field. Below are the status types and reasons that are available:

#### `InferenceService` Status

| Status Type                   | Status Reason                     | Description                       |
|-------------------------------|-----------------------------------|-----------------------------------|
| `InferenceServicesPresent`    | `InferenceServicesNotFound`       | InferenceServices were not found. |
| `InferenceServicesPresent`    | `InferenceServicesFound`          | InferenceServices were found.     |

#### `PersistentVolumeClaim` (PVCs) Status

| Status Type      | Status Reason   | Description                        |
|------------------|-----------------|------------------------------------|
| `PVCAvailable`   | `PVCNotFound`   | `PersistentVolumeClaim` not found.  |
| `PVCAvailable`   | `PVCFound`      | `PersistentVolumeClaim` found.      |

#### Database Status

| Status Type   | Status Reason           | Description                                       |
|---------------|-------------------------|---------------------------------------------------|
| `DBAvailable` | `DBCredentialsNotFound` | Database credentials secret not found             |
| `DBAvailable` | `DBCredentialsError`    | Database credentials malformed (e.g. missing key) |
| `DBAvailable` | `DBConnectionError`     | Service error connecting to the database          |
| `DBAvailable` | `DBAvailable`           | Successfully connected to the database            |


#### Status Behavior

- If a PVC is not available, the `Ready` status of `TrustyAIService` will be set to `False`.
- If on database mode, any `DBAvailable` reason other than `DBAvailable` will set the `TrustyAIService` to `Not Ready`
- However, if `InferenceServices` are not found, the `Ready` status of `TrustyAIService` will not be affected, _i.e._, it is `Ready` by all other conditions, it will remain so.

## Contributing

Please see the [CONTRIBUTING.md](./CONTRIBUTING.md) file for more details on how to contribute to this project.

## License

This project is licensed under the Apache License Version 2.0 - see the [LICENSE](./LICENSE) file for details.
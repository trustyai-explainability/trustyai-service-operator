[![Controller Tests](https://github.com/trustyai-explainability/trustyai-service-operator/actions/workflows/controller-tests.yaml/badge.svg)](https://github.com/trustyai-explainability/trustyai-service-operator/actions/workflows/controller-tests.yaml)
# TrustyAI Kubernetes Operator

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
    kubectl apply -f https://raw.githubusercontent.com/trustyai-explainability/trustyai-service-operator/main/config/crd/bases/trustyai.opendatahub.io.trustyai.opendatahub.io_trustyaiservices.yaml
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
apiVersion: trustyai.opendatahub.io.trusty.opendatahub.io/v1
kind: TrustyAIService
metadata:
  name: trustyai-service-example
spec:
  storage:
    format: "PVC"
    folder: "/inputs"
    pv: "mypv"
    size: "1Gi"
  data:
    filename: "data.csv"
    format: "CSV"
  metrics:
    schedule: "5s"
    batchSize: 5000 # Optional, defaults to 5000
```

`mypv` must be an existing Persistent Volume (PV).

You can apply this manifest with 

```shell
kubectl apply -f <file-name.yaml> -n $NAMESPACE
```
to create a service, where `$NAMESPACE` is the namespace where you want to deploy it.


Additionally, in that namespace:

* a `ServiceMonitor` will be created to allow Prometheus to scrape metrics from the service.
* (if on OpenShift) a `Route` will be created to allow external access to the service.

### Custom Image Configuration using ConfigMap

You can configure the operator to use custom images by creating a `ConfigMap` in the operator's namespace. 
The operator only checks the ConfigMap at deployment, so changes made afterward won't trigger a redeployment of services.

Here's an example of a ConfigMap that specifies a custom image:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: trustyai-service-operator-config
data:
  trustyaiServiceImageName: 'quay.io/mycustomrepo/mycustomimage'
  trustyaiServiceImageTag: 'v1.0.0'
```

You can apply this manifest with the following command, replacing `<file-name.yaml>` with the name of your manifest file:

```shell
kubectl apply -f <file-name.yaml> -n $OPERATOR_NAMESPACE
```

Please ensure the namespace specified is the same as the namespace where you have deployed the operator.

After the ConfigMap is applied, you can then proceed to deploy the TrustyAI service.
The operator will use the image name and tag specified in the ConfigMap for the deployment.

If you want to use a different image or tag in the future, you'll need to update the ConfigMap and redeploy the operator to have the changes take effect. The running TrustyAI services won't be redeployed automatically. To use the new image or tag, you'll need to delete and recreate the TrustyAIService resources.

## Contributing

Please see the [CONTRIBUTING.md](./CONTRIBUTING.md) file for more details on how to contribute to this project.

## License

This project is licensed under the Apache License Version 2.0 - see the [LICENSE](./LICENSE) file for details.
# TrustyAI Kubernetes Operator

## Overview

The TrustyAI Kubernetes Operator aims at simplifying the deployment and management of the [TrustyAI service](https://github.com/trustyai-explainability/trustyai-explainability/tree/main/explainability-service) on Kubernetes and OpenShift clusters by watching for custom resources of kind `TrustyAIService` in the `trustyai.opendatahub.io` API group and manages deployments, services, and optionally, routes and `ServiceMonitors` corresponding to these resources.

The operator ensures the service is properly configured, is discoverable by Prometheus for metrics scraping (on both Kubernetes and OpenShift), and is accessible via a Route on OpenShift.

## Prerequisites

- Kubernetes cluster v1.19+ or OpenShift cluster v4.6+
- `kubectl` v1.19+ or `oc` client v4.6+

## Installation using pre-built Operator image

This operator is available as a [Docker image on Quay.io](https://quay.io/repository/ruimvieira/trustyai-service-operator?tab=history). 
To deploy it on your cluster:

1. **Install the Custom Resource Definition (CRD):**

   Apply the CRD to your cluster:

    ```bash
    kubectl apply -f https://raw.githubusercontent.com/username/trustyai-operator/main/config/crd/bases/trustyai.opendatahub.io.trustyai.opendatahub.io_trustyaiservices.yaml
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
              image: quay.io/ruimvieira/trustyai-service-operator:v0.1.0
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

## Usage

Once the operator is installed, you can create `TrustyAIService` resources, and the operator will create corresponding TrustyAI deployments, services, and (on OpenShift) routes.

Here's an example `TrustyAIService` manifest:

```yaml
apiVersion: trustyai.opendatahub.io.trusty.opendatahub.io/v1
kind: TrustyAIService
metadata:
  name: trustyai-service-example
  namespace: default
spec:
  # Optional values for replicas, image and tag. Below are the default values.
  # replicas: 1
  # image: quay.io/trustyaiservice/trustyai-service
  # tag: latest
  storage:
    format: "PVC"
    folder: "/inputs"
  data:
    filename: "data.csv"
    format: "CSV"
  metrics:
    schedule: "5s"
```

You can apply this manifest with `kubectl apply -f <file-name.yaml>` to create a service.

## Contributing

Please see the [CONTRIBUTING.md](./CONTRIBUTING.md) file for more details on how to contribute to this project.

## License

This project is licensed under the Apache License Version 2.0 - see the [LICENSE](./LICENSE) file for details.
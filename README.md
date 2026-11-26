# TrustyAI Kubernetes Operator
[![Controller Tests](https://github.com/trustyai-explainability/trustyai-service-operator/actions/workflows/controller-tests.yaml/badge.svg)](https://github.com/trustyai-explainability/trustyai-service-operator/actions/workflows/controller-tests.yaml)
[![YAML lint](https://github.com/trustyai-explainability/trustyai-service-operator/actions/workflows/lint-yaml.yaml/badge.svg)](https://github.com/trustyai-explainability/trustyai-service-operator/actions/workflows/lint-yaml.yaml)
[![Gosec Security Scan](https://github.com/trustyai-explainability/trustyai-service-operator/actions/workflows/gosec.yaml/badge.svg)](https://github.com/trustyai-explainability/trustyai-service-operator/actions/workflows/gosec.yaml)


## Overview

The TrustyAI Kubernetes Operator aims at simplifying the deployment and management of various TrustyAI Kubernetes components, such as:
- [TrustyAI Service](https://github.com/trustyai-explainability/trustyai-explainability): A service that deploys alongside KServe models and collects
inference data to enable model explainability, fairness monitoring, and drift tracking.
- [LM-Eval](https://github.com/EleutherAI/lm-evaluation-harness/tree/main): A job-based architecture for deploying and managing LLM evaluations, based on EleutherAI's lm-evaluation-harness library.
- [EvalHub](https://github.com/eval-hub): A service for orchestrating AI/ML model evaluations, providing a unified API for running benchmarks across multiple evaluation providers and frameworks, with support for multi-tenant and single-tenant deployments.

## Prerequisites
- Kubernetes cluster v1.19+ or OpenShift cluster v4.6+
- `kubectl` v1.19+ or `oc` client v4.6+
- `kustomize` v5+
## Installation

This operator is available as an [image on Quay.io](https://quay.io/repository/trustyai/trustyai-service-operator?tab=history). 
To deploy it on your cluster:

```shell
OPERATOR_NAMESPACE=opendatahub
make manifest-gen NAMESPACE=$OPERATOR_NAMESPACE KUSTOMIZE=kustomize
oc apply -f release/trustyai_bundle.yaml -n $OPERATOR_NAMESPACE
```
You can also build your own image, and use that as your TrustyAI operator:

```shell
OPERATOR_NAMESPACE=opendatahub
OPERATOR_IMAGE=quay.io/yourorg/your-image-name:latest
podman build -t $OPERATOR_IMAGE --platform linux/amd64 -f Dockerfile .
podman push $OPERATOR_IMAGE
make manifest-gen NAMESPACE=$OPERATOR_NAMESPACE OPERATOR_IMAGE=$OPERATOR_IMAGE KUSTOMIZE=kustomize
oc apply -f release/trustyai_bundle.yaml -n $OPERATOR_NAMESPACE
```

## Upgrading

### EvalHub authorization configuration

The operator reconciles EvalHub authorization rules in the `<evalhub-name>-config`
ConfigMap. The `kube-rbac-proxy` container mounts its `auth.yaml` key using a
`subPath` volume mount, so existing pods do not receive updates to that file.
Changing the ConfigMap alone does not trigger a Deployment rollout. Updated
authorization rules take effect only after the EvalHub pods are recreated, for
example when an operand image update changes the pod template.

After upgrading the operator, wait for it to reconcile the EvalHub ConfigMap. If
the upgrade changes authorization rules without recreating the pods, recreate
the API pods for each affected EvalHub instance:

```shell
EVALHUB_NAMESPACE=your-namespace
EVALHUB_NAME=your-evalhub
oc delete pod -n "$EVALHUB_NAMESPACE" \
  -l "app=eval-hub,instance=$EVALHUB_NAME,component=api"
oc rollout status deployment/"$EVALHUB_NAME" -n "$EVALHUB_NAMESPACE"
```

This can briefly interrupt API access while replacement pods become ready. In
particular, `GET /api/v1/info` can continue returning HTTP 403 after an operator
upgrade until the pods load the new authorization mapping. Once recreated,
access requires permission to list `evaluations` in the
`trustyai.opendatahub.io` API group in the namespace supplied by `X-Tenant`.

## Usage
For usage information, please see the [OpenDataHub documentation of TrustyAI](https://opendatahub.io/docs/monitoring-data-science-models/#configuring-trustyai_monitor).

For database-backed deployments, see the [TrustyAI Service database tuning guide](docs/database-tuning.md) for ConfigMap-based tuning options.

### EvalHub collection-generation configuration

The operator uses the resolved EvalHub operand image for the API service and the
generation worker, init, and sidecar containers. Use an EvalHub image that
contains the generation worker binary. The operator rejects
`EVALHUB_COLLECTION_GENERATION_RUNNER_IMAGE` in `spec.env` because the worker
image is managed alongside the service image.

Administrators can override effort limits in the EvalHub resource's `spec.env`.
Use `EVALHUB_GENERATION_<EFFORT>_<LIMIT>`, where `<EFFORT>` is `LOW`,
`STANDARD`, or `HIGH`. Their defaults are:

| `<LIMIT>` | Low | Standard | High |
| --- | ---: | ---: | ---: |
| `MAX_ITERATIONS` | 8 | 20 | 40 |
| `MAX_MODEL_CALLS` | 8 | 20 | 40 |
| `MAX_TOOL_CALLS` | 24 | 60 | 120 |
| `MAX_TOTAL_TOKENS` | 32768 | 131072 | 524288 |
| `MAX_OUTPUT_TOKENS` | 2048 | 4096 | 8192 |
| `MAX_DURATION` | 2m | 2m | 10m |

Supply positive integers for counts and Go duration strings such as `3m` for
durations. Omitted or empty overrides use the EvalHub defaults. Each effort
requires `MAX_MODEL_CALLS` to be at least `MAX_ITERATIONS` and
`MAX_TOTAL_TOKENS` to be at least `MAX_OUTPUT_TOKENS`. Every limit must stay
the same or increase from Low to Standard to High, within the EvalHub hard
limits.

Before updating the Deployment, the operator validates literal effort limits,
relationships between explicitly supplied literal limits, and literal
retention and cleanup-interval durations. If preflight fails, the EvalHub
resource's `Ready` condition becomes `False` and names the affected variables.
At startup, EvalHub applies its own effort defaults and hard limits and
validates the effective settings, including `valueFrom` references and
Kubernetes `$(VAR)` expansions.

`EVALHUB_COLLECTION_GENERATION_RETENTION` sets how long terminal generation
records stay visible. Use a non-negative Go duration, or omit the value or set
it to `0` to use the default of `24h`.
`EVALHUB_RESOURCE_JANITOR_INTERVAL` sets
the database cleanup interval independently. Use whole-minute durations from
`1m` to `24h`, or `0` to select the default of `24h`.

## Contributing

Please see the [CONTRIBUTING.md](./CONTRIBUTING.md) file for more details on how to contribute to this project.

## License

This project is licensed under the Apache License Version 2.0 - see the [LICENSE](./LICENSE) file for details.

# Multi-Operator Deployment Guide

This guide explains how to deploy the TrustyAI Service Operator alongside the TrustyAI Guardrails Operator in the same Kubernetes cluster without CR ownership conflicts.

## The Problem

Both operators can manage `NemoGuardrails` custom resources:
- **trustyai-service-operator**: Multi-service operator that manages TrustyAI services, LMES, NeMo Guardrails, and more
- **trustyai-guardrails-operator**: Dedicated operator for guardrails controllers

When both are deployed cluster-wide, they will both try to reconcile the same `NemoGuardrails` CRs, causing conflicts.

## Solution: Namespace-Based Separation

The TrustyAI Service Operator supports namespace-scoped watching via the `WATCH_NAMESPACES` environment variable.

### Deployment Scenarios

#### Scenario 1: Single Operator (Default)
Deploy only one operator. No configuration needed.

**Option A: Service Operator Only (Recommended for Full TrustyAI Stack)**
```bash
kubectl apply -f trustyai-service-operator.yaml
```
Manages: TrustyAI Services, LMES, Guardrails, EvalHub, Orchestrators

**Option B: Guardrails Operator Only**
```bash
kubectl apply -f trustyai-guardrails-operator.yaml
```
Manages: Only guardrails controllers

---

#### Scenario 2: Both Operators with Namespace Separation

**Use Case**: You want the full TrustyAI stack in some namespaces, but dedicated guardrails management in others.

**Configuration**:
1. Deploy trustyai-service-operator to watch only TrustyAI-specific namespaces
2. Deploy trustyai-guardrails-operator to watch only guardrails-specific namespaces
3. Ensure no overlap

**Example Setup**:

```yaml
# trustyai-service-operator watches trustyai-* namespaces
apiVersion: apps/v1
kind: Deployment
metadata:
  name: controller-manager
  namespace: trustyai-service-operator-system
spec:
  template:
    spec:
      containers:
      - name: manager
        env:
        - name: WATCH_NAMESPACES
          value: "trustyai-prod,trustyai-dev,trustyai-test"
```

```yaml
# trustyai-guardrails-operator watches guardrails-* namespaces
apiVersion: apps/v1
kind: Deployment
metadata:
  name: trustyai-guardrails-operator-controller-manager
  namespace: trustyai-guardrails-operator-system
spec:
  template:
    spec:
      containers:
      - name: manager
        env:
        - name: WATCH_NAMESPACES
          value: "guardrails-prod,guardrails-dev"
```

**Result**:
- `NemoGuardrails` CRs in `trustyai-*` namespaces → managed by service operator
- `NemoGuardrails` CRs in `guardrails-*` namespaces → managed by guardrails operator
- No conflicts

---

#### Scenario 3: Service Operator Excludes Guardrails

**Use Case**: You want TrustyAI services everywhere, but guardrails only in specific namespaces.

**Configuration**:
1. Deploy trustyai-service-operator cluster-wide but with NEMO_GUARDRAILS disabled
2. Deploy trustyai-guardrails-operator for specific namespaces

```yaml
# trustyai-service-operator: disable NEMO_GUARDRAILS service
apiVersion: apps/v1
kind: Deployment
metadata:
  name: controller-manager
  namespace: trustyai-service-operator-system
spec:
  template:
    spec:
      containers:
      - name: manager
        args:
        - --leader-elect
        - --enable-services
        - "TAS,LMES,GORCH,EVALHUB"  # No NEMO_GUARDRAILS
```

```yaml
# trustyai-guardrails-operator: manages all guardrails
apiVersion: apps/v1
kind: Deployment
metadata:
  name: trustyai-guardrails-operator-controller-manager
  namespace: trustyai-guardrails-operator-system
spec:
  template:
    spec:
      containers:
      - name: manager
        env:
        - name: WATCH_NAMESPACES
          value: ""  # Cluster-wide
```

**Result**:
- TrustyAI services managed by service operator everywhere
- All `NemoGuardrails` CRs managed by dedicated guardrails operator
- Clear separation of concerns

---

## Configuration Methods

### Method 1: Direct YAML Edit

Edit the deployment manifest before applying:
```yaml
env:
- name: WATCH_NAMESPACES
  value: "namespace1,namespace2,namespace3"
```

### Method 2: Kustomize Patch

Create a patch file:
```yaml
# patch-watch-namespaces.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: controller-manager
  namespace: trustyai-service-operator-system
spec:
  template:
    spec:
      containers:
      - name: manager
        env:
        - name: WATCH_NAMESPACES
          value: "trustyai-prod,trustyai-dev"
```

Apply with kustomize:
```yaml
# kustomization.yaml
resources:
- trustyai-service-operator.yaml

patches:
- path: patch-watch-namespaces.yaml
```

### Method 3: kubectl set env

After deployment, update the environment variable:
```bash
kubectl set env deployment/controller-manager \
  -n trustyai-service-operator-system \
  WATCH_NAMESPACES="trustyai-prod,trustyai-dev"
```

### Method 4: Disable Specific Services

Instead of namespace scoping, disable specific services:
```bash
# Only enable TAS and LMES, no guardrails
kubectl set env deployment/controller-manager \
  -n trustyai-service-operator-system \
  --containers=manager \
  --env-overwrite=false \
  -- \
  args:
    - --leader-elect
    - --enable-services
    - "TAS,LMES"
```

---

## Service Controller Management

The TrustyAI Service Operator supports selective controller enablement via `--enable-services`:

**Available Services**:
- `TAS` - TrustyAI Service
- `LMES` - LM Evaluation Service
- `NEMO_GUARDRAILS` - NeMo Guardrails
- `GORCH` - Guardrails Orchestrator
- `EVALHUB` - Evaluation Hub

**Examples**:
```bash
# All services (default)
--enable-services "TAS,LMES,GORCH,NEMO_GUARDRAILS,EVALHUB"

# Only TrustyAI core services
--enable-services "TAS,LMES"

# Only guardrails-related
--enable-services "NEMO_GUARDRAILS,GORCH"

# Everything except NeMo Guardrails
--enable-services "TAS,LMES,GORCH,EVALHUB"
```

---

## RBAC Considerations

### Cluster-Wide Watching (Default)
Requires ClusterRole and ClusterRoleBinding to access resources across all namespaces.

**Included in default deployment manifests.**

### Namespace-Scoped Watching
When watching specific namespaces, you can optionally reduce RBAC permissions to only those namespaces.

**Important**: The service operator manages multiple CRD types, so namespace-scoped RBAC is more complex than the guardrails operator.

---

## Verification

### Check Which Namespaces Are Being Watched

```bash
# View operator logs
kubectl logs -n trustyai-service-operator-system \
  deployment/controller-manager

# Look for log lines:
# "Configuring operator to watch specific namespaces" namespaces="ns1,ns2"
# OR
# "Watching all namespaces (cluster-wide)"
```

### Check Which Services Are Enabled

```bash
# View deployment args
kubectl get deployment controller-manager \
  -n trustyai-service-operator-system \
  -o jsonpath='{.spec.template.spec.containers[0].args}' | jq
```

### Test Separation

1. Create a `NemoGuardrails` CR in a watched namespace:
```bash
kubectl apply -f - <<EOF
apiVersion: trustyai.opendatahub.io/v1alpha1
kind: NemoGuardrails
metadata:
  name: test-guardrails
  namespace: trustyai-prod
spec:
  nemoConfigs: []
EOF
```

2. Check which operator reconciled it:
```bash
# Check events
kubectl describe nemoguardrails test-guardrails -n trustyai-prod

# Check service operator logs
kubectl logs -n trustyai-service-operator-system \
  deployment/controller-manager | grep test-guardrails
```

---

## Troubleshooting

### Both operators reconciling the same CR

**Symptoms**: Resources being created/updated repeatedly, conflicting status updates

**Solution**: Ensure proper separation using one of these methods:
1. Configure `WATCH_NAMESPACES` to avoid overlap
2. Disable `NEMO_GUARDRAILS` service in one operator
3. Deploy only the operator you need

**Debug**:
```bash
# Check service operator namespace configuration
kubectl get deployment controller-manager \
  -n trustyai-service-operator-system \
  -o yaml | grep -A 5 "WATCH_NAMESPACES"

# Check which services are enabled
kubectl get deployment controller-manager \
  -n trustyai-service-operator-system \
  -o yaml | grep -A 2 "enable-services"
```

### Operator not reconciling CRs

**Symptoms**: CRs created but no resources appear

**Possible Causes**:
1. CR is in a namespace not being watched
2. Required service not enabled in `--enable-services`
3. RBAC permissions insufficient
4. Operator not running

**Debug**:
```bash
# Check operator is running
kubectl get pods -n trustyai-service-operator-system

# Check namespace and service configuration
kubectl get deployment controller-manager \
  -n trustyai-service-operator-system -o yaml

# Check logs for errors
kubectl logs -n trustyai-service-operator-system \
  deployment/controller-manager --tail=100
```

---

## Recommendations

### Development/Testing
- Deploy only the service operator with all services enabled
- Simpler to manage and debug
- Use namespace separation only when testing multi-operator scenarios

### Production

**Option 1 (Recommended): Single Service Operator**
- Deploy trustyai-service-operator with all needed services enabled
- Simpler operational model
- Single point of configuration and monitoring
- Use when you need multiple TrustyAI services

**Option 2: Service Operator + Dedicated Guardrails Operator**
- Use when you have:
  - Different teams managing different services
  - Different update/release cycles for guardrails vs other services
  - Strict namespace isolation requirements
- Configure clear namespace separation
- Document the separation for operations team
- Consider using service exclusion instead of namespace scoping if possible

**Option 3: Guardrails Operator Only**
- Deploy only trustyai-guardrails-operator
- Use when you ONLY need guardrails, no other TrustyAI services
- Simpler and lighter weight

### Multi-Tenant Environments
- Deploy one service operator per tenant
- Use `WATCH_NAMESPACES` to isolate tenants
- Use RBAC to enforce isolation
- Consider disabling unneeded services per tenant

---

## Summary

| Deployment Scenario | Service Operator Config | Guardrails Operator Config | Result |
|---------------------|------------------------|---------------------------|--------|
| Service operator only | All services, cluster-wide | Not deployed | Service operator manages everything |
| Guardrails operator only | Not deployed | Cluster-wide | Guardrails operator manages only guardrails |
| Both with namespace separation | `WATCH_NAMESPACES="ns1,ns2"` | `WATCH_NAMESPACES="ns3,ns4"` | No overlap, no conflicts |
| Both with service exclusion | `--enable-services="TAS,LMES,EVALHUB"` | Cluster-wide | Service operator skips guardrails |
| Service operator without guardrails | Disable `NEMO_GUARDRAILS` | Cluster-wide | Clean service separation |

**Key Principle**: Ensure no two operators watch the same namespace for the same CR types, OR disable conflicting services in one operator.

---

## Related Documentation

- [TrustyAI Guardrails Operator Multi-Operator Guide](https://github.com/trustyai-explainability/trustyai-guardrails-operator/blob/main/docs/multi-operator-deployment.md)

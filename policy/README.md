# Security Policies

This directory contains OPA (Open Policy Agent) Rego policies for validating Kubernetes manifests using Conftest.

## Policies

### container_security.rego
- **Deny** containers running as root (must set `runAsNonRoot: true`)
- **Deny** containers allowing privilege escalation
- **Deny** privileged containers

### resource_limits.rego
- **Warn** about missing resource limits and requests
- **Warn** about missing memory and CPU limits

### network_security.rego
- **Deny** pods using `hostNetwork`
- **Deny** pods using `hostPID`
- **Deny** pods using `hostIPC`

### rbac_security.rego
- **Deny** wildcard (`*`) permissions in ClusterRoles and Roles

### capabilities.rego
- **Deny** dangerous Linux capabilities (SYS_ADMIN, SYS_MODULE, etc.)
- **Warn** when containers don't drop all capabilities first

## Usage

### Automatic validation with pre-commit

Install pre-commit hooks to automatically validate manifests on commit:

```bash
# Install pre-commit (one-time)
pip install pre-commit

# Install the git hooks
pre-commit install

# Test it
pre-commit run --all-files
```

The hooks will automatically run conftest validation on:
- `config/base` manifests
- `config/overlays/odh` manifests
- `config/overlays/rhoai` manifests

**Note**: Requires `conftest` and `kustomize` to be available in your PATH.

### Manual validation

You can also run conftest validation manually:

```bash
# Validate base configuration
kustomize build config/base | conftest test -p policy/ -

# Validate ODH overlay
kustomize build config/overlays/odh | conftest test -p policy/ -

# Validate RHOAI overlay
kustomize build config/overlays/rhoai | conftest test -p policy/ -
```

## Customisation

You can modify these policies or add new ones based on your security requirements. Each policy file follows the OPA Rego v1 format with `deny` rules for violations and `warn` rules for recommendations.

## Requirements

- [Conftest](https://www.conftest.dev/) - Policy testing tool
- [Kustomize](https://kustomize.io/) - Kubernetes configuration management

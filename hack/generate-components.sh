#!/usr/bin/env bash

# Copyright 2024 The TrustyAI Authors
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

# Script to generate Kustomize components from controller-gen output
# This script is called automatically by 'make manifests'
# Compatible with bash 3.2+ (macOS default)

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
CONFIG_DIR="${PROJECT_ROOT}/config"
CRD_BASES_DIR="${CONFIG_DIR}/crd/bases"
RBAC_DIR="${CONFIG_DIR}/rbac"
COMPONENTS_DIR="${CONFIG_DIR}/components"

# Component list (bash 3.2 compatible)
COMPONENT_NAMES="tas evalhub lmes job-mgr gorch nemo-guardrails"

# Function to get component details -> leave blank if the component has no CRDs
get_crd_pattern() {
    case "$1" in
        tas) echo "trustyaiservices" ;;
        evalhub) echo "evalhubs" ;;
        lmes) echo "lmevaljobs" ;;
        job-mgr) echo "" ;;  # JOB_MGR shares CRD with LMES
        gorch) echo "guardrailsorchestrators" ;;
        nemo-guardrails) echo "nemoguardrails" ;;
    esac
}

get_controller_dirs() {
    case "$1" in
        tas) echo "controllers/tas" ;;
        evalhub) echo "controllers/evalhub" ;;
        lmes) echo "controllers/lmes" ;;
        job-mgr) echo "controllers/job_mgr" ;;
        gorch) echo "controllers/gorch" ;;
        nemo-guardrails) echo "controllers/nemo_guardrails" ;;
    esac
}


echo "==> Generating Kustomize components from controller-gen output"

# Ensure components directory exists
mkdir -p "${COMPONENTS_DIR}"

# Generate each component
for component in ${COMPONENT_NAMES}; do
    echo "==> Generating component: ${component}"

    # Get component details
    crd_pattern=$(get_crd_pattern "${component}")
    controller_dirs=$(get_controller_dirs "${component}")

    # Create component directories
    component_dir="${COMPONENTS_DIR}/${component}"
    mkdir -p "${component_dir}/rbac"

    # 1. Move CRD file (skip for job-mgr as it shares CRD with lmes)
    # Auto-generated CRDs are moved from config/crd/bases/ to component directories
    if [[ ! -z ${crd_pattern} ]]; then
      echo "  - Moving CRD (pattern: ${crd_pattern})"
      crd_file=$(find "${CRD_BASES_DIR}" -name "*${crd_pattern}.yaml" 2>/dev/null | head -1)
      if [[ -n "${crd_file}" && -f "${crd_file}" ]]; then
        mv -f "${crd_file}" "${component_dir}/crd/$(basename "${crd_file}")"
        echo "    ✓ Moved: $(basename "${crd_file}")"
      else
        echo "    ✗ No CRDs for pattern '${crd_pattern}'"
      fi
    fi

    # 2. Extract manager RBAC rules
    echo "  - Extracting manager RBAC"
    "${SCRIPT_DIR}/extract-component-rbac.sh" \
        "${component}" \
        "${crd_pattern}" \
        "${controller_dirs}" \
        > "${component_dir}/rbac/manager-rbac.yaml"

    # 3. Generate ClusterRoleBinding for the manager role
    echo "  - Generating manager-rolebinding.yaml"
    cat > "${component_dir}/rbac/manager-rolebinding.yaml" <<EOF
# do not edit: generated via hack/generate-components.sh
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  labels:
    app.kubernetes.io/name: clusterrolebinding
    app.kubernetes.io/instance: ${component}-manager-rolebinding
    app.kubernetes.io/component: rbac
    app.kubernetes.io/created-by: trustyai-service-operator
    app.kubernetes.io/managed-by: kustomize
  name: ${component}-manager-rolebinding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: trustyai-service-operator-${component}-manager-role
subjects:
  - kind: ServiceAccount
    name: controller-manager
    namespace: system
EOF

  # 4.  Generate component kustomization.yaml
    echo "  - Generating kustomization.yaml"
    "${SCRIPT_DIR}/generate-component-kustomization.sh" \
        "${component}" \
        > "${component_dir}/kustomization.yaml"
done


echo "==> Removing original monolithic role.yaml"
rm -f ${CONFIG_DIR}/rbac/role.yaml

echo "==> Component generation complete!"
echo ""
echo "Components created in: ${COMPONENTS_DIR}"
echo "Run 'make components-validate' to verify all components build correctly"

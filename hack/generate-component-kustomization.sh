#!/usr/bin/env bash

# Copyright 2024 The TrustyAI Authors
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

# Script to generate component kustomization.yaml
# Takes component name as argument

if [[ $# -lt 1 ]]; then
    echo "Usage: $0 <component_name>"
    exit 1
fi

COMPONENT_NAME="$1"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
COMPONENT_DIR="${PROJECT_ROOT}/config/components/${COMPONENT_NAME}"

# Start kustomization.yaml
cat <<EOF
# do not edit: generated via hack/generate-component-kustomization.sh
apiVersion: kustomize.config.k8s.io/v1alpha1
kind: Component

resources:
EOF

# Add all YAML files from resource directories
for RESOURCE_DIR in crd rbac configmaps; do
    if [[ -d "${COMPONENT_DIR}/${RESOURCE_DIR}" ]]; then
        # Use find to get all .yaml files, sort them for consistent output
        while IFS= read -r -d '' file; do
            filename=$(basename "$file")
            echo "  - ${RESOURCE_DIR}/${filename}"
        done < <(find "${COMPONENT_DIR}/${RESOURCE_DIR}" -maxdepth 1 -name "*.yaml" -print0 2>/dev/null | sort -z)
    fi
done

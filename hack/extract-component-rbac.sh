#!/usr/bin/env bash

# Copyright 2024 The TrustyAI Authors
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

# Script to extract controller-specific RBAC rules
# Takes component name, CRD pattern, and controller directories as arguments

if [[ $# -lt 3 ]]; then
    echo "Usage: $0 <component_name> <crd_pattern> <controller_dirs>"
    echo "  controller_dirs: comma-separated list of controller directories"
    exit 1
fi

COMPONENT_NAME="$1"
CRD_PATTERN="$2"
CONTROLLER_DIRS="$3"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
ROLE_FILE="${PROJECT_ROOT}/config/rbac/role.yaml"

# Temporary files
TEMP_RULES=$(mktemp)
trap "rm -f ${TEMP_RULES}" EXIT

# Extract rules from kubebuilder markers in controller Go files
IFS=',' read -ra DIRS <<< "${CONTROLLER_DIRS}"
for controller_dir in "${DIRS[@]}"; do
    dir="${PROJECT_ROOT}/${controller_dir}"
    if [[ -d "${dir}" ]]; then
        # Find all Go files with kubebuilder:rbac markers (with or without space)
        find "${dir}" -name "*.go" -exec grep -h "// *+kubebuilder:rbac" {} \; 2>/dev/null || true
    fi
done | "${SCRIPT_DIR}/kubebuilder-rbac-to-yaml.sh" > "${TEMP_RULES}"

# If no rules were extracted from markers, extract from role.yaml
if [[ ! -s "${TEMP_RULES}" && -n "${CRD_PATTERN}" ]]; then
    # For controllers without kubebuilder markers, extract comprehensive rules from role.yaml
    # Include: CRD-specific rules + common controller resources
    awk -v pattern="${CRD_PATTERN}" '
    BEGIN {
        in_rule = 0
        rule_buffer = ""
        should_include = 0
    }

    /^- apiGroups:/ {
        # Output previous rule if it should be included
        if (in_rule && should_include && rule_buffer != "") {
            print rule_buffer
        }
        # Start new rule
        in_rule = 1
        should_include = 0
        rule_buffer = $0
        apigroup_line = ""
        next
    }

    in_rule {
        rule_buffer = rule_buffer "\n" $0

        # Capture apiGroups line for analysis
        if ($0 ~ /^  - /) {
            if (apigroup_line == "") {
                apigroup_line = $0
            }
        }

        # Include if:
        # 1. Contains the CRD pattern (e.g., nemoguardrails)
        # 2. Is for apps apiGroup (Deployments)
        # 3. Is for core "" apiGroup (Services, ConfigMaps, etc.)
        # 4. Is for route.openshift.io (Routes)
        # 5. Is for monitoring.coreos.com (ServiceMonitors)
        # 6. Is for rbac.authorization.k8s.io (ClusterRoleBindings, RoleBindings)
        if ($0 ~ pattern) {
            should_include = 1
        } else if (apigroup_line ~ /apps$/) {
            should_include = 1
        } else if (apigroup_line ~ /""$/) {
            # Core API group - check for controller resources
            if ($0 ~ /deployments/ || $0 ~ /services/ || $0 ~ /configmaps/ ||
                $0 ~ /secrets/ || $0 ~ /pods/) {
                should_include = 1
            }
        } else if (apigroup_line ~ /route\.openshift\.io/) {
            should_include = 1
        } else if (apigroup_line ~ /monitoring\.coreos\.com/) {
            should_include = 1
        } else if (apigroup_line ~ /rbac\.authorization\.k8s\.io/) {
            should_include = 1
        }
    }

    END {
        # Output last rule if it should be included
        if (in_rule && should_include && rule_buffer != "") {
            print rule_buffer
        }
    }
    ' "${ROLE_FILE}" > "${TEMP_RULES}"
fi

# Output the RBAC rules as a ClusterRole
cat <<EOF
# do not edit: generated via hack/extract-component-rbac.sh
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: ${COMPONENT_NAME}-manager-role
EOF

# Append the rules
if [[ -s "${TEMP_RULES}" ]]; then
    echo "rules:"
    cat "${TEMP_RULES}"
else
    echo "rules: []"
fi

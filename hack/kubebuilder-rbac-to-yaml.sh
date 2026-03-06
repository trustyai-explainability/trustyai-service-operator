#!/usr/bin/env bash

# Copyright 2024 The TrustyAI Authors
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

# Script to convert kubebuilder:rbac markers to YAML format
# Reads from stdin, outputs YAML to stdout
# Compatible with bash 3.2+ (macOS default)

# Temporary file for processing
TEMP_FILE=$(mktemp)
TEMP_SEEN=$(mktemp)
trap "rm -f ${TEMP_FILE} ${TEMP_SEEN}" EXIT

# Read all input
cat > "${TEMP_FILE}"

# If no input, exit
if [[ ! -s "${TEMP_FILE}" ]]; then
    exit 0
fi

# Parse kubebuilder markers and convert to YAML
# Format: //+kubebuilder:rbac:groups=foo,resources=bar,verbs=get;list

# Process each line
while IFS= read -r line; do
    # Skip non-rbac markers
    if [[ ! "${line}" =~ kubebuilder:rbac ]]; then
        continue
    fi

    # Extract the marker content after // +kubebuilder:rbac: (with or without space)
    marker="${line#*kubebuilder:rbac:}"

    # Parse key=value pairs
    groups=""
    resources=""
    verbs=""
    urls=""

    IFS=',' read -ra PAIRS <<< "${marker}"
    for pair in "${PAIRS[@]}"; do
        key="${pair%%=*}"
        value="${pair#*=}"

        case "${key}" in
            groups) groups="${value}" ;;
            resources) resources="${value}" ;;
            verbs) verbs="${value}" ;;
            urls) urls="${value}" ;;
        esac
    done

    # Create a unique key for deduplication
    rule_key="${groups}|${resources}|${verbs}|${urls}"

    # Check if we've seen this rule before (simple deduplication)
    if grep -Fxq "${rule_key}" "${TEMP_SEEN}" 2>/dev/null; then
        continue
    fi
    echo "${rule_key}" >> "${TEMP_SEEN}"

    # Output YAML rule
    echo "- apiGroups:"
    if [[ -n "${groups}" ]]; then
        IFS=';' read -ra GROUP_ARRAY <<< "${groups}"
        for group in "${GROUP_ARRAY[@]}"; do
            # Handle empty group (core API)
            if [[ "${group}" == "core" || "${group}" == '""' || -z "${group}" ]]; then
                echo '  - ""'
            else
                echo "  - ${group}"
            fi
        done
    else
        echo '  - ""'
    fi

    if [[ -n "${resources}" ]]; then
        echo "  resources:"
        IFS=';' read -ra RESOURCE_ARRAY <<< "${resources}"
        for resource in "${RESOURCE_ARRAY[@]}"; do
            echo "  - ${resource}"
        done
    fi

    if [[ -n "${verbs}" ]]; then
        echo "  verbs:"
        IFS=';' read -ra VERB_ARRAY <<< "${verbs}"
        for verb in "${VERB_ARRAY[@]}"; do
            echo "  - ${verb}"
        done
    fi

    if [[ -n "${urls}" ]]; then
        echo "  nonResourceURLs:"
        IFS=';' read -ra URL_ARRAY <<< "${urls}"
        for url in "${URL_ARRAY[@]}"; do
            echo "  - ${url}"
        done
    fi
done < "${TEMP_FILE}"

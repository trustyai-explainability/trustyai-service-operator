#!/usr/bin/env bash

# Copyright 2024 The TrustyAI Authors
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

# Script to validate all generated components
# Checks that each component builds successfully with kustomize

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
COMPONENTS_DIR="${PROJECT_ROOT}/config/components"

# Check if kustomize is available
if ! command -v kustomize &> /dev/null; then
    echo "Error: kustomize not found. Please run 'make kustomize' first."
    exit 1
fi

KUSTOMIZE="${PROJECT_ROOT}/bin/kustomize"
if [[ ! -x "${KUSTOMIZE}" ]]; then
    KUSTOMIZE="kustomize"
fi

echo "==> Validating Kustomize components"
echo ""

FAILED_COMPONENTS=()
VALIDATED_COMPONENTS=()

# Validate each component
for component_dir in "${COMPONENTS_DIR}"/*; do
    if [[ ! -d "${component_dir}" ]]; then
        continue
    fi

    component_name=$(basename "${component_dir}")
    echo "==> Validating component: ${component_name}"

    # Check kustomization.yaml exists
    if [[ ! -f "${component_dir}/kustomization.yaml" ]]; then
        echo "  ✗ Missing kustomization.yaml"
        FAILED_COMPONENTS+=("${component_name}")
        continue
    fi

    # Check CRD exists (skip for job-mgr which shares CRD with lmes)
    if [[ "${component_name}" != "job-mgr" ]] && ! compgen -G "${component_dir}/crd/*.yaml" > /dev/null; then
        echo "  ✗ Missing CRD YAML inside of ${component_name}/crd directory"
        FAILED_COMPONENTS+=("${component_name}")
        continue
    fi

    # Try to build the component
    if "${KUSTOMIZE}" build "${component_dir}" > /dev/null 2>&1; then
        echo "  ✓ Component builds successfully"

        # Check that output contains expected resources
        output=$("${KUSTOMIZE}" build "${component_dir}")

        # Verify CRD is present (skip for job-mgr)
        if [[ "${component_name}" != "job-mgr" ]]; then
            if echo "${output}" | grep -q "^kind: CustomResourceDefinition"; then
                echo "  ✓ CRD present in output"
            else
                echo "  ✗ Warning: CRD not found in output"
            fi
        else
            echo "  ✓ No CRD (shares with LMES component)"
        fi

        # Verify ClusterRole is present
        if echo "${output}" | grep -q "^kind: ClusterRole"; then
            echo "  ✓ ClusterRole present in output"
        else
            echo "  ✗ Warning: ClusterRole not found in output"
        fi

        VALIDATED_COMPONENTS+=("${component_name}")
    else
        echo "  ✗ Failed to build component"
        echo ""
        echo "Error output:"
        "${KUSTOMIZE}" build "${component_dir}" 2>&1 || true
        echo ""
        FAILED_COMPONENTS+=("${component_name}")
    fi

    echo ""
done

# Summary
echo "==> Validation Summary"
echo ""
echo "Validated components: ${#VALIDATED_COMPONENTS[@]}"
for component in "${VALIDATED_COMPONENTS[@]}"; do
    echo "  ✓ ${component}"
done

if [[ ${#FAILED_COMPONENTS[@]} -gt 0 ]]; then
    echo ""
    echo "Failed components: ${#FAILED_COMPONENTS[@]}"
    for component in "${FAILED_COMPONENTS[@]}"; do
        echo "  ✗ ${component}"
    done
    echo ""
    exit 1
fi

echo ""
echo "==> Validating overlays with component composition"
echo ""

OVERLAYS_DIR="${PROJECT_ROOT}/config/overlays"
FAILED_OVERLAYS=()
VALIDATED_OVERLAYS=()

# Validate each overlay
for overlay_dir in "${OVERLAYS_DIR}"/*; do
    if [[ ! -d "${overlay_dir}" ]]; then
        continue
    fi

    overlay_name=$(basename "${overlay_dir}")
    echo "==> Validating overlay: ${overlay_name}"

    # Check kustomization.yaml exists
    if [[ ! -f "${overlay_dir}/kustomization.yaml" ]]; then
        echo "  ✗ Missing kustomization.yaml"
        FAILED_OVERLAYS+=("${overlay_name}")
        continue
    fi

    # Try to build the overlay
    if "${KUSTOMIZE}" build "${overlay_dir}" > /dev/null 2>&1; then
        echo "  ✓ Overlay builds successfully"
        VALIDATED_OVERLAYS+=("${overlay_name}")
    else
        echo "  ✗ Failed to build overlay"
        echo ""
        echo "Error output:"
        "${KUSTOMIZE}" build "${overlay_dir}" 2>&1 || true
        echo ""
        FAILED_OVERLAYS+=("${overlay_name}")
    fi

    echo ""
done

# Summary
echo "==> Overlay Validation Summary"
echo ""
echo "Validated overlays: ${#VALIDATED_OVERLAYS[@]}"
for overlay in "${VALIDATED_OVERLAYS[@]}"; do
    echo "  ✓ ${overlay}"
done

if [[ ${#FAILED_OVERLAYS[@]} -gt 0 ]]; then
    echo ""
    echo "Failed overlays: ${#FAILED_OVERLAYS[@]}"
    for overlay in "${FAILED_OVERLAYS[@]}"; do
        echo "  ✗ ${overlay}"
    done
    echo ""
    exit 1
fi

echo ""
echo "==> All components and overlays validated successfully!"

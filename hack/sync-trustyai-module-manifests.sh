#!/usr/bin/env bash

# Synchronize the generated workload-operator Kustomize tree into the module
# image source tree. The module image packages this directory at
# /opt/manifests-template and renders its ODH/RHOAI overlays at runtime.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
SOURCE_DIR="${PROJECT_ROOT}/config"
DEST_DIR="${PROJECT_ROOT}/trustyai-operator-module/config/manifests-template"

if [[ ! -d "${SOURCE_DIR}" ]]; then
    echo "error: workload manifest source does not exist: ${SOURCE_DIR}" >&2
    exit 1
fi

echo "Synchronizing workload manifests: ${SOURCE_DIR} -> ${DEST_DIR}"
rm -rf "${DEST_DIR}"
mkdir -p "${DEST_DIR}"
cp -a "${SOURCE_DIR}/." "${DEST_DIR}/"

echo "Synchronized $(find "${DEST_DIR}" -type f | wc -l | tr -d ' ') manifest files"

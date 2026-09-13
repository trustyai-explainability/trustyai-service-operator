#!/usr/bin/env bash
# Assemble the generated TrustyAI workload-operator manifests into
# /opt/manifests-template/. The source tree is synchronized by the top-level
# Makefile before the module image is built.

set -euo pipefail

MANIFESTS_DIR="${MANIFESTS_DIR:-/opt/manifests-template}"
TEMPLATE_SRC="${TEMPLATE_SRC:-config/manifests-template}"

echo "get_trustyai_manifests.sh: staging ${TEMPLATE_SRC} → ${MANIFESTS_DIR}"
mkdir -p "${MANIFESTS_DIR}"
cp -rT "${TEMPLATE_SRC}" "${MANIFESTS_DIR}"

echo "get_trustyai_manifests.sh: done"

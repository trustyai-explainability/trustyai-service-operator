#!/usr/bin/env bash
# Assemble TrustyAI workload operator manifests into /opt/manifests-template/.
#
# The workload operator's kustomize tree (config/) is copied from the build
# context into trustyai-operator/config/ inside the image. The module
# controller's selectOverlay() points directly at the real overlay paths, so
# no shim kustomization files are needed.
#
# Build context must be the repo root (trustyai-service-operator/) and the
# Dockerfile must COPY config/ as workload-config/ before calling this script.

set -euo pipefail

MANIFESTS_DIR="${MANIFESTS_DIR:-/opt/manifests-template}"
WORKLOAD_CONFIG="${WORKLOAD_CONFIG:-workload-config}"

echo "get_trustyai_manifests.sh: copying workload config ${WORKLOAD_CONFIG} → ${MANIFESTS_DIR}/trustyai-operator/config"
mkdir -p "${MANIFESTS_DIR}/trustyai-operator"
cp -R "${WORKLOAD_CONFIG}" "${MANIFESTS_DIR}/trustyai-operator/config"

echo "get_trustyai_manifests.sh: done"

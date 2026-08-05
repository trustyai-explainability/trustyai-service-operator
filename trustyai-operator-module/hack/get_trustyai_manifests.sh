#!/usr/bin/env bash
# Assemble TrustyAI operator manifests into /opt/manifests-template/.
#
# In production this script will download the official operator manifests
# bundle and unpack them into MANIFESTS_DIR. For now it copies the
# checked-in placeholder overlay structure so the module controller has a
# valid Kustomize tree to render at runtime.

set -euo pipefail

MANIFESTS_DIR="${MANIFESTS_DIR:-/opt/manifests-template}"
TEMPLATE_SRC="${TEMPLATE_SRC:-config/manifests-template}"

echo "get_trustyai_manifests.sh: staging ${TEMPLATE_SRC} → ${MANIFESTS_DIR}"
mkdir -p "${MANIFESTS_DIR}"
cp -rT "${TEMPLATE_SRC}" "${MANIFESTS_DIR}"

# TODO: replace the cp above with a download of the real operator manifests:
#   curl -sSL "${TRUSTYAI_MANIFESTS_URL}" | tar xz -C "${MANIFESTS_DIR}"

echo "get_trustyai_manifests.sh: done"

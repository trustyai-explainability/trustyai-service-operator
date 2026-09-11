#!/usr/bin/env bash
# Fetches provider and collection YAML files from the eval-hub upstream
# repository and generates Kubernetes ConfigMap manifests for the operator
# to deploy.
#
# Usage:
#     hack/sync-evalhub-providers.sh [branch]
#
# Arguments:
#     branch  Git branch to fetch from (default: main)
#
# Dependencies: curl, jq, yq (mikefarah/yq v4+)

set -euo pipefail

REPO="eval-hub/eval-hub"
UPSTREAM_PROVIDERS_DIR="config/providers"
UPSTREAM_COLLECTIONS_DIR="config/collections"
OUTPUT_DIR="config/configmaps/evalhub"

PROVIDER_TYPE_LABEL="trustyai.opendatahub.io/evalhub-provider-type"
PROVIDER_NAME_LABEL="trustyai.opendatahub.io/evalhub-provider-name"
COLLECTION_TYPE_LABEL="trustyai.opendatahub.io/evalhub-collection-type"
COLLECTION_NAME_LABEL="trustyai.opendatahub.io/evalhub-collection-name"

BRANCH="${1:-main}"

# Providers that reuse an existing kustomize image variable instead of
# the default evalhub-provider-<id>-image naming convention.
image_var_override() {
    case "$1" in
        garak)                echo "garak-provider-image" ;;
        garak-kfp)            echo "garak-provider-image" ;;
        lm_evaluation_harness) echo "lmes-pod-image" ;;
        *)                    echo "evalhub-provider-${2}-image" ;;
    esac
}

check_deps() {
    local missing=()
    for cmd in curl jq yq; do
        if ! command -v "$cmd" &>/dev/null; then
            missing+=("$cmd")
        fi
    done
    if [[ ${#missing[@]} -gt 0 ]]; then
        echo "ERROR: missing required tools: ${missing[*]}" >&2
        echo "Install with: brew install ${missing[*]}  (or your package manager)" >&2
        exit 1
    fi
}

list_yaml_files() {
    local upstream_dir="$1"
    local api_url="https://api.github.com/repos/${REPO}/contents/${upstream_dir}?ref=${BRANCH}"
    echo "Fetching file list from ${api_url}" >&2
    curl -sfL "$api_url" | jq -r '.[] | select(.name | test("\\.(yaml|yml)$")) | .name'
}

process_provider() {
    local filename="$1"
    local raw_url="https://raw.githubusercontent.com/${REPO}/${BRANCH}/${UPSTREAM_PROVIDERS_DIR}/${filename}"
    local content
    content="$(curl -sfL "$raw_url")"

    local provider_id
    provider_id="$(echo "$content" | yq -r '.id // ""')"
    if [[ -z "$provider_id" ]]; then
        echo "  SKIP: no 'id' field found in ${filename}" >&2
        return 1
    fi

    local safe_id="${provider_id//_/-}"
    local cm_file="provider-${safe_id}.yaml"
    local cm_name="evalhub-provider-${safe_id}"
    local var_name
    var_name="$(image_var_override "$provider_id" "$safe_id")"

    echo "  id=${provider_id} -> ${cm_file}" >&2

    local original_image
    original_image="$(echo "$content" | yq -r '.runtime.k8s.image // ""')"

    local provider_yaml="$content"
    if [[ -n "$original_image" ]]; then
        provider_yaml="$(echo "$provider_yaml" | sed "s|${original_image}|\$(${var_name})|g")"
    fi

    local indented
    indented="$(echo "$provider_yaml" | sed '/^$/!s/^/    /')"

    cat > "${OUTPUT_DIR}/${cm_file}" <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: ${cm_name}
  labels:
    ${PROVIDER_TYPE_LABEL}: system
    ${PROVIDER_NAME_LABEL}: ${safe_id}
data:
  ${filename}: |
${indented}
EOF

    echo "${cm_file}"
}

process_collection() {
    local filename="$1"
    local raw_url="https://raw.githubusercontent.com/${REPO}/${BRANCH}/${UPSTREAM_COLLECTIONS_DIR}/${filename}"
    local content
    content="$(curl -sfL "$raw_url")"

    local collection_id
    collection_id="$(echo "$content" | yq -r '.id // ""')"
    if [[ -z "$collection_id" ]]; then
        echo "  SKIP: no 'id' field found in ${filename}" >&2
        return 1
    fi

    local safe_id="${collection_id//_/-}"
    local cm_file="collection-${safe_id}.yaml"
    local cm_name="evalhub-collection-${safe_id}"

    echo "  id=${collection_id} -> ${cm_file}" >&2

    local indented
    indented="$(echo "$content" | sed '/^$/!s/^/    /')"

    cat > "${OUTPUT_DIR}/${cm_file}" <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: ${cm_name}
  labels:
    ${COLLECTION_TYPE_LABEL}: system
    ${COLLECTION_NAME_LABEL}: ${safe_id}
data:
  ${filename}: |
${indented}
EOF

    echo "${cm_file}"
}

write_kustomization() {
    {
        echo "resources:"
        for f in "$@"; do
            echo "  - ${f}"
        done
        echo ""
        echo "namespace: system"
    } > "${OUTPUT_DIR}/kustomization.yaml"
}

main() {
    check_deps

    mkdir -p "$OUTPUT_DIR"

    local cm_files=()
    local provider_ids=()
    local collection_ids=()

    # --- Providers ---
    local provider_filenames
    provider_filenames="$(list_yaml_files "$UPSTREAM_PROVIDERS_DIR")"
    if [[ -z "$provider_filenames" ]]; then
        echo "ERROR: No YAML files found in ${UPSTREAM_PROVIDERS_DIR}" >&2
        exit 1
    fi

    rm -f "${OUTPUT_DIR}"/provider-*.yaml

    while IFS= read -r filename; do
        echo "Processing provider ${filename}..."
        local cm_file
        if cm_file="$(process_provider "$filename")"; then
            cm_files+=("$cm_file")
            local safe_id="${cm_file#provider-}"
            safe_id="${safe_id%.yaml}"
            provider_ids+=("$safe_id")
        fi
    done <<< "$provider_filenames"

    # --- Collections ---
    local collection_filenames
    collection_filenames="$(list_yaml_files "$UPSTREAM_COLLECTIONS_DIR")"
    if [[ -z "$collection_filenames" ]]; then
        echo "ERROR: No YAML files found in ${UPSTREAM_COLLECTIONS_DIR}" >&2
        exit 1
    fi

    rm -f "${OUTPUT_DIR}"/collection-*.yaml

    while IFS= read -r filename; do
        echo "Processing collection ${filename}..."
        local cm_file
        if cm_file="$(process_collection "$filename")"; then
            cm_files+=("$cm_file")
            local safe_id="${cm_file#collection-}"
            safe_id="${safe_id%.yaml}"
            collection_ids+=("$safe_id")
        fi
    done <<< "$collection_filenames"

    write_kustomization "${cm_files[@]}"

    echo ""
    echo "Generated ${#cm_files[@]} ConfigMaps in ${OUTPUT_DIR}/"
    echo "Provider IDs: $(IFS=', '; echo "${provider_ids[*]}")"
    echo "Collection IDs: $(IFS=', '; echo "${collection_ids[*]}")"
}

main

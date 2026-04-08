#!/usr/bin/env python3
"""
Fetches provider and collection YAML files from the eval-hub upstream repository
and generates Kubernetes ConfigMap manifests for the operator to deploy.

Usage:
    hack/sync-evalhub-providers.py [branch]

Arguments:
    branch  Git branch to fetch from (default: main)
"""

import json
import sys
import textwrap
import urllib.request
from pathlib import Path

import yaml

REPO = "eval-hub/eval-hub"
UPSTREAM_PROVIDERS_DIR = "config/providers"
UPSTREAM_COLLECTIONS_DIR = "config/collections"
OUTPUT_DIR = Path("config/configmaps/evalhub")

PROVIDER_TYPE_LABEL = "trustyai.opendatahub.io/evalhub-provider-type"
PROVIDER_NAME_LABEL = "trustyai.opendatahub.io/evalhub-provider-name"
COLLECTION_TYPE_LABEL = "trustyai.opendatahub.io/evalhub-collection-type"
COLLECTION_NAME_LABEL = "trustyai.opendatahub.io/evalhub-collection-name"

# Files to exclude from the upstream repository (by filename)
EXCLUDE_FILES = {
    "ragas.yaml",
}

# Providers that reuse an existing kustomize image variable instead of
# the default ``evalhub-provider-<id>-image`` naming convention.
IMAGE_VAR_OVERRIDES: dict[str, str] = {
    "garak": "garak-provider-image",
    "garak-kfp": "garak-provider-image",
    "lm_evaluation_harness": "lmes-pod-image",
}


def fetch_json(url: str):
    with urllib.request.urlopen(url) as resp:
        return json.load(resp)


def fetch_text(url: str) -> str:
    with urllib.request.urlopen(url) as resp:
        return resp.read().decode()


def list_yaml_files(upstream_dir: str, branch: str) -> list[str]:
    api_url = f"https://api.github.com/repos/{REPO}/contents/{upstream_dir}?ref={branch}"
    print(f"Fetching file list from {api_url}")
    entries = fetch_json(api_url)
    return [e["name"] for e in entries if e["name"].endswith((".yaml", ".yml"))]


def process_provider(filename: str, branch: str) -> tuple[str, str] | None:
    """Download a provider YAML, replace the image with a kustomize placeholder,
    and generate a ConfigMap manifest. Returns (cm_filename, provider_id) or None."""
    raw_url = f"https://raw.githubusercontent.com/{REPO}/{branch}/{UPSTREAM_PROVIDERS_DIR}/{filename}"
    content = fetch_text(raw_url)
    data = yaml.safe_load(content)

    provider_id = data.get("id")
    if not provider_id:
        print(f"  SKIP: no 'id' field found in {filename}", file=sys.stderr)
        return None

    # Sanitize for K8s resource names (RFC 1123: only lowercase alphanumeric and hyphens)
    safe_id = provider_id.replace("_", "-")

    cm_file = f"provider-{safe_id}.yaml"
    cm_name = f"evalhub-provider-{safe_id}"
    var_name = IMAGE_VAR_OVERRIDES.get(provider_id, f"evalhub-provider-{safe_id}-image")

    print(f"  id={provider_id} -> {cm_file}")

    # Capture the original image and replace all occurrences with kustomize placeholder
    original_image = None
    if "runtime" in data and "k8s" in data["runtime"]:
        original_image = data["runtime"]["k8s"].get("image")

    provider_yaml = yaml.dump(data, default_flow_style=False, sort_keys=False)
    if original_image:
        provider_yaml = provider_yaml.replace(original_image, f"$({var_name})")

    cm = textwrap.dedent(f"""\
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: {cm_name}
          labels:
            {PROVIDER_TYPE_LABEL}: system
            {PROVIDER_NAME_LABEL}: {safe_id}
        data:
          {filename}: |
        """)
    indented = textwrap.indent(provider_yaml, "    ")

    (OUTPUT_DIR / cm_file).write_text(cm + indented)
    return cm_file, safe_id


def process_collection(filename: str, branch: str) -> tuple[str, str] | None:
    """Download a collection YAML and generate a ConfigMap manifest.
    Returns (cm_filename, collection_id) or None."""
    raw_url = f"https://raw.githubusercontent.com/{REPO}/{branch}/{UPSTREAM_COLLECTIONS_DIR}/{filename}"
    content = fetch_text(raw_url)
    data = yaml.safe_load(content)

    collection_id = data.get("id")
    if not collection_id:
        print(f"  SKIP: no 'id' field found in {filename}", file=sys.stderr)
        return None

    safe_id = collection_id.replace("_", "-")

    cm_file = f"collection-{safe_id}.yaml"
    cm_name = f"evalhub-collection-{safe_id}"

    print(f"  id={collection_id} -> {cm_file}")

    collection_yaml = yaml.dump(data, default_flow_style=False, sort_keys=False)

    cm = textwrap.dedent(f"""\
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: {cm_name}
          labels:
            {COLLECTION_TYPE_LABEL}: system
            {COLLECTION_NAME_LABEL}: {safe_id}
        data:
          {filename}: |
        """)
    indented = textwrap.indent(collection_yaml, "    ")

    (OUTPUT_DIR / cm_file).write_text(cm + indented)
    return cm_file, safe_id


def write_kustomization(cm_files: list[str]):
    lines = ["resources:"]
    for f in cm_files:
        lines.append(f"  - {f}")
    lines.append("")
    lines.append("namespace: system")
    lines.append("")
    (OUTPUT_DIR / "kustomization.yaml").write_text("\n".join(lines))


def main():
    branch = sys.argv[1] if len(sys.argv) > 1 else "main"

    cm_files = []

    # --- Providers ---
    provider_filenames = list_yaml_files(UPSTREAM_PROVIDERS_DIR, branch)
    if not provider_filenames:
        print(f"ERROR: No YAML files found in {UPSTREAM_PROVIDERS_DIR}", file=sys.stderr)
        sys.exit(1)

    # Clean existing provider ConfigMap files
    for old in OUTPUT_DIR.glob("provider-*.yaml"):
        old.unlink()

    provider_ids = []
    for filename in provider_filenames:
        if filename in EXCLUDE_FILES:
            print(f"Skipping {filename} (excluded)")
            continue
        print(f"Processing provider {filename}...")
        result = process_provider(filename, branch)
        if result:
            cm_file, provider_id = result
            cm_files.append(cm_file)
            provider_ids.append(provider_id)

    # --- Collections ---
    collection_filenames = list_yaml_files(UPSTREAM_COLLECTIONS_DIR, branch)
    if not collection_filenames:
        print(f"ERROR: No YAML files found in {UPSTREAM_COLLECTIONS_DIR}", file=sys.stderr)
        sys.exit(1)

    # Clean existing collection ConfigMap files
    for old in OUTPUT_DIR.glob("collection-*.yaml"):
        old.unlink()

    collection_ids = []
    for filename in collection_filenames:
        print(f"Processing collection {filename}...")
        result = process_collection(filename, branch)
        if result:
            cm_file, collection_id = result
            cm_files.append(cm_file)
            collection_ids.append(collection_id)

    write_kustomization(cm_files)

    print(f"\nGenerated {len(cm_files)} ConfigMaps in {OUTPUT_DIR}/")
    print(f"Provider IDs: {', '.join(provider_ids)}")
    print(f"Collection IDs: {', '.join(collection_ids)}")


if __name__ == "__main__":
    main()

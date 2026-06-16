package rbac

import rego.v1

# --- Layer 1: allowlist ---

test_known_api_resource_passes if {
	count(deny) == 0 with input as {
		"kind": "ClusterRole",
		"metadata": {"name": "test-role"},
		"rules": [{"apiGroups": ["apps"], "resources": ["deployments"], "verbs": ["get", "list"]}],
	}
}

test_unknown_api_resource_denied if {
	count(deny) > 0 with input as {
		"kind": "ClusterRole",
		"metadata": {"name": "test-role"},
		"rules": [{"apiGroups": ["example.io"], "resources": ["foos"], "verbs": ["get", "list"]}],
	}
}

test_multiple_resources_one_unknown_denied if {
	count(deny) > 0 with input as {
		"kind": "ClusterRole",
		"metadata": {"name": "test-role"},
		"rules": [
			{"apiGroups": ["apps"], "resources": ["deployments"], "verbs": ["get"]},
			{"apiGroups": ["example.io"], "resources": ["widgets"], "verbs": ["get"]},
		],
	}
}

# --- Layer 2: wildcards ---

test_wildcard_verb_denied if {
	count(deny) > 0 with input as {
		"kind": "ClusterRole",
		"metadata": {"name": "test-role"},
		"rules": [{"apiGroups": ["apps"], "resources": ["deployments"], "verbs": ["*"]}],
	}
}

test_wildcard_resource_denied if {
	count(deny) > 0 with input as {
		"kind": "ClusterRole",
		"metadata": {"name": "test-role"},
		"rules": [{"apiGroups": ["apps"], "resources": ["*"], "verbs": ["get"]}],
	}
}

test_wildcard_apigroup_denied if {
	count(deny) > 0 with input as {
		"kind": "ClusterRole",
		"metadata": {"name": "test-role"},
		"rules": [{"apiGroups": ["*"], "resources": ["pods"], "verbs": ["get"]}],
	}
}

# --- Layer 2: secrets ---

test_secrets_read_allowed if {
	count(deny) == 0 with input as {
		"kind": "ClusterRole",
		"metadata": {"name": "test-role"},
		"rules": [{"apiGroups": [""], "resources": ["secrets"], "verbs": ["get", "list", "watch"]}],
	}
}

test_secrets_write_denied if {
	count(deny) > 0 with input as {
		"kind": "ClusterRole",
		"metadata": {"name": "test-role"},
		"rules": [{"apiGroups": [""], "resources": ["secrets"], "verbs": ["get", "update"]}],
	}
}

test_secrets_delete_denied if {
	count(deny) > 0 with input as {
		"kind": "ClusterRole",
		"metadata": {"name": "test-role"},
		"rules": [{"apiGroups": [""], "resources": ["secrets"], "verbs": ["delete"]}],
	}
}

# --- Layer 2: secrets exemptions ---

test_secrets_write_exempt_tas_manager if {
	count(deny) == 0 with input as {
		"kind": "ClusterRole",
		"metadata": {"name": "trustyai-service-operator-tas-manager-role"},
		"rules": [{"apiGroups": [""], "resources": ["secrets"], "verbs": ["create", "delete"]}],
	}
}

test_secrets_write_exempt_gorch_manager if {
	count(deny) == 0 with input as {
		"kind": "ClusterRole",
		"metadata": {"name": "trustyai-service-operator-gorch-manager-role"},
		"rules": [{"apiGroups": [""], "resources": ["secrets"], "verbs": ["update", "patch"]}],
	}
}

test_secrets_write_not_exempt_unknown_role if {
	count(deny) > 0 with input as {
		"kind": "ClusterRole",
		"metadata": {"name": "trustyai-service-operator-rogue-manager-role"},
		"rules": [{"apiGroups": [""], "resources": ["secrets"], "verbs": ["create"]}],
	}
}

# --- Layer 2: CRB write exemptions ---

test_crb_write_exempt_evalhub_manager if {
	count(deny) == 0 with input as {
		"kind": "ClusterRole",
		"metadata": {"name": "trustyai-service-operator-evalhub-manager-role"},
		"rules": [{"apiGroups": ["rbac.authorization.k8s.io"], "resources": ["clusterrolebindings"], "verbs": ["create", "delete"]}],
	}
}

test_crb_write_exempt_tas_manager if {
	count(deny) == 0 with input as {
		"kind": "ClusterRole",
		"metadata": {"name": "trustyai-service-operator-tas-manager-role"},
		"rules": [{"apiGroups": ["rbac.authorization.k8s.io"], "resources": ["clusterrolebindings"], "verbs": ["update"]}],
	}
}

test_crb_write_not_exempt_unknown_role if {
	count(deny) > 0 with input as {
		"kind": "ClusterRole",
		"metadata": {"name": "trustyai-service-operator-rogue-role"},
		"rules": [{"apiGroups": ["rbac.authorization.k8s.io"], "resources": ["clusterrolebindings"], "verbs": ["create"]}],
	}
}

# --- Layer 2: privilege escalation ---

test_clusterroles_write_denied if {
	count(deny) > 0 with input as {
		"kind": "ClusterRole",
		"metadata": {"name": "test-role"},
		"rules": [{"apiGroups": ["rbac.authorization.k8s.io"], "resources": ["clusterroles"], "verbs": ["create"]}],
	}
}

test_clusterrolebindings_write_denied if {
	count(deny) > 0 with input as {
		"kind": "ClusterRole",
		"metadata": {"name": "test-role"},
		"rules": [{"apiGroups": ["rbac.authorization.k8s.io"], "resources": ["clusterrolebindings"], "verbs": ["patch"]}],
	}
}

test_clusterrolebindings_read_allowed if {
	count(deny) == 0 with input as {
		"kind": "ClusterRole",
		"metadata": {"name": "test-role"},
		"rules": [{"apiGroups": ["rbac.authorization.k8s.io"], "resources": ["clusterrolebindings"], "verbs": ["get", "list"]}],
	}
}

# --- Layer 2: escalation verbs ---

test_escalate_verb_denied if {
	count(deny) > 0 with input as {
		"kind": "ClusterRole",
		"metadata": {"name": "test-role"},
		"rules": [{"apiGroups": ["rbac.authorization.k8s.io"], "resources": ["roles"], "verbs": ["escalate"]}],
	}
}

test_bind_verb_denied if {
	count(deny) > 0 with input as {
		"kind": "ClusterRole",
		"metadata": {"name": "test-role"},
		"rules": [{"apiGroups": ["rbac.authorization.k8s.io"], "resources": ["roles"], "verbs": ["bind"]}],
	}
}

test_impersonate_verb_denied if {
	count(deny) > 0 with input as {
		"kind": "ClusterRole",
		"metadata": {"name": "test-role"},
		"rules": [{"apiGroups": [""], "resources": ["serviceaccounts"], "verbs": ["impersonate"]}],
	}
}

# --- Non-ClusterRole input ---

test_non_clusterrole_ignored if {
	count(deny) == 0 with input as {
		"kind": "Role",
		"metadata": {"name": "test-role"},
		"rules": [{"apiGroups": ["evil.io"], "resources": ["badthings"], "verbs": ["*"]}],
	}
}

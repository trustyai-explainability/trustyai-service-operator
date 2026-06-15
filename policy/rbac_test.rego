package rbac

test_base_auth_delegator_passes if {
	count(deny) == 0 with input as {
		"kind": "ClusterRoleBinding",
		"metadata": {"name": "manager-auth-delegator"},
		"roleRef": {"name": "system:auth-delegator"},
	}
}

test_prefixed_auth_delegator_passes if {
	count(deny) == 0 with input as {
		"kind": "ClusterRoleBinding",
		"metadata": {"name": "trustyai-service-operator-manager-auth-delegator"},
		"roleRef": {"name": "system:auth-delegator"},
	}
}

test_proxy_rolebinding_passes if {
	count(deny) == 0 with input as {
		"kind": "ClusterRoleBinding",
		"metadata": {"name": "trustyai-service-operator-proxy-rolebinding"},
		"roleRef": {"name": "trustyai-service-operator-proxy-role"},
	}
}

test_evalhub_manager_passes if {
	count(deny) == 0 with input as {
		"kind": "ClusterRoleBinding",
		"metadata": {"name": "trustyai-service-operator-evalhub-manager-rolebinding"},
		"roleRef": {"name": "trustyai-service-operator-evalhub-manager-role"},
	}
}

test_lmes_manager_passes if {
	count(deny) == 0 with input as {
		"kind": "ClusterRoleBinding",
		"metadata": {"name": "trustyai-service-operator-lmes-manager-rolebinding"},
		"roleRef": {"name": "trustyai-service-operator-lmes-manager-role"},
	}
}

test_unexpected_crb_denied if {
	count(deny) > 0 with input as {
		"kind": "ClusterRoleBinding",
		"metadata": {"name": "rogue-crb"},
		"roleRef": {"name": "manager-role"},
	}
}

test_wrong_role_denied if {
	count(deny) > 0 with input as {
		"kind": "ClusterRoleBinding",
		"metadata": {"name": "trustyai-service-operator-proxy-rolebinding"},
		"roleRef": {"name": "some-other-role"},
	}
}

test_non_crb_ignored if {
	count(deny) == 0 with input as {
		"kind": "RoleBinding",
		"metadata": {"name": "anything"},
		"roleRef": {"name": "anything"},
	}
}

test_clusterrole_ignored if {
	count(deny) == 0 with input as {
		"kind": "ClusterRole",
		"metadata": {"name": "trustyai-service-operator-manager-role"},
	}
}

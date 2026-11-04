package rbac

import rego.v1

base_nemo_manager_role := {
	"kind": "ClusterRole",
	"metadata": {"name": "nemo-guardrails-manager-role"},
	"rules": [
		{"apiGroups": ["networking.k8s.io"], "resources": ["networkpolicies"], "verbs": ["get", "list", "watch", "create", "update", "patch", "delete"]},
	],
}

test_nemo_manager_networkpolicy_permissions_pass if {
	count(deny) == 0 with input as base_nemo_manager_role
}

test_nemo_manager_requires_networkpolicy_permissions if {
	role := object.union(base_nemo_manager_role, {
		"rules": [{"apiGroups": [""], "resources": ["services"], "verbs": ["get"]}],
	})
	count(deny) > 0 with input as role
}

test_nemo_manager_rejects_missing_networkpolicy_delete if {
	role := object.union(base_nemo_manager_role, {
		"rules": [
			{"apiGroups": ["networking.k8s.io"], "resources": ["networkpolicies"], "verbs": ["get", "list", "watch", "create", "update", "patch"]},
		],
	})
	count(deny) > 0 with input as role
}

test_nemo_manager_rejects_extra_networkpolicy_resources if {
	role := object.union(base_nemo_manager_role, {
		"rules": [
			{"apiGroups": ["networking.k8s.io"], "resources": ["networkpolicies", "pods"], "verbs": ["get", "list", "watch", "create", "update", "patch", "delete"]},
		],
	})
	count(deny) > 0 with input as role
}

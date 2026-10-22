package rbac

import rego.v1

base_tas_manager_role := {
	"kind": "ClusterRole",
	"metadata": {"name": "tas-manager-role"},
	"rules": [
		{"apiGroups": ["networking.k8s.io"], "resources": ["networkpolicies"], "verbs": ["get", "list", "watch", "create", "update"]},
		{"apiGroups": ["apps"], "resources": ["replicasets"], "verbs": ["get"]},
	],
}

test_tas_manager_networkpolicy_and_replicaset_permissions_pass if {
	count(deny) == 0 with input as base_tas_manager_role
}

test_tas_manager_requires_networkpolicy_permissions if {
	role := object.union(base_tas_manager_role, {
		"rules": [{"apiGroups": ["apps"], "resources": ["replicasets"], "verbs": ["get"]}],
	})
	count(deny) > 0 with input as role
}

test_tas_manager_rejects_networkpolicy_delete if {
	role := object.union(base_tas_manager_role, {
		"rules": [
			{"apiGroups": ["networking.k8s.io"], "resources": ["networkpolicies"], "verbs": ["get", "list", "watch", "create", "update", "delete"]},
			{"apiGroups": ["apps"], "resources": ["replicasets"], "verbs": ["get"]},
		],
	})
	count(deny) > 0 with input as role
}

test_tas_manager_rejects_extra_networkpolicy_resources if {
	role := object.union(base_tas_manager_role, {
		"rules": [
			{"apiGroups": ["networking.k8s.io"], "resources": ["networkpolicies", "pods"], "verbs": ["get", "list", "watch", "create", "update"]},
			{"apiGroups": ["apps"], "resources": ["replicasets"], "verbs": ["get"]},
		],
	})
	count(deny) > 0 with input as role
}

test_tas_manager_rejects_unneeded_replicaset_list_watch if {
	role := object.union(base_tas_manager_role, {
		"rules": [
			{"apiGroups": ["networking.k8s.io"], "resources": ["networkpolicies"], "verbs": ["get", "list", "watch", "create", "update"]},
			{"apiGroups": ["apps"], "resources": ["replicasets"], "verbs": ["get", "list", "watch"]},
		],
	})
	count(deny) > 0 with input as role
}

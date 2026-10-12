package rbac

import rego.v1

base_evalhub_jobs_writer := {
	"kind": "ClusterRole",
	"metadata": {"name": "trustyai-service-operator-evalhub-jobs-writer"},
	"rules": [
		{"apiGroups": ["batch"], "resources": ["jobs"], "verbs": ["create", "delete", "get", "list", "patch"]},
		{"apiGroups": ["networking.k8s.io"], "resources": ["networkpolicies"], "verbs": ["create", "get", "update"]},
	],
}

test_evalhub_jobs_writer_minimal_networkpolicy_permissions_pass if {
	count(deny) == 0 with input as base_evalhub_jobs_writer
}

test_evalhub_jobs_writer_networkpolicy_delete_is_denied if {
	role := object.union(base_evalhub_jobs_writer, {
		"rules": [
			{"apiGroups": ["batch"], "resources": ["jobs"], "verbs": ["create", "delete", "get", "list", "patch"]},
			{"apiGroups": ["networking.k8s.io"], "resources": ["networkpolicies"], "verbs": ["create", "delete", "get", "update"]},
		],
	})
	count(deny) > 0 with input as role
}

test_evalhub_jobs_writer_requires_networkpolicy_permissions if {
	role := object.union(base_evalhub_jobs_writer, {
		"rules": [{"apiGroups": ["batch"], "resources": ["jobs"], "verbs": ["create", "delete", "get", "list", "patch"]}],
	})
	count(deny) > 0 with input as role
}

package rbac

import rego.v1

base_evalhub_hardware_profiles_reader := {
	"kind": "ClusterRole",
	"metadata": {"name": "trustyai-service-operator-evalhub-hardware-profiles-reader"},
	"rules": [
		{"apiGroups": ["infrastructure.opendatahub.io"], "resources": ["hardwareprofiles"], "verbs": ["get", "list"]},
		{"apiGroups": ["kueue.x-k8s.io"], "resources": ["localqueues"], "verbs": ["list"]},
	],
}

test_evalhub_hardware_profiles_reader_localqueue_list_pass if {
	count(deny) == 0 with input as base_evalhub_hardware_profiles_reader
}

test_evalhub_hardware_profiles_reader_unprefixed_name_pass if {
	role := object.union(base_evalhub_hardware_profiles_reader, {
		"metadata": {"name": "evalhub-hardware-profiles-reader"},
	})
	count(deny) == 0 with input as role
}

test_evalhub_hardware_profiles_reader_localqueue_missing_is_denied if {
	role := object.union(base_evalhub_hardware_profiles_reader, {
		"rules": [
			{"apiGroups": ["infrastructure.opendatahub.io"], "resources": ["hardwareprofiles"], "verbs": ["get", "list"]},
		],
	})
	count(deny) > 0 with input as role
}

test_evalhub_hardware_profiles_reader_localqueue_extra_verbs_are_denied if {
	role := object.union(base_evalhub_hardware_profiles_reader, {
		"rules": [
			{"apiGroups": ["infrastructure.opendatahub.io"], "resources": ["hardwareprofiles"], "verbs": ["get", "list"]},
			{"apiGroups": ["kueue.x-k8s.io"], "resources": ["localqueues"], "verbs": ["get", "list", "watch"]},
		],
	})
	count(deny) > 0 with input as role
}

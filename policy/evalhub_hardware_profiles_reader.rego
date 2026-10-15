package rbac

import rego.v1

# The EvalHub API service receives this ClusterRole through namespaced RoleBindings
# in authorized tenant namespaces. The API lists Kueue LocalQueues in the tenant
# namespace to resolve queue-backed hardware profiles for GET /api/v1/info; it
# performs a single list call per request, so only "list" is granted.
evalhub_hardware_profiles_reader_names := {
	"evalhub-hardware-profiles-reader",
	"trustyai-service-operator-evalhub-hardware-profiles-reader",
}

evalhub_localqueue_verbs := {"list"}

is_evalhub_localqueue_rule(rule) if {
	"kueue.x-k8s.io" in rule.apiGroups
	"localqueues" in rule.resources
}

has_evalhub_localqueue_rule if {
	some rule in input.rules
	is_evalhub_localqueue_rule(rule)
}

deny contains msg if {
	input.kind == "ClusterRole"
	evalhub_hardware_profiles_reader_names[input.metadata.name]
	not has_evalhub_localqueue_rule
	msg := sprintf("RBAC VIOLATION: EvalHub hardware-profiles-reader ClusterRole '%s' must grant LocalQueue list.", [input.metadata.name])
}

deny contains msg if {
	input.kind == "ClusterRole"
	evalhub_hardware_profiles_reader_names[input.metadata.name]
	rule := input.rules[_]
	is_evalhub_localqueue_rule(rule)
	verbs := {verb | verb := rule.verbs[_]}
	verbs != evalhub_localqueue_verbs
	msg := sprintf("RBAC VIOLATION: EvalHub hardware-profiles-reader ClusterRole '%s' must grant only LocalQueue list, got %v.", [input.metadata.name, verbs])
}

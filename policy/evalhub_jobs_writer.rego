package rbac

import rego.v1

# The EvalHub API service receives this ClusterRole through namespaced RoleBindings
# in authorized tenant namespaces. Its per-job NetworkPolicy lifecycle requires
# create/get/update plus delete: OwnerReferencesPermissionEnforcement checks delete
# permission when EvalHub changes the policy owner from the ConfigMap to the Job.
evalhub_jobs_writer_names := {
	"evalhub-jobs-writer",
	"trustyai-service-operator-evalhub-jobs-writer",
}

evalhub_jobs_networkpolicy_verbs := {"create", "delete", "get", "update"}

is_evalhub_jobs_networkpolicy_rule(rule) if {
	"networking.k8s.io" in rule.apiGroups
	"networkpolicies" in rule.resources
}

has_evalhub_jobs_networkpolicy_rule if {
	some rule in input.rules
	is_evalhub_jobs_networkpolicy_rule(rule)
}

deny contains msg if {
	input.kind == "ClusterRole"
	evalhub_jobs_writer_names[input.metadata.name]
	not has_evalhub_jobs_networkpolicy_rule
	msg := sprintf("RBAC VIOLATION: EvalHub jobs-writer ClusterRole '%s' must grant NetworkPolicy create/delete/get/update.", [input.metadata.name])
}

deny contains msg if {
	input.kind == "ClusterRole"
	evalhub_jobs_writer_names[input.metadata.name]
	rule := input.rules[_]
	is_evalhub_jobs_networkpolicy_rule(rule)
	verbs := {verb | verb := rule.verbs[_]}
	verbs != evalhub_jobs_networkpolicy_verbs
	msg := sprintf("RBAC VIOLATION: EvalHub jobs-writer ClusterRole '%s' must grant only NetworkPolicy create/delete/get/update, got %v.", [input.metadata.name, verbs])
}

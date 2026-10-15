package rbac

import rego.v1

nemo_manager_role_names := {"nemo-guardrails-manager-role"}

nemo_networkpolicy_verbs := {"get", "list", "watch", "create", "update"}

nemo_replicaset_verbs := {"get"}

is_nemo_networkpolicy_rule(rule) if {
	"networking.k8s.io" in rule.apiGroups
	"networkpolicies" in rule.resources
}

is_nemo_replicaset_rule(rule) if {
	"apps" in rule.apiGroups
	"replicasets" in rule.resources
}

has_nemo_networkpolicy_rule if {
	some rule in input.rules
	is_nemo_networkpolicy_rule(rule)
}

has_nemo_replicaset_rule if {
	some rule in input.rules
	is_nemo_replicaset_rule(rule)
}

deny contains msg if {
	input.kind == "ClusterRole"
	nemo_manager_role_names[input.metadata.name]
	not has_nemo_networkpolicy_rule
	msg := "RBAC VIOLATION: NemoGuardrails manager ClusterRole must grant NetworkPolicy get/list/watch/create/update."
}

deny contains msg if {
	input.kind == "ClusterRole"
	nemo_manager_role_names[input.metadata.name]
	rule := input.rules[_]
	is_nemo_networkpolicy_rule(rule)
	verbs := {verb | verb := rule.verbs[_]}
	verbs != nemo_networkpolicy_verbs
	msg := sprintf("RBAC VIOLATION: NemoGuardrails manager ClusterRole must grant only NetworkPolicy get/list/watch/create/update, got %v.", [verbs])
}

deny contains msg if {
	input.kind == "ClusterRole"
	nemo_manager_role_names[input.metadata.name]
	rule := input.rules[_]
	is_nemo_networkpolicy_rule(rule)
	groups := {group | group := rule.apiGroups[_]}
	groups != {"networking.k8s.io"}
	msg := sprintf("RBAC VIOLATION: NemoGuardrails NetworkPolicy rule must target only networking.k8s.io, got %v.", [groups])
}

deny contains msg if {
	input.kind == "ClusterRole"
	nemo_manager_role_names[input.metadata.name]
	rule := input.rules[_]
	is_nemo_networkpolicy_rule(rule)
	resources := {resource | resource := rule.resources[_]}
	resources != {"networkpolicies"}
	msg := sprintf("RBAC VIOLATION: NemoGuardrails NetworkPolicy rule must target only networkpolicies, got %v.", [resources])
}

deny contains msg if {
	input.kind == "ClusterRole"
	nemo_manager_role_names[input.metadata.name]
	not has_nemo_replicaset_rule
	msg := "RBAC VIOLATION: NemoGuardrails manager ClusterRole must grant ReplicaSet get to verify existing Pod ownership."
}

deny contains msg if {
	input.kind == "ClusterRole"
	nemo_manager_role_names[input.metadata.name]
	rule := input.rules[_]
	is_nemo_replicaset_rule(rule)
	verbs := {verb | verb := rule.verbs[_]}
	verbs != nemo_replicaset_verbs
	msg := sprintf("RBAC VIOLATION: NemoGuardrails manager ClusterRole must grant only ReplicaSet get, got %v.", [verbs])
}

deny contains msg if {
	input.kind == "ClusterRole"
	nemo_manager_role_names[input.metadata.name]
	rule := input.rules[_]
	is_nemo_replicaset_rule(rule)
	groups := {group | group := rule.apiGroups[_]}
	groups != {"apps"}
	msg := sprintf("RBAC VIOLATION: NemoGuardrails ReplicaSet rule must target only apps, got %v.", [groups])
}

deny contains msg if {
	input.kind == "ClusterRole"
	nemo_manager_role_names[input.metadata.name]
	rule := input.rules[_]
	is_nemo_replicaset_rule(rule)
	resources := {resource | resource := rule.resources[_]}
	resources != {"replicasets"}
	msg := sprintf("RBAC VIOLATION: NemoGuardrails ReplicaSet rule must target only replicasets, got %v.", [resources])
}

package rbac

import rego.v1

# The component manifest carries the bare role name; every overlay in
# config/overlays applies namePrefix, so policy-check and CI only ever see the
# prefixed name. Both forms must match or the guard below never fires.
nemo_manager_role_names := {
	"nemo-guardrails-manager-role",
	"trustyai-service-operator-nemo-guardrails-manager-role",
}

# NeMo reconciles its operand NetworkPolicy in the workload namespace, which can
# differ from the custom resource namespace. Cross-namespace policies cannot carry
# an owner reference, so the controller also deletes them during operand cleanup.
nemo_networkpolicy_verbs := {"get", "list", "watch", "create", "update", "patch", "delete"}

is_nemo_networkpolicy_rule(rule) if {
	"networking.k8s.io" in rule.apiGroups
	"networkpolicies" in rule.resources
}

has_nemo_networkpolicy_rule if {
	some rule in input.rules
	is_nemo_networkpolicy_rule(rule)
}

deny contains msg if {
	input.kind == "ClusterRole"
	nemo_manager_role_names[input.metadata.name]
	not has_nemo_networkpolicy_rule
	msg := "RBAC VIOLATION: NemoGuardrails manager ClusterRole must grant NetworkPolicy get/list/watch/create/update/patch/delete."
}

deny contains msg if {
	input.kind == "ClusterRole"
	nemo_manager_role_names[input.metadata.name]
	rule := input.rules[_]
	is_nemo_networkpolicy_rule(rule)
	verbs := {verb | verb := rule.verbs[_]}
	verbs != nemo_networkpolicy_verbs
	msg := sprintf("RBAC VIOLATION: NemoGuardrails manager ClusterRole must grant only NetworkPolicy get/list/watch/create/update/patch/delete, got %v.", [verbs])
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

package rbac

import rego.v1

# Closed allowlist of every legitimate ClusterRoleBinding across all
# kustomize overlays (base, odh, rhoai, lmes, odh-kueue, testing,
# mcp-guardrails).
#
# The map key is the post-kustomize CRB name, the value is the
# ClusterRole it must reference.  Overlays that do not apply the
# namePrefix (base) produce un-prefixed names; those are listed
# separately.
#
# To add a new legitimate CRB, add it here and explain why in the PR.

expected_crbs := {
	# --- rbac-base (all overlays) ---
	"manager-auth-delegator": "system:auth-delegator",
	"proxy-rolebinding": "proxy-role",
	"trustyai-service-operator-manager-auth-delegator": "system:auth-delegator",
	"trustyai-service-operator-proxy-rolebinding": "trustyai-service-operator-proxy-role",

	# --- component: tas ---
	"trustyai-service-operator-tas-manager-rolebinding": "trustyai-service-operator-tas-manager-role",

	# --- component: lmes ---
	"trustyai-service-operator-lmes-manager-rolebinding": "trustyai-service-operator-lmes-manager-role",
	"trustyai-service-operator-default-lmeval-user-rolebinding": "trustyai-service-operator-lmeval-user-role",

	# --- component: evalhub (prefixed overlays: odh, rhoai, dev, testing) ---
	"trustyai-service-operator-evalhub-manager-rolebinding": "trustyai-service-operator-evalhub-manager-role",
	"trustyai-service-operator-evalhub-collections-access-binding": "trustyai-service-operator-evalhub-collections-access",
	"trustyai-service-operator-evalhub-providers-access-binding": "trustyai-service-operator-evalhub-providers-access",
	"trustyai-service-operator-evalhub-mlflow-access-binding": "trustyai-service-operator-evalhub-mlflow-access",
	"trustyai-service-operator-evalhub-mlflow-jobs-access-binding": "trustyai-service-operator-evalhub-mlflow-jobs-access",
	"trustyai-service-operator-evalhub-jobs-writer-binding": "trustyai-service-operator-evalhub-jobs-writer",
	"trustyai-service-operator-evalhub-job-config-binding": "trustyai-service-operator-evalhub-job-config",

	# --- component: evalhub (un-prefixed overlay: evalhub-only) ---
	"evalhub-manager-rolebinding": "trustyai-service-operator-evalhub-manager-role",
	"evalhub-collections-access-binding": "evalhub-collections-access",
	"evalhub-providers-access-binding": "evalhub-providers-access",
	"evalhub-mlflow-access-binding": "evalhub-mlflow-access",
	"evalhub-mlflow-jobs-access-binding": "evalhub-mlflow-jobs-access",
	"evalhub-jobs-writer-binding": "evalhub-jobs-writer",
	"evalhub-job-config-binding": "evalhub-job-config",

	# --- component: gorch ---
	"trustyai-service-operator-gorch-manager-rolebinding": "trustyai-service-operator-gorch-manager-role",

	# --- component: nemo-guardrails ---
	"trustyai-service-operator-nemo-guardrails-manager-rolebinding": "trustyai-service-operator-nemo-guardrails-manager-role",

	# --- component: job-mgr ---
	"trustyai-service-operator-job-mgr-manager-rolebinding": "trustyai-service-operator-job-mgr-manager-role",
}

deny contains msg if {
	input.kind == "ClusterRoleBinding"
	name := input.metadata.name
	not expected_crbs[name]
	msg := sprintf(
		"RBAC VIOLATION: unexpected ClusterRoleBinding '%s' binding '%s'. Add to policy/rbac.rego allowlist if intentional.",
		[name, input.roleRef.name],
	)
}

deny contains msg if {
	input.kind == "ClusterRoleBinding"
	name := input.metadata.name
	expected_crbs[name]
	input.roleRef.name != expected_crbs[name]
	msg := sprintf(
		"RBAC VIOLATION: ClusterRoleBinding '%s' expected to bind '%s' but binds '%s'.",
		[name, expected_crbs[name], input.roleRef.name],
	)
}

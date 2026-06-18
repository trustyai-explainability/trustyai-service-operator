package rbac

import rego.v1

# Closed allowlist of (apiGroup, resource) pairs permitted in ClusterRole
# rules across all kustomize overlays.
#
# To add a new permission, add the pair here and explain why in the PR.

allowed_api_resources := {
	# --- core ---
	["", "configmaps"],
	["", "events"],
	["", "namespaces"],
	["", "persistentvolumeclaims"],
	["", "persistentvolumes"],
	["", "pods"],
	["", "pods/exec"],
	["", "secrets"],
	["", "serviceaccounts"],
	["", "services"],

	# --- apps ---
	["apps", "deployments"],
	["apps", "deployments/finalizers"],
	["apps", "deployments/status"],

	# --- batch ---
	["batch", "jobs"],

	# --- coordination ---
	["coordination.k8s.io", "leases"],

	# --- scheduling ---
	["scheduling.k8s.io", "priorityclasses"],

	# --- apiextensions ---
	["apiextensions.k8s.io", "customresourcedefinitions"],

	# --- auth ---
	["authentication.k8s.io", "tokenreviews"],
	["authorization.k8s.io", "subjectaccessreviews"],

	# --- RBAC ---
	["rbac.authorization.k8s.io", "clusterroles"],
	["rbac.authorization.k8s.io", "clusterrolebindings"],
	["rbac.authorization.k8s.io", "roles"],
	["rbac.authorization.k8s.io", "rolebindings"],

	# --- openshift config ---
	["config.openshift.io", "apiservers"],

	# --- networking / routes ---
	["gateway.networking.k8s.io", "gateways"],
	["mcp.kuadrant.io", "mcpgatewayextensions"],
	["networking.istio.io", "destinationrules"],
	["networking.istio.io", "envoyfilters"],
	["networking.istio.io", "virtualservices"],
	["route.openshift.io", "routes"],

	# --- monitoring ---
	["monitoring.coreos.com", "servicemonitors"],

	# --- kserve ---
	["serving.kserve.io", "inferenceservices"],
	["serving.kserve.io", "inferenceservices/finalizers"],
	["serving.kserve.io", "servingruntimes"],

	# --- kueue ---
	["kueue.x-k8s.io", "resourceflavors"],
	["kueue.x-k8s.io", "workloads"],
	["kueue.x-k8s.io", "workloads/finalizers"],
	["kueue.x-k8s.io", "workloads/status"],
	["kueue.x-k8s.io", "workloadpriorityclasses"],

	# --- infrastructure ---
	["infrastructure.opendatahub.io", "hardwareprofiles"],

	# --- mlflow ---
	["mlflow.kubeflow.org", "experiments"],

	# --- trustyai ---
	["trustyai.opendatahub.io", "collections"],
	["trustyai.opendatahub.io", "evaluations"],
	["trustyai.opendatahub.io", "evalhubs"],
	["trustyai.opendatahub.io", "evalhubs/finalizers"],
	["trustyai.opendatahub.io", "evalhubs/proxy"],
	["trustyai.opendatahub.io", "evalhubs/status"],
	["trustyai.opendatahub.io", "guardrailsorchestrators"],
	["trustyai.opendatahub.io", "guardrailsorchestrators/finalizers"],
	["trustyai.opendatahub.io", "guardrailsorchestrators/status"],
	["trustyai.opendatahub.io", "lmevaljobs"],
	["trustyai.opendatahub.io", "lmevaljobs/finalizers"],
	["trustyai.opendatahub.io", "lmevaljobs/status"],
	["trustyai.opendatahub.io", "nemoguardrails"],
	["trustyai.opendatahub.io", "nemoguardrails/finalizers"],
	["trustyai.opendatahub.io", "nemoguardrails/status"],
	["trustyai.opendatahub.io", "providers"],
	["trustyai.opendatahub.io", "status-events"],
	["trustyai.opendatahub.io", "trustyaiservices"],
	["trustyai.opendatahub.io", "trustyaiservices/finalizers"],
	["trustyai.opendatahub.io", "trustyaiservices/status"],
}

# Layer 1: deny unknown (apiGroup, resource) pairs in ClusterRole rules.

deny contains msg if {
	input.kind == "ClusterRole"
	rule := input.rules[i]
	group := rule.apiGroups[_]
	res := rule.resources[_]
	group != "*"
	res != "*"
	not allowed_api_resources[[group, res]]
	msg := sprintf(
		"RBAC VIOLATION: ClusterRole '%s' requests (%s, %s) which is not in the allowlist. Add to policy/clusterrole.rego if intentional.",
		[input.metadata.name, group, res],
	)
}

# Layer 2: deny wildcard apiGroups.

deny contains msg if {
	input.kind == "ClusterRole"
	rule := input.rules[_]
	"*" in rule.apiGroups
	msg := sprintf(
		"RBAC VIOLATION: ClusterRole '%s' uses wildcard apiGroup '*'.",
		[input.metadata.name],
	)
}

# Layer 2: deny wildcard resources.

deny contains msg if {
	input.kind == "ClusterRole"
	rule := input.rules[_]
	"*" in rule.resources
	msg := sprintf(
		"RBAC VIOLATION: ClusterRole '%s' uses wildcard resource '*'.",
		[input.metadata.name],
	)
}

# Layer 2: deny wildcard verbs.

deny contains msg if {
	input.kind == "ClusterRole"
	rule := input.rules[_]
	"*" in rule.verbs
	msg := sprintf(
		"RBAC VIOLATION: ClusterRole '%s' uses wildcard verb '*'.",
		[input.metadata.name],
	)
}

# Layer 2: deny write verbs on secrets.
#
# Exempt roles are manager roles that legitimately create/manage secrets
# for TLS certificates or service credentials in their managed workloads.

write_verbs := {"create", "update", "patch", "delete", "deletecollection"}

secrets_write_exempt_suffixes := {
	"tas-manager-role",
	"gorch-manager-role",
	"nemo-guardrails-manager-role",
	"evalhub-model-secret",
}

is_secrets_write_exempt(name) if {
	some suffix in secrets_write_exempt_suffixes
	endswith(name, suffix)
}

deny contains msg if {
	input.kind == "ClusterRole"
	not is_secrets_write_exempt(input.metadata.name)
	rule := input.rules[i]
	"" in rule.apiGroups
	"secrets" in rule.resources
	verb := rule.verbs[_]
	write_verbs[verb]
	msg := sprintf(
		"RBAC VIOLATION: ClusterRole '%s' has write verb '%s' on secrets.",
		[input.metadata.name, verb],
	)
}

# Layer 2: deny write verbs on clusterroles / clusterrolebindings.
#
# Exempt roles are manager roles that create CRBs for their managed
# workloads (e.g. binding service accounts to component-specific roles).

crb_write_exempt_suffixes := {
	"tas-manager-role",
	"evalhub-manager-role",
	"nemo-guardrails-manager-role",
}

is_crb_write_exempt(name) if {
	some suffix in crb_write_exempt_suffixes
	endswith(name, suffix)
}

deny contains msg if {
	input.kind == "ClusterRole"
	not is_crb_write_exempt(input.metadata.name)
	rule := input.rules[i]
	"rbac.authorization.k8s.io" in rule.apiGroups
	escalation_target := {"clusterroles", "clusterrolebindings"}
	res := rule.resources[_]
	escalation_target[res]
	verb := rule.verbs[_]
	write_verbs[verb]
	msg := sprintf(
		"RBAC VIOLATION: ClusterRole '%s' has write verb '%s' on %s (privilege escalation).",
		[input.metadata.name, verb, res],
	)
}

# Layer 2: deny escalation verbs.
#
# evalhub-manager-role is exempt for "bind" only: it uses bind on a specific
# ClusterRole (resourceNames-scoped) to delegate secret access to the eval-hub
# service SA without holding raw secret write verbs itself.

escalation_verbs := {"escalate", "bind", "impersonate"}

bind_exempt_suffixes := {"evalhub-manager-role"}

is_bind_exempt(name) if {
	some suffix in bind_exempt_suffixes
	endswith(name, suffix)
}

is_exempt_bind(name, verb) if {
	verb == "bind"
	is_bind_exempt(name)
}

deny contains msg if {
	input.kind == "ClusterRole"
	rule := input.rules[_]
	verb := rule.verbs[_]
	escalation_verbs[verb]
	not is_exempt_bind(input.metadata.name, verb)
	msg := sprintf(
		"RBAC VIOLATION: ClusterRole '%s' uses escalation verb '%s'.",
		[input.metadata.name, verb],
	)
}

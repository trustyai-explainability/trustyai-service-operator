package networkpolicy

import rego.v1

# Shape policy for the TrustyAI operator manager-pod NetworkPolicy
# (controller-manager; RHOAIENG-96335).
#
# The operator's own ingress policy must:
#   - allow TCP 8443 (secure metrics) only from the supported OpenShift
#     monitoring namespaces, via namespaceSelector-only peers;
#   - allow TCP 8081 (kubelet health probes) port-scoped only, because
#     node-originated probes are not reliably constrainable by peer
#     selectors;
#   - allow TCP 9443 (CRD conversion webhooks) port-scoped only, because
#     the calling Kubernetes API server is host-networked with a dynamic
#     address and port;
#   - stay ingress-only: operator egress is tracked separately (HPSTRAT-278).

monitoring_namespaces := {"openshift-monitoring", "openshift-user-workload-monitoring"}

operator_ingress_ports := {"8443", "8081", "9443"}

selects_operator_pods if {
	input.kind == "NetworkPolicy"
	input.spec.podSelector.matchLabels["control-plane"] == "trustyai-service-operator"
}

port_key(port) := sprintf("%v", [port])

rule_allows_port(rule, port) if {
	some p in rule.ports
	port_key(p.port) == port
}

has_ingress_rule_for(port) if {
	selects_operator_pods
	some rule in input.spec.ingress
	rule_allows_port(rule, port)
}

has_ports(rule) if {
	count(rule.ports) > 0
}

# --- policy types ---------------------------------------------------------

deny contains msg if {
	selects_operator_pods
	"Egress" in input.spec.policyTypes
	msg := "OPERATOR NP VIOLATION: the operator manager-pod NetworkPolicy must not restrict or broaden Egress (operator egress is tracked separately under HPSTRAT-278)."
}

deny contains msg if {
	selects_operator_pods
	not "Ingress" in input.spec.policyTypes
	msg := "OPERATOR NP VIOLATION: the operator manager-pod NetworkPolicy must declare the Ingress policy type."
}

# --- every ingress rule is port-scoped to a known listener -----------------

deny contains msg if {
	selects_operator_pods
	some rule in input.spec.ingress
	not has_ports(rule)
	msg := "OPERATOR NP VIOLATION: ingress rules on the operator manager pod must be port-scoped; found a rule with no ports."
}

deny contains msg if {
	selects_operator_pods
	some rule in input.spec.ingress
	some p in rule.ports
	not port_key(p.port) in operator_ingress_ports
	msg := sprintf("OPERATOR NP VIOLATION: ingress port %v is not one of the operator manager pod's listeners (8443 metrics, 8081 probes, 9443 webhooks).", [p.port])
}

# --- required rules --------------------------------------------------------

deny contains msg if {
	selects_operator_pods
	not has_ingress_rule_for("8443")
	msg := "OPERATOR NP VIOLATION: TCP 8443 (secure metrics) ingress from the supported OpenShift monitoring namespaces is required."
}

deny contains msg if {
	selects_operator_pods
	not has_ingress_rule_for("8081")
	msg := "OPERATOR NP VIOLATION: TCP 8081 (health/readiness probes) ingress is required."
}

deny contains msg if {
	selects_operator_pods
	not has_ingress_rule_for("9443")
	msg := "OPERATOR NP VIOLATION: TCP 9443 (CRD conversion webhooks) ingress is required."
}

# --- the metrics rule is scoped to the monitoring namespaces ----------------

deny contains msg if {
	selects_operator_pods
	some rule in input.spec.ingress
	rule_allows_port(rule, "8443")
	namespaces := {ns | some peer in rule.from; ns := peer.namespaceSelector.matchLabels["kubernetes.io/metadata.name"]}
	namespaces != monitoring_namespaces
	msg := sprintf("OPERATOR NP VIOLATION: the TCP 8443 (metrics) rule must allow exactly the supported OpenShift monitoring namespaces %v, got %v.", [monitoring_namespaces, namespaces])
}

deny contains msg if {
	selects_operator_pods
	some rule in input.spec.ingress
	rule_allows_port(rule, "8443")
	some peer in rule.from
	peer.podSelector
	msg := "OPERATOR NP VIOLATION: the TCP 8443 (metrics) rule must use namespaceSelector-only peers (monitoring scrapers are selected by namespace)."
}

deny contains msg if {
	selects_operator_pods
	some rule in input.spec.ingress
	rule_allows_port(rule, "8443")
	some peer in rule.from
	peer.ipBlock
	msg := "OPERATOR NP VIOLATION: the TCP 8443 (metrics) rule must not use ipBlock peers."
}

deny contains msg if {
	selects_operator_pods
	some rule in input.spec.ingress
	rule_allows_port(rule, "8443")
	some peer in rule.from
	not peer.namespaceSelector
	msg := "OPERATOR NP VIOLATION: the TCP 8443 (metrics) rule must not contain an allow-all or pod/ipBlock peer."
}

deny contains msg if {
	selects_operator_pods
	some rule in input.spec.ingress
	rule_allows_port(rule, "8443")
	some peer in rule.from
	not peer.namespaceSelector.matchLabels["kubernetes.io/metadata.name"]
	msg := "OPERATOR NP VIOLATION: the TCP 8443 (metrics) rule must select monitoring namespaces by kubernetes.io/metadata.name, not an open namespaceSelector."
}

# --- probe and webhook rules are port-scoped only ---------------------------

deny contains msg if {
	selects_operator_pods
	some rule in input.spec.ingress
	rule_allows_port(rule, "8081")
	some peer in rule.from
	msg := "OPERATOR NP VIOLATION: the TCP 8081 (health probe) rule must be port-scoped only; kubelet-originated probes cannot be reliably constrained by peer selectors."
}

deny contains msg if {
	selects_operator_pods
	some rule in input.spec.ingress
	rule_allows_port(rule, "9443")
	some peer in rule.from
	msg := "OPERATOR NP VIOLATION: the TCP 9443 (webhook) rule must be port-scoped only; the calling API server is host-networked and cannot be expressed as a peer."
}

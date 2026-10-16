package networkpolicy

import rego.v1

# Shape policy for the TrustyAI operator manager-pod NetworkPolicies
# (controller-manager; RHOAIENG-96335).
#
# The operator's own pod is covered by two direction-split policies:
#   - the ingress policy must:
#     - allow TCP 8443 (secure metrics) only from the supported OpenShift
#       monitoring namespaces, via namespaceSelector-only peers;
#     - allow TCP 8081 (kubelet health probes) port-scoped only, because
#       node-originated probes are not reliably constrainable by peer
#       selectors;
#     - allow TCP 9443 (CRD conversion webhooks) port-scoped only, because
#       the calling Kubernetes API server is host-networked with a dynamic
#       address and port;
#   - the egress policy must declare the explicit allow-all egress posture
#     required by ODH-ADR-Operator-0016 ("explicit egress behavior").
#     Restricting operator egress remains tracked separately (HPSTRAT-278).

monitoring_namespaces := {"openshift-monitoring", "openshift-user-workload-monitoring"}

operator_ingress_ports := {"8443", "8081", "9443"}

selects_operator_pods if {
	input.kind == "NetworkPolicy"
	input.spec.podSelector.matchLabels["control-plane"] == "trustyai-service-operator"
}

declares_ingress if "Ingress" in input.spec.policyTypes

declares_egress if "Egress" in input.spec.policyTypes

port_key(port) := sprintf("%v", [port])

rule_allows_port(rule, port) if {
	some p in rule.ports
	port_key(p.port) == port
	is_tcp_port(p)
}

# NetworkPolicyPort.protocol defaults to TCP when omitted.
is_tcp_port(p) if not p.protocol
is_tcp_port(p) if p.protocol == "TCP"

# has_ingress_rule_for is intentionally not guarded by declares_ingress: the
# required-rule denies carry that guard themselves, because `not` succeeds when
# the helper is undefined, which would otherwise fire them on the egress policy.
has_ingress_rule_for(port) if {
	selects_operator_pods
	some rule in input.spec.ingress
	rule_allows_port(rule, port)
}

has_ports(rule) if {
	count(rule.ports) > 0
}

# --- policy types and direction split ---------------------------------------
# Ingress rules live on the ingress policy; the egress posture is declared by
# the dedicated allow-all policy (ODH-ADR-Operator-0016 explicit egress).

deny contains msg if {
	selects_operator_pods
	declares_ingress
	declares_egress
	msg := "OPERATOR NP VIOLATION: the operator manager-pod policies are direction-split; the ingress policy must not declare Egress."
}

deny contains msg if {
	selects_operator_pods
	not declares_ingress
	not declares_egress
	msg := "OPERATOR NP VIOLATION: the operator manager-pod NetworkPolicy must declare the Ingress policy type."
}

# --- the egress policy is a documented allow-all -----------------------------

deny contains msg if {
	selects_operator_pods
	declares_egress
	input.spec.policyTypes != ["Egress"]
	msg := "OPERATOR NP VIOLATION: the egress policy must declare only Egress in policyTypes."
}

deny contains msg if {
	selects_operator_pods
	declares_egress
	not input.spec.egress
	msg := "OPERATOR NP VIOLATION: the egress policy must set egress explicitly; an omitted egress field means deny-all and would break API server and DNS access."
}

deny contains msg if {
	selects_operator_pods
	declares_egress
	count(input.spec.egress) != 1
	msg := "OPERATOR NP VIOLATION: the egress policy must carry exactly one rule, the documented allow-all."
}

deny contains msg if {
	selects_operator_pods
	declares_egress
	input.spec.egress[0] != {}
	msg := "OPERATOR NP VIOLATION: the egress policy's single rule must be the documented allow-all ({}); restricting operator egress is tracked under HPSTRAT-278."
}

deny contains msg if {
	selects_operator_pods
	declares_egress
	input.spec.ingress
	msg := "OPERATOR NP VIOLATION: the egress policy must not carry ingress rules."
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

deny contains msg if {
	selects_operator_pods
	some rule in input.spec.ingress
	some p in rule.ports
	p.protocol
	p.protocol != "TCP"
	msg := sprintf("OPERATOR NP VIOLATION: the operator manager pod serves only TCP listeners (8443 metrics, 8081 probes, 9443 webhooks); found protocol %v on port %v.", [p.protocol, p.port])
}

# --- required rules (ingress policy only) -----------------------------------

deny contains msg if {
	selects_operator_pods
	declares_ingress
	not has_ingress_rule_for("8443")
	msg := "OPERATOR NP VIOLATION: TCP 8443 (secure metrics) ingress from the supported OpenShift monitoring namespaces is required."
}

deny contains msg if {
	selects_operator_pods
	declares_ingress
	not has_ingress_rule_for("8081")
	msg := "OPERATOR NP VIOLATION: TCP 8081 (health/readiness probes) ingress is required."
}

deny contains msg if {
	selects_operator_pods
	declares_ingress
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

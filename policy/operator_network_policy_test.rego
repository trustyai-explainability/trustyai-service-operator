package networkpolicy

import rego.v1

# --- builders --------------------------------------------------------------

monitoring_ns(name) := {"namespaceSelector": {"matchLabels": {"kubernetes.io/metadata.name": name}}}

valid_peers := [monitoring_ns("openshift-monitoring"), monitoring_ns("openshift-user-workload-monitoring")]

np_with_metrics_peers(peers) := {
	"kind": "NetworkPolicy",
	"metadata": {"name": "controller-manager-allow-ports", "namespace": "system"},
	"spec": {
		"podSelector": {"matchLabels": {"control-plane": "trustyai-service-operator"}},
		"policyTypes": ["Ingress"],
		"ingress": [
			{"from": peers, "ports": [{"protocol": "TCP", "port": 8443}]},
			{"ports": [{"protocol": "TCP", "port": 8081}]},
			{"ports": [{"protocol": "TCP", "port": 9443}]},
		],
	},
}

valid_np := np_with_metrics_peers(valid_peers)

# Overlays apply the namePrefix and inject app.kubernetes.io/part-of into
# podSelector matchLabels via kustomize includeSelectors.
valid_overlay_np := object.union(valid_np, {
	"metadata": {"name": "trustyai-service-operator-controller-manager-allow-ports"},
	"spec": object.union(valid_np.spec, {"podSelector": {"matchLabels": {
		"control-plane": "trustyai-service-operator",
		"app.kubernetes.io/part-of": "trustyai",
	}}}),
})

valid_egress_np := {
	"kind": "NetworkPolicy",
	"metadata": {"name": "controller-manager-allow-egress", "namespace": "system"},
	"spec": {
		"podSelector": {"matchLabels": {"control-plane": "trustyai-service-operator"}},
		"policyTypes": ["Egress"],
		"egress": [{}],
	},
}

valid_overlay_egress_np := object.union(valid_egress_np, {
	"metadata": {"name": "trustyai-service-operator-controller-manager-allow-egress"},
	"spec": object.union(valid_egress_np.spec, {"podSelector": {"matchLabels": {
		"control-plane": "trustyai-service-operator",
		"app.kubernetes.io/part-of": "trustyai",
	}}}),
})

without_ingress_rule(np, idx) := object.union(np, {
	"spec": object.union(np.spec, {"ingress": [rule | some i, rule in np.spec.ingress; i != idx]}),
})

rule_with_from(rule, i, idx, peers) := object.union(rule, {"from": peers}) if i == idx
rule_with_from(rule, i, idx, peers) := rule if i != idx

with_rule_from(np, idx, peers) := object.union(np, {
	"spec": object.union(np.spec, {"ingress": [
		rule_with_from(rule, i, idx, peers) | some i, rule in np.spec.ingress
	]}),
})

with_extra_ingress_rule(np, rule) := object.union(np, {
	"spec": object.union(np.spec, {"ingress": array.concat(np.spec.ingress, [rule])}),
})

rule_at(rule, i, idx, new_rule) := new_rule if i == idx
rule_at(rule, i, idx, new_rule) := rule if i != idx

set_ingress_rule(np, idx, new_rule) := object.union(np, {
	"spec": object.union(np.spec, {"ingress": [
		rule_at(rule, i, idx, new_rule) | some i, rule in np.spec.ingress
	]}),
})

# --- valid shapes pass -----------------------------------------------------

test_valid_base_np_passes if {
	count(deny) == 0 with input as valid_np
}

test_valid_overlay_np_passes if {
	count(deny) == 0 with input as valid_overlay_np
}

# --- policy types ----------------------------------------------------------

test_egress_policy_type_denied if {
	np := object.union(valid_np, {"spec": object.union(valid_np.spec, {"policyTypes": ["Ingress", "Egress"]})})
	count(deny) > 0 with input as np
}

test_missing_ingress_policy_type_denied if {
	np := object.union(valid_np, {"spec": object.union(valid_np.spec, {"policyTypes": ["Egress"]})})
	count(deny) > 0 with input as np
}

# --- egress policy -----------------------------------------------------------

test_valid_egress_np_passes if {
	count(deny) == 0 with input as valid_egress_np
}

test_valid_overlay_egress_np_passes if {
	count(deny) == 0 with input as valid_overlay_egress_np
}

test_egress_np_restricted_rule_denied if {
	np := object.union(valid_egress_np, {"spec": object.union(valid_egress_np.spec, {"egress": [
		{"to": [{"namespaceSelector": {"matchLabels": {"kubernetes.io/metadata.name": "openshift-monitoring"}}}]},
	]})})
	count(deny) > 0 with input as np
}

test_egress_np_deny_all_denied if {
	np := object.union(valid_egress_np, {"spec": object.union(valid_egress_np.spec, {"egress": []})})
	count(deny) > 0 with input as np
}

test_egress_np_missing_egress_field_denied if {
	np := {
		"kind": "NetworkPolicy",
		"metadata": {"name": "controller-manager-allow-egress", "namespace": "system"},
		"spec": {
			"podSelector": {"matchLabels": {"control-plane": "trustyai-service-operator"}},
			"policyTypes": ["Egress"],
		},
	}
	count(deny) > 0 with input as np
}

test_egress_np_with_ingress_rules_denied if {
	np := object.union(valid_egress_np, {"spec": object.union(valid_egress_np.spec, {"ingress": [
		{"ports": [{"protocol": "TCP", "port": 8081}]},
	]})})
	count(deny) > 0 with input as np
}

test_egress_np_with_ingress_policy_type_denied if {
	np := object.union(valid_egress_np, {"spec": object.union(valid_egress_np.spec, {"policyTypes": ["Egress", "Ingress"]})})
	count(deny) > 0 with input as np
}

# --- required rules --------------------------------------------------------

test_missing_metrics_rule_denied if {
	count(deny) > 0 with input as without_ingress_rule(valid_np, 0)
}

test_missing_probe_rule_denied if {
	count(deny) > 0 with input as without_ingress_rule(valid_np, 1)
}

test_missing_webhook_rule_denied if {
	count(deny) > 0 with input as without_ingress_rule(valid_np, 2)
}

# --- metrics rule peer scoping ----------------------------------------------

test_metrics_rule_extra_namespace_denied if {
	np := np_with_metrics_peers(array.concat(valid_peers, [monitoring_ns("openshift-cluster-observability-operator")]))
	count(deny) > 0 with input as np
}

test_metrics_rule_missing_namespace_denied if {
	np := np_with_metrics_peers([monitoring_ns("openshift-monitoring")])
	count(deny) > 0 with input as np
}

test_metrics_rule_pod_peer_denied if {
	np := np_with_metrics_peers(array.concat(valid_peers, [{"podSelector": {"matchLabels": {"app": "prometheus"}}}]))
	count(deny) > 0 with input as np
}

test_metrics_rule_ipblock_peer_denied if {
	np := np_with_metrics_peers([{"ipBlock": {"cidr": "10.0.0.0/8"}}])
	count(deny) > 0 with input as np
}

test_metrics_rule_allow_all_peer_denied if {
	np := np_with_metrics_peers([{}])
	count(deny) > 0 with input as np
}

test_metrics_rule_open_namespace_selector_denied if {
	np := np_with_metrics_peers([{"namespaceSelector": {}}])
	count(deny) > 0 with input as np
}

# --- probe and webhook rules stay port-scoped -------------------------------

test_probe_rule_with_peer_denied if {
	np := with_rule_from(valid_np, 1, valid_peers)
	count(deny) > 0 with input as np
}

test_webhook_rule_with_peer_denied if {
	np := with_rule_from(valid_np, 2, valid_peers)
	count(deny) > 0 with input as np
}

# --- rule shape --------------------------------------------------------------

test_portless_rule_denied if {
	np := with_extra_ingress_rule(valid_np, {"from": valid_peers})
	count(deny) > 0 with input as np
}

test_unknown_port_denied if {
	np := with_extra_ingress_rule(valid_np, {"ports": [{"protocol": "TCP", "port": 9999}]})
	count(deny) > 0 with input as np
}

# --- protocol ----------------------------------------------------------------

test_udp_metrics_rule_denied if {
	np := set_ingress_rule(valid_np, 0, {"from": valid_peers, "ports": [{"protocol": "UDP", "port": 8443}]})
	count(deny) > 0 with input as np
}

test_udp_probe_rule_denied if {
	np := set_ingress_rule(valid_np, 1, {"ports": [{"protocol": "UDP", "port": 8081}]})
	count(deny) > 0 with input as np
}

test_extra_udp_port_rule_denied if {
	np := with_extra_ingress_rule(valid_np, {"from": valid_peers, "ports": [{"protocol": "UDP", "port": 8443}]})
	count(deny) > 0 with input as np
}

# protocol omitted defaults to TCP per the Kubernetes API.
test_protocol_omitted_passes if {
	np := object.union(valid_np, {"spec": object.union(valid_np.spec, {"ingress": [
		{"from": valid_peers, "ports": [{"port": 8443}]},
		{"ports": [{"port": 8081}]},
		{"ports": [{"port": 9443}]},
	]})})
	count(deny) == 0 with input as np
}

# --- inputs out of scope are ignored -----------------------------------------

test_foreign_networkpolicy_ignored if {
	count(deny) == 0 with input as {
		"kind": "NetworkPolicy",
		"metadata": {"name": "some-operand-policy"},
		"spec": {
			"podSelector": {"matchLabels": {"app": "trustyai-service"}},
			"policyTypes": ["Ingress", "Egress"],
			"ingress": [{"from": [{}]}],
		},
	}
}

test_non_networkpolicy_input_ignored if {
	count(deny) == 0 with input as {
		"kind": "Deployment",
		"metadata": {"name": "controller-manager"},
		"spec": {"selector": {"matchLabels": {"control-plane": "trustyai-service-operator"}}},
	}
}

package selector

import rego.v1

# --- base deployment (single selector label) ---

test_base_deployment_correct_selector_passes if {
	count(deny) == 0 with input as {
		"kind": "Deployment",
		"metadata": {"name": "controller-manager"},
		"spec": {"selector": {"matchLabels": {
			"control-plane": "trustyai-service-operator",
		}}},
	}
}

test_base_deployment_stale_control_plane_denied if {
	count(deny) > 0 with input as {
		"kind": "Deployment",
		"metadata": {"name": "controller-manager"},
		"spec": {"selector": {"matchLabels": {
			"control-plane": "controller-manager",
		}}},
	}
}

test_base_deployment_extra_label_denied if {
	count(deny) > 0 with input as {
		"kind": "Deployment",
		"metadata": {"name": "controller-manager"},
		"spec": {"selector": {"matchLabels": {
			"control-plane": "trustyai-service-operator",
			"extra": "label",
		}}},
	}
}

# --- overlay deployment (three selector labels, including 3.5 legacy ODH label) ---

test_overlay_deployment_correct_selector_passes if {
	count(deny) == 0 with input as {
		"kind": "Deployment",
		"metadata": {"name": "trustyai-service-operator-controller-manager"},
		"spec": {"selector": {"matchLabels": {
			"control-plane": "trustyai-service-operator",
			"app.kubernetes.io/part-of": "trustyai",
			"app.opendatahub.io/trustyai": "true",
		}}},
	}
}

test_overlay_deployment_stale_control_plane_denied if {
	count(deny) > 0 with input as {
		"kind": "Deployment",
		"metadata": {"name": "trustyai-service-operator-controller-manager"},
		"spec": {"selector": {"matchLabels": {
			"control-plane": "controller-manager",
			"app.kubernetes.io/part-of": "trustyai",
			"app.opendatahub.io/trustyai": "true",
		}}},
	}
}

test_overlay_deployment_missing_part_of_denied if {
	count(deny) > 0 with input as {
		"kind": "Deployment",
		"metadata": {"name": "trustyai-service-operator-controller-manager"},
		"spec": {"selector": {"matchLabels": {
			"control-plane": "trustyai-service-operator",
			"app.opendatahub.io/trustyai": "true",
		}}},
	}
}

test_overlay_deployment_missing_odh_component_denied if {
	count(deny) > 0 with input as {
		"kind": "Deployment",
		"metadata": {"name": "trustyai-service-operator-controller-manager"},
		"spec": {"selector": {"matchLabels": {
			"control-plane": "trustyai-service-operator",
			"app.kubernetes.io/part-of": "trustyai",
		}}},
	}
}

test_overlay_deployment_extra_label_denied if {
	count(deny) > 0 with input as {
		"kind": "Deployment",
		"metadata": {"name": "trustyai-service-operator-controller-manager"},
		"spec": {"selector": {"matchLabels": {
			"control-plane": "trustyai-service-operator",
			"app.kubernetes.io/part-of": "trustyai",
			"app.opendatahub.io/trustyai": "true",
			"extra": "label",
		}}},
	}
}

# --- non-deployment and unrelated deployment ---

test_non_deployment_ignored if {
	count(deny) == 0 with input as {
		"kind": "Service",
		"metadata": {"name": "controller-manager"},
		"spec": {"selector": {"matchLabels": {
			"control-plane": "wrong-value",
		}}},
	}
}

test_unrelated_deployment_ignored if {
	count(deny) == 0 with input as {
		"kind": "Deployment",
		"metadata": {"name": "some-other-deployment"},
		"spec": {"selector": {"matchLabels": {
			"app": "something",
		}}},
	}
}

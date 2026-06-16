package rbac

import rego.v1

# Pinned selector labels for the operator Deployment.
#
# Kubernetes Deployments have an immutable spec.selector field — changing
# it between releases breaks upgrades because the existing Deployment
# cannot be patched.  This policy pins the expected matchLabels for each
# post-kustomize Deployment name so that CI catches accidental selector
# changes before they ship.
#
# Two variants exist because the base kustomization applies no
# namePrefix or includeSelectors, while all overlays apply both.

expected_selectors := {
	# config/base (no namePrefix, no includeSelectors)
	"controller-manager": {"control-plane": "trustyai-service-operator"},

	# all overlays (namePrefix: trustyai-service-operator-, includeSelectors: true)
	"trustyai-service-operator-controller-manager": {
		"control-plane": "trustyai-service-operator",
		"app.kubernetes.io/part-of": "trustyai",
	},
}

deny contains msg if {
	input.kind == "Deployment"
	expected := expected_selectors[input.metadata.name]
	actual := input.spec.selector.matchLabels
	some key, val in expected
	actual[key] != val
	msg := sprintf(
		"SELECTOR VIOLATION: Deployment '%s' selector label '%s' expected '%s' but got '%s'. Changing spec.selector breaks upgrades.",
		[input.metadata.name, key, val, actual[key]],
	)
}

deny contains msg if {
	input.kind == "Deployment"
	expected := expected_selectors[input.metadata.name]
	actual := input.spec.selector.matchLabels
	some key, _ in expected
	not actual[key]
	msg := sprintf(
		"SELECTOR VIOLATION: Deployment '%s' selector missing expected label '%s'. Changing spec.selector breaks upgrades.",
		[input.metadata.name, key],
	)
}

deny contains msg if {
	input.kind == "Deployment"
	expected := expected_selectors[input.metadata.name]
	actual := input.spec.selector.matchLabels
	some key, _ in actual
	not expected[key]
	msg := sprintf(
		"SELECTOR VIOLATION: Deployment '%s' selector has unexpected label '%s'. Changing spec.selector breaks upgrades.",
		[input.metadata.name, key],
	)
}

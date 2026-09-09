package cluster_guardrail_policy

import (
	clusterguardrailpolicyv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/cluster_guardrail_policy/v1alpha1"
)

// resolveEffectiveGuardrails returns the guardrail rules to apply after
// merging spec.guardrails, spec.defaults, and spec.overrides.
//
// Priority:
//   - spec.guardrails is a shorthand for atomic defaults (backward compatible)
//   - spec.defaults.guardrails provides base rules
//   - spec.overrides.guardrails always wins
func resolveEffectiveGuardrails(spec *clusterguardrailpolicyv1alpha1.ClusterGuardrailPolicySpec) *clusterguardrailpolicyv1alpha1.GuardrailRules {
	// Start with the base rules
	var base *clusterguardrailpolicyv1alpha1.GuardrailRules

	if spec.Guardrails != nil {
		base = spec.Guardrails
	} else if spec.Defaults != nil {
		base = spec.Defaults.Guardrails
	}

	if base == nil {
		if spec.Overrides != nil {
			return spec.Overrides.Guardrails
		}
		return nil
	}

	if spec.Overrides == nil || spec.Overrides.Guardrails == nil {
		return base
	}

	overrides := spec.Overrides.Guardrails
	strategy := spec.Overrides.Strategy
	if strategy == "" {
		strategy = clusterguardrailpolicyv1alpha1.MergeStrategyAtomic
	}

	if strategy == clusterguardrailpolicyv1alpha1.MergeStrategyAtomic {
		return overrides
	}

	// Merge strategy: override only non-nil fields
	return mergeGuardrailRules(base, overrides)
}

// mergeGuardrailRules merges override fields on top of base.
// Only non-nil override fields replace the corresponding base fields.
func mergeGuardrailRules(base, override *clusterguardrailpolicyv1alpha1.GuardrailRules) *clusterguardrailpolicyv1alpha1.GuardrailRules {
	result := base.DeepCopy()

	if override.NemoGuardrails != nil {
		result.NemoGuardrails = override.NemoGuardrails
	}
	if override.InputGuard != nil {
		result.InputGuard = override.InputGuard
	}
	if override.OutputGuard != nil {
		result.OutputGuard = override.OutputGuard
	}

	return result
}

package evalhub

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
)

const (
	generationIterations     = "max_iterations"
	generationModelCalls     = "max_model_calls"
	generationTotalTokens    = "max_total_tokens"
	generationOutputTokens   = "max_output_tokens"
	generationRetentionEnv   = "EVALHUB_COLLECTION_GENERATION_RETENTION"
	generationJanitorEnv     = "EVALHUB_RESOURCE_JANITOR_INTERVAL"
	generationRunnerImageEnv = "EVALHUB_COLLECTION_GENERATION_RUNNER_IMAGE"
)

var generationEfforts = [...]string{"low", "standard", "high"}

type generationLimit struct {
	field    string
	duration bool
}

var generationLimits = [...]generationLimit{
	{field: generationIterations},
	{field: generationModelCalls},
	{field: "max_tool_calls"},
	{field: generationTotalTokens},
	{field: generationOutputTokens},
	{field: "max_duration", duration: true},
}

func generationEnvName(effort, field string) string {
	return "EVALHUB_GENERATION_" + strings.ToUpper(effort+"_"+field)
}

// validateGenerationEnv preflights relationships between explicit literals.
// The deployed EvalHub image validates its effective defaults and hard limits
// after Kubernetes resolves any dynamic values. Preflight errors report only
// variable names and constraints.
func validateGenerationEnv(overrides []corev1.EnvVar) error {
	env := make(map[string]corev1.EnvVar, len(overrides))
	for _, variable := range overrides {
		// mergeEnvVars uses the last CR override for a duplicate name.
		env[variable.Name] = variable
	}
	if _, present := env[generationRunnerImageEnv]; present {
		return fmt.Errorf("spec.env: %s is managed by the operator and must be removed to use the resolved EvalHub image", generationRunnerImageEnv)
	}

	var budgets [len(generationEfforts)]map[string]int64
	for i, effort := range generationEfforts {
		budget := make(map[string]int64, len(generationLimits))
		for _, limit := range generationLimits {
			name := generationEnvName(effort, limit.field)
			variable := env[name]
			if variable.Value == "" || generationValueIsDynamic(variable) {
				continue
			}
			value, err := limit.parse(variable.Value)
			if err != nil {
				return fmt.Errorf("spec.env: %s %w", name, err)
			}
			budget[limit.field] = value
		}
		budgets[i] = budget

		if err := validateGenerationPair(effort, budget, generationModelCalls, generationIterations); err != nil {
			return err
		}
		if err := validateGenerationPair(effort, budget, generationTotalTokens, generationOutputTokens); err != nil {
			return err
		}
	}

	for _, limit := range generationLimits {
		var previous int64
		var previousEffort string
		for i, effort := range generationEfforts {
			value, known := budgets[i][limit.field]
			if !known {
				continue
			}
			if value < previous {
				return fmt.Errorf("spec.env: %s must be at least %s",
					generationEnvName(effort, limit.field), generationEnvName(previousEffort, limit.field))
			}
			previous, previousEffort = value, effort
		}
	}

	retention := env[generationRetentionEnv]
	if retention.Value != "" && !generationValueIsDynamic(retention) {
		duration, err := time.ParseDuration(retention.Value)
		if err != nil || duration < 0 {
			return fmt.Errorf("spec.env: %s must be a non-negative duration", generationRetentionEnv)
		}
	}
	interval := env[generationJanitorEnv]
	if interval.Value == "" || generationValueIsDynamic(interval) {
		return nil
	}
	duration, err := time.ParseDuration(interval.Value)
	if err != nil || (duration != 0 && (duration < time.Minute || duration > 24*time.Hour || duration%time.Minute != 0)) {
		return fmt.Errorf("spec.env: %s must be zero or a whole number of minutes between 1m and 24h", generationJanitorEnv)
	}
	return nil
}

func validateGenerationPair(effort string, budget map[string]int64, higher, lower string) error {
	high, highKnown := budget[higher]
	low, lowKnown := budget[lower]
	if highKnown && lowKnown && high < low {
		return fmt.Errorf("spec.env: %s must be at least %s", generationEnvName(effort, higher), generationEnvName(effort, lower))
	}
	return nil
}

func (limit generationLimit) parse(raw string) (int64, error) {
	if limit.duration {
		duration, err := time.ParseDuration(raw)
		if err != nil || duration <= 0 {
			return 0, fmt.Errorf("must be a positive duration")
		}
		return int64(duration), nil
	}
	// EvalHub's mapstructure decoder uses base 0 for integer environment values.
	value, err := strconv.ParseInt(raw, 0, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("must be a positive integer")
	}
	return value, nil
}

func generationValueIsDynamic(variable corev1.EnvVar) bool {
	if variable.ValueFrom != nil {
		return true
	}
	// Kubernetes treats a double dollar as a literal dollar.
	_, rest, found := strings.Cut(strings.ReplaceAll(variable.Value, "$$", ""), "$(")
	return found && strings.Contains(rest, ")")
}

package evalhub

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
)

const (
	generationIterations   = "max_iterations"
	generationModelCalls   = "max_model_calls"
	generationTotalTokens  = "max_total_tokens"
	generationOutputTokens = "max_output_tokens"
	generationJanitorEnv   = "EVALHUB_RESOURCE_JANITOR_INTERVAL"
)

var generationEfforts = [...]string{"low", "standard", "high"}

type generationLimit struct {
	field    string
	defaults [len(generationEfforts)]int64
	ceiling  int64
	duration bool
}

// Keep these values aligned with the supported EvalHub operand's
// internal/agent/loop DefaultPresets and DefaultHardLimits. EvalHub still
// validates the effective values when its process starts.
var generationLimits = [...]generationLimit{
	{field: generationIterations, defaults: [3]int64{8, 20, 40}, ceiling: 100},
	{field: generationModelCalls, defaults: [3]int64{8, 20, 40}, ceiling: 100},
	{field: "max_tool_calls", defaults: [3]int64{24, 60, 120}, ceiling: 500},
	{field: generationTotalTokens, defaults: [3]int64{32768, 131072, 524288}, ceiling: 1_000_000},
	{field: generationOutputTokens, defaults: [3]int64{2048, 4096, 8192}, ceiling: 16_384},
	{field: "max_duration", defaults: [3]int64{int64(2 * time.Minute), int64(2 * time.Minute), int64(10 * time.Minute)}, ceiling: int64(30 * time.Minute), duration: true},
}

func generationEnvName(effort, field string) string {
	return "EVALHUB_GENERATION_" + strings.ToUpper(effort+"_"+field)
}

// validateGenerationEnv preflights relationships whose operands are known.
// It leaves references and Kubernetes variable expansion to the kubelet and
// EvalHub. It never dereferences or includes environment values in errors.
func validateGenerationEnv(overrides []corev1.EnvVar) error {
	env := make(map[string]corev1.EnvVar, len(overrides))
	for _, variable := range overrides {
		// mergeEnvVars uses the last CR override for a duplicate name.
		env[variable.Name] = variable
	}

	var budgets [len(generationEfforts)]map[string]int64
	for i, effort := range generationEfforts {
		budget := make(map[string]int64, len(generationLimits))
		for _, limit := range generationLimits {
			name := generationEnvName(effort, limit.field)
			variable := env[name]
			if generationValueIsDynamic(variable) {
				continue
			}
			value := limit.defaults[i]
			if variable.Value != "" {
				var err error
				value, err = limit.parse(variable.Value)
				if err != nil {
					return fmt.Errorf("spec.env: %s %w", name, err)
				}
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
		if err != nil || duration <= 0 || int64(duration) > limit.ceiling {
			return 0, fmt.Errorf("must be a positive duration no greater than %s", time.Duration(limit.ceiling))
		}
		return int64(duration), nil
	}
	// EvalHub's mapstructure decoder uses base 0 for integer environment values.
	value, err := strconv.ParseInt(raw, 0, 64)
	if err != nil || value <= 0 || value > limit.ceiling {
		return 0, fmt.Errorf("must be an integer between 1 and %d", limit.ceiling)
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

package evalhub

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestValidateGenerationEnv(t *testing.T) {
	t.Parallel()
	const (
		iterations   = "EVALHUB_GENERATION_STANDARD_MAX_ITERATIONS"
		calls        = "EVALHUB_GENERATION_STANDARD_MAX_MODEL_CALLS"
		total        = "EVALHUB_GENERATION_STANDARD_MAX_TOTAL_TOKENS"
		output       = "EVALHUB_GENERATION_STANDARD_MAX_OUTPUT_TOKENS"
		lowDuration  = "EVALHUB_GENERATION_LOW_MAX_DURATION"
		duration     = "EVALHUB_GENERATION_STANDARD_MAX_DURATION"
		highDuration = "EVALHUB_GENERATION_HIGH_MAX_DURATION"
		janitor      = "EVALHUB_RESOURCE_JANITOR_INTERVAL"
	)
	literal := func(name, value string) corev1.EnvVar { return corev1.EnvVar{Name: name, Value: value} }
	reference := func(name string) corev1.EnvVar {
		return corev1.EnvVar{Name: name, ValueFrom: &corev1.EnvVarSource{
			ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "generation-config"}, Key: "value",
			},
		}}
	}
	tests := []struct {
		name string
		env  []corev1.EnvVar
		want []string
	}{
		{name: "defaults"},
		{name: "iterations above calls", env: []corev1.EnvVar{literal(iterations, "25")}, want: []string{calls, iterations}},
		{name: "valid paired limits", env: []corev1.EnvVar{literal(iterations, "25"), literal(calls, "25"), literal(janitor, "1m")}},
		{name: "output above total", env: []corev1.EnvVar{literal(total, "4095")}, want: []string{total, output}},
		{name: "duration falls with effort", env: []corev1.EnvVar{literal(lowDuration, "3m")}, want: []string{duration, lowDuration}},
		{name: "invalid count", env: []corev1.EnvVar{literal(iterations, "0")}, want: []string{iterations, "integer"}},
		{name: "hard ceiling", env: []corev1.EnvVar{literal(output, "16385")}, want: []string{output, "16384"}},
		{name: "duration ceiling", env: []corev1.EnvVar{literal(highDuration, "31m")}, want: []string{highDuration, "30m"}},
		{name: "zero cleanup uses default", env: []corev1.EnvVar{literal(janitor, "0")}},
		{name: "maximum cleanup", env: []corev1.EnvVar{literal(janitor, "24h")}},
		{name: "excessive cleanup", env: []corev1.EnvVar{literal(janitor, "25h")}, want: []string{janitor}},
		{name: "negative cleanup", env: []corev1.EnvVar{literal(janitor, "-1m")}, want: []string{janitor}},
		{name: "fractional cleanup", env: []corev1.EnvVar{literal(janitor, "90s")}, want: []string{janitor, "whole number of minutes"}},
		{name: "bad cleanup text", env: []corev1.EnvVar{literal(janitor, "unparseable")}, want: []string{janitor}},
		{name: "empty values and last duplicate win", env: []corev1.EnvVar{literal(iterations, "25"), literal(iterations, ""), literal(janitor, "")}},
		{name: "last duplicate is invalid", env: []corev1.EnvVar{literal(iterations, "20"), literal(iterations, "25")}, want: []string{iterations, calls}},
		{name: "related dynamic value is unknown", env: []corev1.EnvVar{literal(iterations, "25"), reference(calls)}},
		{name: "last duplicate is dynamic", env: []corev1.EnvVar{literal(iterations, "25"), literal(calls, "19"), reference(calls)}},
		{name: "expanded override is unknown", env: []corev1.EnvVar{literal(iterations, "25"), literal(calls, "$(CALL_LIMIT)"), literal(janitor, "$(CLEANUP_MINUTES)m")}},
		{name: "escaped placeholder is literal", env: []corev1.EnvVar{literal(calls, "$$(CALL_LIMIT)")}, want: []string{calls}},
		{name: "dynamic cleanup reference is unknown", env: []corev1.EnvVar{reference(janitor)}},
		{name: "unknown unrelated value cannot hide invalid pair", env: []corev1.EnvVar{literal(iterations, "25"), reference(janitor)}, want: []string{iterations, calls}},
		{name: "known bounds can contradict across dynamic standard", env: []corev1.EnvVar{literal(lowDuration, "11m"), reference(duration)}, want: []string{highDuration, lowDuration}},
		{name: "unrelated env is untouched", env: []corev1.EnvVar{literal("SOME_OTHER_SETTING", "unparseable")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGenerationEnv(tt.env)
			if len(tt.want) == 0 {
				require.NoError(t, err)
				return
			}
			for _, detail := range tt.want {
				require.ErrorContains(t, err, detail)
			}
		})
	}
}

func TestValidateGenerationEnvDoesNotEchoRawValues(t *testing.T) {
	t.Parallel()
	const rawValue = "private-value"
	err := validateGenerationEnv([]corev1.EnvVar{{Name: "EVALHUB_GENERATION_LOW_MAX_MODEL_CALLS", Value: rawValue}})
	require.Error(t, err)
	require.NotContains(t, err.Error(), rawValue)
}

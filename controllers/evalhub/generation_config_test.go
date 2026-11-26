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
		retention    = "EVALHUB_COLLECTION_GENERATION_RETENTION"
		runner       = "EVALHUB_COLLECTION_GENERATION_RUNNER_IMAGE"
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
		{name: "iterations above calls", env: []corev1.EnvVar{literal(iterations, "25"), literal(calls, "24")}, want: []string{calls, iterations}},
		{name: "valid paired limits", env: []corev1.EnvVar{literal(iterations, "25"), literal(calls, "25"), literal(janitor, "1m")}},
		{name: "output above total", env: []corev1.EnvVar{literal(total, "4095"), literal(output, "4096")}, want: []string{total, output}},
		{name: "duration falls with effort", env: []corev1.EnvVar{literal(lowDuration, "3m"), literal(duration, "2m")}, want: []string{duration, lowDuration}},
		{name: "invalid count", env: []corev1.EnvVar{literal(iterations, "0")}, want: []string{iterations, "integer"}},
		{name: "invalid effort duration", env: []corev1.EnvVar{literal(duration, "0s")}, want: []string{duration, "positive duration"}},
		{name: "omitted operands use the deployed image defaults", env: []corev1.EnvVar{literal(iterations, "25"), literal(lowDuration, "3m")}},
		{name: "effort ceilings are validated by the deployed image", env: []corev1.EnvVar{literal(output, "16385"), literal(highDuration, "31m")}},
		{name: "positive retention", env: []corev1.EnvVar{literal(retention, "30s")}},
		{name: "empty retention uses default", env: []corev1.EnvVar{literal(retention, "")}},
		{name: "zero retention uses default", env: []corev1.EnvVar{literal(retention, "0")}},
		{name: "malformed retention", env: []corev1.EnvVar{literal(retention, "1d")}, want: []string{retention, "non-negative duration"}},
		{name: "negative retention", env: []corev1.EnvVar{literal(retention, "-1s")}, want: []string{retention, "non-negative duration"}},
		{name: "dynamic retention", env: []corev1.EnvVar{reference(retention)}},
		{name: "expanded retention", env: []corev1.EnvVar{literal(retention, "$(RETENTION_HOURS)h")}},
		{name: "runner image override", env: []corev1.EnvVar{literal(runner, "example.test/runner:latest")}, want: []string{runner, "managed by the operator"}},
		{name: "runner image reference", env: []corev1.EnvVar{reference(runner)}, want: []string{runner, "managed by the operator"}},
		{name: "empty runner image override", env: []corev1.EnvVar{literal(runner, "")}, want: []string{runner}},
		{name: "zero cleanup uses default", env: []corev1.EnvVar{literal(janitor, "0")}},
		{name: "maximum cleanup", env: []corev1.EnvVar{literal(janitor, "24h")}},
		{name: "excessive cleanup", env: []corev1.EnvVar{literal(janitor, "25h")}, want: []string{janitor}},
		{name: "negative cleanup", env: []corev1.EnvVar{literal(janitor, "-1m")}, want: []string{janitor}},
		{name: "fractional cleanup", env: []corev1.EnvVar{literal(janitor, "90s")}, want: []string{janitor, "whole number of minutes"}},
		{name: "bad cleanup text", env: []corev1.EnvVar{literal(janitor, "unparseable")}, want: []string{janitor}},
		{name: "empty values and last duplicate win", env: []corev1.EnvVar{literal(iterations, "25"), literal(iterations, ""), literal(janitor, "")}},
		{name: "last duplicate is invalid", env: []corev1.EnvVar{literal(iterations, "20"), literal(iterations, "25"), literal(calls, "20")}, want: []string{iterations, calls}},
		{name: "related dynamic value is unknown", env: []corev1.EnvVar{literal(iterations, "25"), reference(calls)}},
		{name: "last duplicate is dynamic", env: []corev1.EnvVar{literal(iterations, "25"), literal(calls, "19"), reference(calls)}},
		{name: "expanded override is unknown", env: []corev1.EnvVar{literal(iterations, "25"), literal(calls, "$(CALL_LIMIT)"), literal(janitor, "$(CLEANUP_MINUTES)m")}},
		{name: "escaped placeholder is literal", env: []corev1.EnvVar{literal(calls, "$$(CALL_LIMIT)")}, want: []string{calls}},
		{name: "dynamic cleanup reference is unknown", env: []corev1.EnvVar{reference(janitor)}},
		{name: "unknown unrelated value cannot hide invalid pair", env: []corev1.EnvVar{literal(iterations, "25"), literal(calls, "20"), reference(janitor)}, want: []string{iterations, calls}},
		{name: "known bounds can contradict across dynamic standard", env: []corev1.EnvVar{literal(lowDuration, "11m"), reference(duration), literal(highDuration, "10m")}, want: []string{highDuration, lowDuration}},
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

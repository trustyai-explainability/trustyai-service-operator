package lmes

import "regexp"

// LMEval validation patterns
var (
	// PatternModelName allows for alphanumeric, hyphens, underscores, forward slashes, dots, and colons in model names
	PatternModelName = regexp.MustCompile(`^[a-zA-Z0-9._/-]+$`)

	// PatternTemplateName allows for alphanumeric, hyphens, underscores, dots, and colons in template names
	PatternTemplateName = regexp.MustCompile(`^[a-zA-Z0-9._:-]+$`)

	// PatternArgName allows for alphanumeric, hyphens, underscores, and dots in argument names
	PatternArgName = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

	// PatternArgValue allows for reasonable characters (including spaces) in arg values
	PatternArgValue = regexp.MustCompile(`^[a-zA-Z0-9._/:\- ]+$`)

	// PatternLimit allows for integers, floats, and percentage in limit values
	PatternLimit = regexp.MustCompile(`^(\d+(\.\d+)?|\d*\.\d+)$`)

	// PatternTaskName allows for alphanumeric, hyphens, underscores, parentheses in task names
	PatternTaskName = regexp.MustCompile(`^[a-zA-Z0-9._()-]+$`)

	// AllowedModels allows for a subset of model types
	AllowedModels = map[string]struct{}{
		"hf":                      {},
		"openai-completions":      {},
		"openai-chat-completions": {},
		"local-completions":       {},
		"local-chat-completions":  {},
		"watsonx_llm":             {},
		"textsynth":               {},
	}
)

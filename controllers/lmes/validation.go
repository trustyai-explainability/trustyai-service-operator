package lmes

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	lmesv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/lmes/v1alpha1"
)

// ValidateJSON checks that the input is a valid standalone JSON
func ValidateJSON(input string) error {
	decoder := json.NewDecoder(strings.NewReader(input))

	var data interface{}
	if err := decoder.Decode(&data); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	if decoder.More() {
		return fmt.Errorf("extra data found after valid JSON")
	}

	return nil
}

// ValidateJSONContent validates that JSON content is safe for execution
func ValidateJSONContent(input string) error {
	// First validate that it's proper JSON
	if err := ValidateJSON(input); err != nil {
		return err
	}

	commandInjectionPatterns := []string{
		"$(", "`", "&&", "||",
	}

	for _, pattern := range commandInjectionPatterns {
		if strings.Contains(input, pattern) {
			return fmt.Errorf("JSON content contains dangerous pattern: %s", pattern)
		}
	}

	return nil
}

// ValidateCustomArtifactValue validates CustomArtifact.Value which can be either JSON or plain text
func ValidateCustomArtifactValue(input string) error {
	// Try to parse as JSON first
	if err := ValidateJSON(input); err == nil {
		// It's valid JSON, apply JSON-specific validation
		return ValidateJSONContent(input)
	}

	// It's not JSON, treat as plain text and validate for shell metacharacters
	if ContainsShellMetacharacters(input) {
		return fmt.Errorf("custom artifact value contains shell metacharacters")
	}

	// Additional validation for plain text content that could be invalid
	dangerousPatterns := []string{
		"$(", "`", "&&", "||", ";",
	}

	for _, pattern := range dangerousPatterns {
		if strings.Contains(input, pattern) {
			return fmt.Errorf("custom artifact value contains dangerous pattern: %s", pattern)
		}
	}

	return nil
}

// ValidateUserInput validates all user fields
func ValidateUserInput(job *lmesv1alpha1.LMEvalJob) error {
	if job == nil {
		return fmt.Errorf("job cannot be nil")
	}

	// Validate model name
	if err := ValidateModelName(job.Spec.Model); err != nil {
		return fmt.Errorf("invalid model: %w", err)
	}

	// Validate model arguments
	if err := ValidateArgs(job.Spec.ModelArgs, "model argument"); err != nil {
		return fmt.Errorf("invalid model arguments: %w", err)
	}

	// Validate task names
	if err := ValidateTaskNames(job.Spec.TaskList.TaskNames); err != nil {
		return fmt.Errorf("invalid task names: %w", err)
	}

	// Validate generation arguments (genArgs)
	if err := ValidateArgs(job.Spec.GenArgs, "generation argument"); err != nil {
		return fmt.Errorf("invalid generation arguments: %w", err)
	}

	// Validate limit field
	if job.Spec.Limit != "" {
		if err := ValidateLimit(job.Spec.Limit); err != nil {
			return fmt.Errorf("invalid limit: %w", err)
		}
	}

	// Validate S3 path
	if job.Spec.HasOfflineS3() {
		if err := ValidateS3Path(job.Spec.Offline.StorageSpec.S3Spec.Path); err != nil {
			return fmt.Errorf("invalid S3 path: %w", err)
		}
	}

	// Validate batch size
	if job.Spec.BatchSize != nil {
		if err := ValidateBatchSizeInput(*job.Spec.BatchSize); err != nil {
			return fmt.Errorf("invalid batch size: %w", err)
		}
	}

	// Validate custom task git source
	if job.Spec.TaskList.CustomTasks != nil && len(job.Spec.TaskList.TaskNames) > 0 {
		gitSource := job.Spec.TaskList.CustomTasks.Source.GitSource
		if err := ValidateGitURL(gitSource.URL); err != nil {
			return fmt.Errorf("invalid git URL: %w", err)
		}
		if gitSource.Path != "" {
			if err := ValidateGitPath(gitSource.Path); err != nil {
				return fmt.Errorf("invalid git path: %w", err)
			}
		}
		if gitSource.Branch != nil {
			if err := ValidateGitBranch(*gitSource.Branch); err != nil {
				return fmt.Errorf("invalid git branch: %w", err)
			}
		}
		if gitSource.Commit != nil {
			if err := ValidateGitCommit(*gitSource.Commit); err != nil {
				return fmt.Errorf("invalid git commit: %w", err)
			}
		}
	}

	return nil
}

// ValidateModelName validates model name
func ValidateModelName(model string) error {
	if model == "" {
		return fmt.Errorf("model name cannot be empty")
	}

	if _, ok := AllowedModels[model]; !ok {
		return fmt.Errorf("model name must be one of: hf, local-completions, local-chat-completions, watsonx_llm")
	}

	return nil
}

// ValidateArgs validates argument arrays
func ValidateArgs(args []lmesv1alpha1.Arg, argType string) error {
	for i, arg := range args {
		if err := ValidateArgName(arg.Name); err != nil {
			return fmt.Errorf("%s[%d] name: %w", argType, i, err)
		}
		if err := ValidateArgValue(arg.Value); err != nil {
			return fmt.Errorf("%s[%d] value: %w", argType, i, err)
		}
	}
	return nil
}

// ValidateTaskRecipes validates task recipe arrays
func ValidateTaskRecipes(args []lmesv1alpha1.TaskRecipe, argType string) error {
	for i, arg := range args {

		// Validate cards
		if arg.Card.Name != "" {
			if err := ValidateArgName(arg.Card.Name); err != nil {
				return fmt.Errorf("%s[%d] card name: %w", argType, i, err)
			}
		}
		if arg.Card.Custom != "" {
			if err := ValidateJSONContent(arg.Card.Custom); err != nil {
				return fmt.Errorf("%s[%d] custom: %w", argType, i, err)
			}
		}

		if arg.Template != nil {
			if arg.Template.Name != "" {
				if err := ValidateTaskName(arg.Template.Name); err != nil {
					return fmt.Errorf("%s[%d] template name: %w", argType, i, err)
				}
			}
			if arg.Template.Ref != "" {
				if err := ValidateTaskName(arg.Template.Ref); err != nil {
					return fmt.Errorf("%s[%d] template ref: %w", argType, i, err)
				}
			}
		}

		if arg.SystemPrompt != nil {
			if arg.SystemPrompt.Name != "" {
				if err := ValidateTaskName(arg.SystemPrompt.Name); err != nil {
					return fmt.Errorf("%s[%d] system prompt name: %w", argType, i, err)
				}
			}
			if arg.SystemPrompt.Ref != "" {
				if err := ValidateTaskName(arg.SystemPrompt.Ref); err != nil {
					return fmt.Errorf("%s[%d] system prompt ref: %w", argType, i, err)
				}
			}
		}
		// Same pattern as task name
		if arg.Task != nil {
			if arg.Task.Name != "" {
				if err := ValidateTaskName(arg.Task.Name); err != nil {
					return fmt.Errorf("%s[%d] task name: %w", argType, i, err)
				}
			}
			if arg.Task.Ref != "" {
				if err := ValidateTaskName(arg.Task.Ref); err != nil {
					return fmt.Errorf("%s[%d] task ref: %w", argType, i, err)
				}
			}
		}

		// Validate individual metrics
		// The CLI format metrics=[metric1,metric2] is generated by TaskRecipe.String()
		for j, metric := range arg.Metrics {
			if metric.Name != "" {
				if err := ValidateTaskName(metric.Name); err != nil {
					return fmt.Errorf("%s[%d] metric[%d] name: %w", argType, i, j, err)
				}
			}
			if metric.Ref != "" {
				if err := ValidateTaskName(metric.Ref); err != nil {
					return fmt.Errorf("%s[%d] metric[%d] ref: %w", argType, i, j, err)
				}
			}
		}

		if arg.Format != nil {
			if err := ValidateTaskName(*arg.Format); err != nil {
				return fmt.Errorf("%s[%d] format: %w", argType, i, err)
			}
		}

	}
	return nil
}

// ValidateArgName validates argument names
func ValidateArgName(name string) error {
	if name == "" {
		return fmt.Errorf("argument name cannot be empty")
	}

	if !PatternArgName.MatchString(name) {
		return fmt.Errorf("argument name contains invalid characters (only alphanumeric, ., _, - allowed)")
	}

	return nil
}

// ValidateArgValue validates argument values
func ValidateArgValue(value string) error {
	if !PatternArgValue.MatchString(value) {
		return fmt.Errorf("argument value contains invalid characters (only alphanumeric, ., _, /, :, -, and spaces allowed)")
	}

	// Additional check for shell metacharacters
	if ContainsShellMetacharacters(value) {
		return fmt.Errorf("argument value contains shell metacharacters")
	}

	return nil
}

// ValidateLimit validates limit field format
func ValidateLimit(limit string) error {
	if !PatternLimit.MatchString(limit) {
		return fmt.Errorf("limit must be a valid number")
	}

	// additional check
	if ContainsShellMetacharacters(limit) {
		return fmt.Errorf("limit contains shell metacharacters")
	}

	return nil
}

// ValidateSystemInstruction validates system instruction content
func ValidateSystemInstruction(instruction string) error {
	// Check for shell escape sequences
	if strings.Contains(instruction, "\"") && strings.Contains(instruction, ";") {
		return fmt.Errorf("system instruction contains potentially dangerous character combinations")
	}

	// Check for uncommon patterns
	uncommonPatterns := []string{
		"$(", "`", "|", "&", "&&", "||", ";", "\n", "\r",
	}

	for _, pattern := range uncommonPatterns {
		if strings.Contains(instruction, pattern) {
			return fmt.Errorf("system instruction contains pattern not allowed: %s", pattern)
		}
	}

	return nil
}

// ValidateTemplateName validates chat template names
func ValidateTemplateName(name string) error {
	if name == "" {
		return fmt.Errorf("template name cannot be empty")
	}

	if !PatternTemplateName.MatchString(name) {
		return fmt.Errorf("template name contains invalid characters (only alphanumeric, ., _, :, - allowed)")
	}

	return nil
}

// ValidateTaskNames validates task name arrays
func ValidateTaskNames(taskNames []string) error {
	for i, taskName := range taskNames {
		if err := ValidateTaskName(taskName); err != nil {
			return fmt.Errorf("task name[%d]: %w", i, err)
		}
	}
	return nil
}

// ValidateTaskName validates individual task names
func ValidateTaskName(name string) error {
	if name == "" {
		return fmt.Errorf("task name cannot be empty")
	}

	if !PatternTaskName.MatchString(name) {
		return fmt.Errorf("task name contains invalid characters (only alphanumeric, ., _, -, () allowed)")
	}

	return nil
}

// ContainsShellMetacharacters checks for dangerous shell characters
// Allows JSON-specific characters like {}, [] but blocks command execution patterns
func ContainsShellMetacharacters(input string) bool {
	// We exclude {, }, [, ] as they're needed for JSON content
	shellChars := []string{
		";", "&", "|", "$", "`", "(", ")",
		"<", ">", "\"", "'", "\\", "\n", "\r", "\t",
	}

	for _, char := range shellChars {
		if strings.Contains(input, char) {
			return true
		}
	}

	return false
}

// ValidateS3Path validates S3 paths
func ValidateS3Path(path string) error {
	if path == "" {
		return nil // Empty path is valid for S3 root
	}

	// Check for shell metacharacters
	if ContainsShellMetacharacters(path) {
		return fmt.Errorf("S3 path contains invalid characters")
	}

	// S3 paths should not contain invalid patterns
	dangerousPatterns := []string{"../", "..\\", "./", ".\\"}
	for _, pattern := range dangerousPatterns {
		if strings.Contains(path, pattern) {
			return fmt.Errorf("S3 path contains invalid pattern: %s", pattern)
		}
	}

	// S3 paths allowed characters
	if !regexp.MustCompile(`^[a-zA-Z0-9._/-]*$`).MatchString(path) {
		return fmt.Errorf("S3 path contains invalid characters (only alphanumeric, ., _, /, - allowed)")
	}

	return nil
}

// ValidatePath validates file system paths
func ValidatePath(path, pathType string) error {
	if path == "" {
		return fmt.Errorf("%s cannot be empty", pathType)
	}

	// Check for shell metacharacters
	if ContainsShellMetacharacters(path) {
		return fmt.Errorf("%s contains shell metacharacters", pathType)
	}

	// Check for path traversal attempts
	if strings.Contains(path, "../") || strings.Contains(path, "..\\") {
		return fmt.Errorf("%s contains path traversal sequence", pathType)
	}

	// Paths should be reasonable (alphanumeric, slashes, hyphens, underscores, dots)
	if !regexp.MustCompile(`^[a-zA-Z0-9._/-]*$`).MatchString(path) {
		return fmt.Errorf("%s contains invalid characters (only alphanumeric, ., _, /, - allowed)", pathType)
	}

	return nil
}

// ValidateBatchSizeInput validates batch size input format
func ValidateBatchSizeInput(batchSize string) error {
	if batchSize == "" {
		return fmt.Errorf("batch size cannot be empty")
	}

	// Check for shell metacharacters
	if ContainsShellMetacharacters(batchSize) {
		return fmt.Errorf("batch size contains shell metacharacters")
	}

	// Allow "auto", "auto:N", or plain numbers
	validPatterns := []string{
		`^auto$`,
		`^auto:\d+$`,
		`^\d+$`,
	}

	for _, pattern := range validPatterns {
		if regexp.MustCompile(pattern).MatchString(batchSize) {
			return nil
		}
	}

	return fmt.Errorf("batch size must be 'auto', 'auto:N', or a positive integer")
}

// ValidateChatTemplateName validates chat template names
func ValidateChatTemplateName(name string) error {
	if name == "" {
		return fmt.Errorf("chat template name cannot be empty")
	}

	if !PatternTaskName.MatchString(name) {
		return fmt.Errorf("chat template name contains invalid characters (only alphanumeric, ., _, - allowed)")
	}

	return nil
}

// ValidateGitURL validates git repository URLs
func ValidateGitURL(url string) error {
	if url == "" {
		return fmt.Errorf("git URL cannot be empty")
	}

	// Check for shell metacharacters
	if ContainsShellMetacharacters(url) {
		return fmt.Errorf("git URL contains shell metacharacters")
	}

	// Must be HTTPS URL for security
	if !regexp.MustCompile(`^https://[a-zA-Z0-9._/-]+$`).MatchString(url) {
		return fmt.Errorf("git URL must be a valid HTTPS URL (only alphanumeric, ., _, /, - allowed)")
	}

	return nil
}

// ValidateGitPath validates git repository paths
func ValidateGitPath(path string) error {
	if path == "" {
		return nil // Empty path is valid
	}

	// Check for shell metacharacters
	if ContainsShellMetacharacters(path) {
		return fmt.Errorf("git path contains shell metacharacters")
	}

	// Check for path traversal attempts
	if strings.Contains(path, "../") || strings.Contains(path, "..\\") {
		return fmt.Errorf("git path contains path traversal sequence")
	}

	// Git paths allowed characters
	if !regexp.MustCompile(`^[a-zA-Z0-9._/-]*$`).MatchString(path) {
		return fmt.Errorf("git path contains invalid characters (only alphanumeric, ., _, /, - allowed)")
	}

	return nil
}

// ValidateGitBranch validates git branch names
func ValidateGitBranch(branch string) error {
	if branch == "" {
		return nil // Empty branch is valid (uses default)
	}

	// Git branch names should only contain safe characters
	// Allow alphanumeric, hyphens, underscores, dots, and forward slashes
	if !regexp.MustCompile(`^[a-zA-Z0-9._/-]+$`).MatchString(branch) {
		return fmt.Errorf("git branch contains invalid characters (only alphanumeric, ., _, /, - allowed)")
	}

	return nil
}

// ValidateGitCommit validates git commit hashes
func ValidateGitCommit(commit string) error {
	if commit == "" {
		return nil // Empty commit is valid (uses default)
	}

	// Git commit hashes should be hexadecimal (full SHA-1: 40 chars, short: 7+ chars)
	if !regexp.MustCompile(`^[a-fA-F0-9]{7,40}$`).MatchString(commit) {
		return fmt.Errorf("git commit must be a valid hexadecimal hash (7-40 characters)")
	}

	return nil
}

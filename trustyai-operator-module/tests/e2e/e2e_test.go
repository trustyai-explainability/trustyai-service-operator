//go:build e2e

//nolint:testpackage
package e2e

import "testing"

// TestE2E is the single entrypoint the CI workflow runs (make test-tom-e2e).
// Subtests share the one cluster/module-operator deployment set up by CI, so
// they run in a fixed order: validation first (does not mutate the singleton
// CR), then the lifecycle test which creates and tears down the CR.
func TestE2E(t *testing.T) {
	t.Run("validation", TestValidation)
	t.Run("lifecycle", TestLifecycle)
}

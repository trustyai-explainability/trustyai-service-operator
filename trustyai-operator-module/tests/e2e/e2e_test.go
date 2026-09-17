//go:build e2e

//nolint:testpackage
package e2e

import "testing"

// TestE2E is the single entrypoint the CI workflow runs (make test-tom-e2e).
// Subtests share the one cluster/module-operator deployment set up by CI, so
// they run in a fixed order: validation first, then the lifecycle test which
// exercises and tears down the singleton CR applied by the combined workflow.
func TestE2E(t *testing.T) {
	t.Run("validation", testValidation)
	t.Run("lifecycle", testLifecycle)
}

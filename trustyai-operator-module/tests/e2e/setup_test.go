//go:build e2e

//nolint:testpackage
package e2e

import (
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if err := SetupTestEnv(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to set up e2e test environment: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

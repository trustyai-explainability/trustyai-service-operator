//go:build e2e

//nolint:testpackage
package e2e

import (
	"context"
	"testing"

	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
)

// TestValidation checks cluster-level preconditions that must hold before the
// lifecycle test runs: the module operator itself is up, and the CRD's
// singleton-name admission rule is enforced by the live API server (not just
// asserted in a unit test against the Go struct/string constant).
func TestValidation(t *testing.T) {
	ctx := context.Background()

	t.Run("module operator deployment is ready", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(requireDeploymentReady(ctx, OperatorNamespace, DeploymentName)).To(gomega.Succeed())
	})

	t.Run("rejects a TrustyAI resource not named 'default'", func(t *testing.T) {
		g := gomega.NewWithT(t)
		invalid := newManagedTrustyAI("not-" + InstanceName)
		err := k8sClient.Create(ctx, invalid)
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("must be named 'default'"))
	})

	t.Run("no stale singleton CR left over from a previous run", func(t *testing.T) {
		g := gomega.NewWithT(t)
		_, err := getModule(ctx)
		g.Expect(errors.IsNotFound(err)).To(gomega.BeTrue(),
			"expected no TrustyAI/%s CR to exist yet; got: %v", InstanceName, err)
	})
}

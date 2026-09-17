//go:build e2e

//nolint:testpackage
package e2e

import (
	"context"
	"testing"

	"github.com/onsi/gomega"
	common "github.com/opendatahub-io/odh-platform-utilities/api/common"
)

// testValidation checks cluster-level preconditions that must hold before the
// lifecycle test runs: the module operator itself is up, and the CRD's
// singleton-name admission rule is enforced by the live API server (not just
// asserted in a unit test against the Go struct/string constant). The fixture
// is applied by the combined Kind workflow before this test starts.
func testValidation(t *testing.T) {
	ctx := context.Background()

	t.Run("module operator deployment is ready", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(requireDeploymentReady(ctx, OperatorNamespace, DeploymentName)).To(gomega.Succeed())
	})

	t.Run("rejects a TrustyAI resource not named 'default-trustyai'", func(t *testing.T) {
		g := gomega.NewWithT(t)
		invalid := newManagedTrustyAI("not-" + InstanceName)
		err := k8sClient.Create(ctx, invalid)
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("must be named 'default-trustyai'"))
	})

	t.Run("the combined test fixture is present", func(t *testing.T) {
		g := gomega.NewWithT(t)
		module, err := getModule(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(module.Spec.ManagementState).To(gomega.Equal(common.Managed))
	})
}

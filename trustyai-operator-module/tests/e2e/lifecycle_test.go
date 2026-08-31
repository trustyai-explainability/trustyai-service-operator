//go:build e2e

//nolint:testpackage
package e2e

import (
	"context"
	"testing"

	"github.com/onsi/gomega"
	common "github.com/opendatahub-io/odh-platform-utilities/api/common"
	"github.com/trustyai-explainability/trustyai-operator-module/pkg/trustyaimodule"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// TestLifecycle drives the singleton TrustyAI CR through creation, the
// Removed management-state cleanup path, and deletion against a real
// cluster - the parts of the reconciler that are stable regardless of #872's
// in-flight health/GC rewrite. It seeds a Prometheus instance first so the
// required-dependency precondition gate doesn't block reconciliation before
// it ever reaches the DSC ConfigMap this test exercises.
func TestLifecycle(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := context.Background()

	g.Expect(createPrometheusInstance(ctx, OperatorNamespace, "e2e-prometheus")).To(gomega.Succeed())
	t.Cleanup(func() {
		_ = deletePrometheusInstance(ctx, OperatorNamespace, "e2e-prometheus")
	})

	t.Run("creates the singleton CR and adds a finalizer", func(t *testing.T) {
		g := gomega.NewWithT(t)
		module := newManagedTrustyAI(InstanceName)
		g.Expect(k8sClient.Create(ctx, module)).To(gomega.Succeed())

		g.Eventually(func() []string {
			m, err := getModule(ctx)
			if err != nil {
				return nil
			}
			return m.Finalizers
		}, pollTimeout, pollInterval).Should(gomega.ContainElement(trustyaimodule.FinalizerName))
	})

	t.Run("reconciles past the dependency gate and creates the DSC ConfigMap", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{
				Name:      trustyaimodule.DSCConfigMapName,
				Namespace: OperatorNamespace,
			}, &corev1.ConfigMap{})
		}, pollTimeout, pollInterval).Should(gomega.Succeed())
	})

	t.Run("Removed management state deletes the DSC ConfigMap and clears observedGeneration lag", func(t *testing.T) {
		g := gomega.NewWithT(t)
		module, err := getModule(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		module.Spec.ManagementState = common.Removed
		g.Expect(k8sClient.Update(ctx, module)).To(gomega.Succeed())

		g.Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      trustyaimodule.DSCConfigMapName,
				Namespace: OperatorNamespace,
			}, &corev1.ConfigMap{})
			return errors.IsNotFound(err)
		}, pollTimeout, pollInterval).Should(gomega.BeTrue())

		g.Eventually(func() common.Phase {
			m, err := getModule(ctx)
			if err != nil {
				return ""
			}
			return m.Status.Phase
		}, pollTimeout, pollInterval).Should(gomega.Equal(common.PhaseNotReady))
	})

	t.Run("deleting the CR removes it and the finalizer completes cleanup", func(t *testing.T) {
		g := gomega.NewWithT(t)
		module, err := getModule(ctx)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(k8sClient.Delete(ctx, module)).To(gomega.Succeed())
		g.Expect(waitForModuleGone(ctx)).To(gomega.Succeed())
	})
}

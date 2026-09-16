//go:build e2e

//nolint:testpackage
package e2e

import (
	"context"
	"testing"

	"github.com/onsi/gomega"
	common "github.com/opendatahub-io/odh-platform-utilities/api/common"
	"github.com/trustyai-explainability/trustyai-operator-module/pkg/trustyaimodule"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

// testLifecycle drives the singleton TrustyAI CR through creation, the
// Removed management-state cleanup path, and deletion against a real
// cluster. It seeds a Prometheus instance first so the required-dependency
// precondition gate does not block reconciliation before it reaches the DSC
// ConfigMap this test exercises.
func testLifecycle(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := context.Background()

	g.Expect(createPrometheusInstance(ctx, OperatorNamespace, "e2e-prometheus")).To(gomega.Succeed())
	t.Cleanup(func() {
		_ = deletePrometheusInstance(ctx, OperatorNamespace, "e2e-prometheus")
	})

	t.Run("creates the singleton CR and adds a finalizer", func(t *testing.T) {
		g := gomega.NewWithT(t)
		module := newTASOnlyModule(InstanceName)
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

	t.Run("deploys workload operator resources and reports provisioning success", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(waitForResource(ctx, types.NamespacedName{
			Name: WorkloadOperatorDeploymentName, Namespace: OperatorNamespace,
		}, &appsv1.Deployment{})).To(gomega.Succeed())
		g.Expect(waitForResource(ctx, types.NamespacedName{
			Name: WorkloadOperatorMetricsServiceName, Namespace: OperatorNamespace,
		}, &corev1.Service{})).To(gomega.Succeed())
		g.Expect(waitForModuleCondition(ctx, string(common.ConditionTypeProvisioningSucceeded), metav1.ConditionTrue)).
			To(gomega.Succeed())
	})

	t.Run("reports Ready when the enabled operand instance is healthy", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(requireDeploymentReady(
			ctx,
			OperatorNamespace,
			WorkloadOperatorDeploymentName,
		)).To(gomega.Succeed())
		g.Expect(createHealthyTrustyAIService(ctx, OperatorNamespace, "e2e-tas")).To(gomega.Succeed())
		t.Cleanup(func() {
			operand := &unstructured.Unstructured{}
			operand.SetGroupVersionKind(trustyAIServiceGVK)
			operand.SetName("e2e-tas")
			operand.SetNamespace(OperatorNamespace)
			_ = k8sClient.Delete(ctx, operand)
		})
		g.Expect(waitForModulePhase(ctx, common.PhaseReady)).To(gomega.Succeed())
	})

	t.Run("records platform version transitions in status.releases", func(t *testing.T) {
		g := gomega.NewWithT(t)
		platformCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      trustyaimodule.PlatformConfigMapName,
				Namespace: OperatorNamespace,
			},
			Data: map[string]string{trustyaimodule.PlatformVersionKey: "3.6.0"},
		}
		g.Expect(k8sClient.Create(ctx, platformCM)).To(gomega.Succeed())
		t.Cleanup(func() { _ = k8sClient.Delete(ctx, platformCM) })

		g.Eventually(func() string {
			module, err := getModule(ctx)
			if err != nil {
				return ""
			}
			return module.Status.GetPlatformRelease()
		}, pollTimeout, pollInterval).Should(gomega.Equal("3.6.0"))

		platformCM.Data[trustyaimodule.PlatformVersionKey] = "3.6.1"
		g.Expect(k8sClient.Update(ctx, platformCM)).To(gomega.Succeed())
		g.Eventually(func() string {
			module, err := getModule(ctx)
			if err != nil {
				return ""
			}
			return module.Status.GetPlatformRelease()
		}, pollTimeout, pollInterval).Should(gomega.Equal("3.6.1"))
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

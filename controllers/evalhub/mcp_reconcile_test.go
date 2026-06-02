package evalhub

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"
)

var _ = Describe("EvalHub MCP reconciliation", func() {
	const (
		testNamespacePrefix = "evalhub-mcp-test"
		evalHubName         = "mcp-evalhub"
	)

	var (
		testNamespace string
		namespace     *corev1.Namespace
		evalHub       *evalhubv1alpha1.EvalHub
		reconciler    *EvalHubReconciler
	)

	BeforeEach(func() {
		testNamespace = fmt.Sprintf("%s-%d", testNamespacePrefix, time.Now().UnixNano())
		namespace = createNamespace(testNamespace)
		Expect(k8sClient.Create(ctx, namespace)).Should(Succeed())

		evalHub = createEvalHubWithMCP(evalHubName, testNamespace, true)
		Expect(k8sClient.Create(ctx, evalHub)).Should(Succeed())

		serviceCA := createServiceCAConfigMap(evalHubName, testNamespace)
		Expect(k8sClient.Create(ctx, serviceCA)).Should(Succeed())

		reconciler, _ = setupReconciler(testNamespace)
	})

	AfterEach(func() {
		cleanupResourcesInNamespace(testNamespace, evalHub, nil)
		deleteNamespace(namespace)
	})

	Context("when MCP is enabled", func() {
		It("should reconcile ConfigMap, Deployment, and Service", func() {
			Expect(reconciler.reconcileMCPConfigMap(ctx, evalHub)).Should(Succeed())
			Expect(reconciler.reconcileMCPDeployment(ctx, evalHub)).Should(Succeed())
			Expect(reconciler.reconcileMCPService(ctx, evalHub)).Should(Succeed())

			cm := waitForConfigMap(mcpConfigMapName(evalHub), testNamespace)
			Expect(cm.OwnerReferences).To(HaveLen(1))
			Expect(cm.OwnerReferences[0].Kind).To(Equal("EvalHub"))
			Expect(cm.Data).To(HaveKey(mcpConfigFileName))

			var cfg MCPConfig
			Expect(yaml.Unmarshal([]byte(cm.Data[mcpConfigFileName]), &cfg)).To(Succeed())
			Expect(cfg.BaseURL).To(Equal(fmt.Sprintf("https://%s.%s.svc.cluster.local:8443", evalHubName, testNamespace)))
			Expect(cfg.Transport).To(Equal("http"))

			deploy := waitForDeployment(mcpDeploymentName(evalHub), testNamespace)
			Expect(deploy.Spec.Template.Labels["component"]).To(Equal("mcp"))
			Expect(deploy.Spec.Template.Spec.Containers).To(HaveLen(2))
			mcpContainer := deploy.Spec.Template.Spec.Containers[0]
			Expect(mcpContainer.Name).To(Equal(mcpContainerName))
			Expect(mcpContainer.Args).To(ContainElement("--config"))
			Expect(mcpContainer.Args).To(ContainElement(mcpConfigFilePath()))
			Expect(mcpContainer.Args).To(ContainElement("127.0.0.1"))

			krp := deploy.Spec.Template.Spec.Containers[1]
			Expect(krp.Name).To(Equal(kubeRBACProxyContainerName))
			Expect(krp.Args).To(ContainElement("--config-file=" + kubeRBACProxyConfigMountPath))

			Expect(cm.Data).To(HaveKey(evalHubAuthConfigMapKey))

			svc := waitForService(mcpServiceName(evalHub), testNamespace)
			Expect(svc.Annotations["service.beta.openshift.io/serving-cert-secret-name"]).To(Equal(mcpServiceName(evalHub) + "-tls"))
			Expect(svc.Spec.Ports[0].Port).To(Equal(int32(mcpServicePort)))
		})

		It("should mount EVALHUB_TOKEN when authSecret is set", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "mcp-token", Namespace: testNamespace},
				Data:       map[string][]byte{"token": []byte("test-token")},
			}
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			evalHub.Spec.MCP.AuthSecret = "mcp-token"
			Expect(k8sClient.Update(ctx, evalHub)).Should(Succeed())

			Expect(reconciler.reconcileMCPDeployment(ctx, evalHub)).Should(Succeed())

			deploy := waitForDeployment(mcpDeploymentName(evalHub), testNamespace)
			var tokenEnv *corev1.EnvVar
			for i := range deploy.Spec.Template.Spec.Containers[0].Env {
				if deploy.Spec.Template.Spec.Containers[0].Env[i].Name == "EVALHUB_TOKEN" {
					tokenEnv = &deploy.Spec.Template.Spec.Containers[0].Env[i]
					break
				}
			}
			Expect(tokenEnv).NotTo(BeNil())
			Expect(tokenEnv.ValueFrom.SecretKeyRef.Name).To(Equal("mcp-token"))
		})

		It("should set MCP status with https URL when deployment is ready", func() {
			Expect(reconciler.reconcileMCPDeployment(ctx, evalHub)).Should(Succeed())

			deploy := waitForDeployment(mcpDeploymentName(evalHub), testNamespace)
			deploy.Status.ReadyReplicas = 1
			deploy.Status.Replicas = 1
			Expect(k8sClient.Status().Update(ctx, deploy)).Should(Succeed())

			reconciler.updateMCPStatus(ctx, evalHub, true)
			Expect(evalHub.Status.MCP).NotTo(BeNil())
			Expect(evalHub.Status.MCP.Phase).To(Equal("Ready"))
			Expect(evalHub.Status.MCP.Ready).To(BeTrue())
			Expect(evalHub.Status.MCP.URL).To(Equal(fmt.Sprintf("https://%s.%s.svc.cluster.local:8443",
				mcpServiceName(evalHub), testNamespace)))
		})
	})

	Context("when MCP is disabled", func() {
		BeforeEach(func() {
			Expect(reconciler.reconcileMCPConfigMap(ctx, evalHub)).Should(Succeed())
			Expect(reconciler.reconcileMCPDeployment(ctx, evalHub)).Should(Succeed())
			Expect(reconciler.reconcileMCPService(ctx, evalHub)).Should(Succeed())
		})

		It("should delete MCP resources", func() {
			disabled := false
			evalHub.Spec.MCP.Enabled = &disabled
			Expect(k8sClient.Update(ctx, evalHub)).Should(Succeed())

			Expect(reconciler.reconcileMCPConfigMap(ctx, evalHub)).Should(Succeed())
			Expect(reconciler.reconcileMCPDeployment(ctx, evalHub)).Should(Succeed())
			Expect(reconciler.reconcileMCPService(ctx, evalHub)).Should(Succeed())

			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: mcpConfigMapName(evalHub), Namespace: testNamespace,
				}, &corev1.ConfigMap{})
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: mcpDeploymentName(evalHub), Namespace: testNamespace,
				}, &appsv1.Deployment{})
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: mcpServiceName(evalHub), Namespace: testNamespace,
				}, &corev1.Service{})
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			reconciler.updateMCPStatus(ctx, evalHub, true)
			Expect(evalHub.Status.MCP.Phase).To(Equal("Disabled"))
			Expect(evalHub.Status.MCP.Ready).To(BeFalse())
		})
	})

	Context("when MCP is unset", func() {
		It("should report Disabled MCP status", func() {
			evalHubNoMCP := createEvalHubInstanceWithSQLite("no-mcp", testNamespace)
			Expect(k8sClient.Create(ctx, evalHubNoMCP)).Should(Succeed())

			reconciler.updateMCPStatus(ctx, evalHubNoMCP, true)
			Expect(evalHubNoMCP.Status.MCP).NotTo(BeNil())
			Expect(evalHubNoMCP.Status.MCP.Phase).To(Equal("Disabled"))
		})
	})
})

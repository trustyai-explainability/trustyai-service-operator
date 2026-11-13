package tas

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	appsv1 "k8s.io/api/apps/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// Readiness without a serving API is the claim the RFE is about, and it is the
// one claim the Kind legs cannot make: IsAllReady includes RouteReady and a Kind
// cluster has no OpenShift router, so the operand never reports Ready there
// whatever serving does. Here route, PVC and deployment readiness are driven
// directly, so the Ready value is the operator's own verdict.
//
// The CRD is deleted from the API server rather than left out of a fake world,
// because the verdict comes from an uncached read of the CRD: absent versus
// present is the API server's answer, not a fixture's. Deleting it also covers
// the running-operator case, where serving goes away under a live controller and
// the only thing that can notice is the reconcile requeue.
var _ = Describe("Readiness without serving", func() {
	It("stays Ready when the serving CRD is deleted under a running operator", func() {
		// Other TAS specs replace the suite client with fake clients, so this one
		// brings its own API-backed client. The manager started in BeforeSuite is
		// API-backed and does the reconciling.
		apiClient, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
		Expect(err).NotTo(HaveOccurred())
		apiCtx := context.Background()

		// Whatever happens below, the later specs need the serving API the suite
		// installed.
		DeferCleanup(func() {
			_, installErr := envtest.InstallCRDs(cfg, envtest.CRDInstallOptions{
				Paths:              []string{servingCRDFixturePath},
				ErrorIfPathMissing: true,
			})
			Expect(installErr).NotTo(HaveOccurred(), "failed to restore the serving CRD")
		})

		conditionOf := func(instance *trustyaiopendatahubiov1.TrustyAIService, conditionType string) *trustyaiopendatahubiov1.TASCondition {
			for i := range instance.Status.Conditions {
				if instance.Status.Conditions[i].Type == conditionType {
					return &instance.Status.Conditions[i]
				}
			}
			return nil
		}

		namespace := "serving-free-readiness"
		Expect(createNamespace(apiCtx, apiClient, namespace)).To(Succeed())
		instance := createDefaultPVCCustomResource(namespace)
		// The API server assigns the identity it will use in owner references.
		instance.UID = ""
		Expect(apiClient.Create(apiCtx, instance)).To(Succeed())
		key := client.ObjectKeyFromObject(instance)
		DeferCleanup(func() {
			// The finalizer removes the operands, which envtest cannot garbage
			// collect on its own.
			Expect(client.IgnoreNotFound(apiClient.Delete(apiCtx, instance))).To(Succeed())
		})

		By("driving route, PVC and deployment to ready with serving installed")
		Eventually(func() error {
			return apiClient.Get(apiCtx, key, &appsv1.Deployment{})
		}, defaultTimeout, defaultPolling).Should(Succeed(), "the operator did not create the deployment")

		// envtest has no router controller and no volume binder, so the two
		// readiness verdicts the operator reads from those resources are set here.
		Eventually(func() error {
			return makeRouteReady(apiCtx, apiClient, instance)
		}, defaultTimeout, defaultPolling).Should(Succeed(), "failed to make route ready")
		Eventually(func() error {
			return makePVCReady(apiCtx, apiClient, instance)
		}, defaultTimeout, defaultPolling).Should(Succeed(), "failed to bind PVC")
		Eventually(func() error {
			return makeDeploymentReady(apiCtx, apiClient, instance)
		}, defaultTimeout, defaultPolling).Should(Succeed(), "failed to make deployment ready")

		By("asserting Ready with serving installed but no InferenceService")
		Eventually(func(g Gomega) {
			g.Expect(apiClient.Get(apiCtx, key, instance)).To(Succeed())
			ready := conditionOf(instance, PhaseReady)
			g.Expect(ready).ToNot(BeNil(), "the operator has not reported Ready yet")
			g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
			serving := conditionOf(instance, StatusTypeInferenceServicesPresent)
			g.Expect(serving).ToNot(BeNil())
			g.Expect(serving.Reason).To(Equal(StatusReasonNoInferenceServices))
		}, defaultTimeout, defaultPolling).Should(Succeed())

		By("deleting the serving CRD while the operator runs")
		crd := &apiextensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: inferenceServiceCRDName}}
		Expect(apiClient.Delete(apiCtx, crd)).To(Succeed())
		Eventually(func() bool {
			err := apiClient.Get(apiCtx, client.ObjectKeyFromObject(crd), &apiextensionsv1.CustomResourceDefinition{})
			return apierrors.IsNotFound(err)
		}, defaultTimeout, defaultPolling).Should(BeTrue(), "the serving CRD was not removed")

		By("waiting for the requeue, which is the only thing that re-derives serving state")
		// Nothing watches serving, so the verdict can only change on the next pass:
		// defaultRequeueDelay after the last one. Serving is informational, so the
		// same pass has to keep readiness where it was.
		Eventually(func(g Gomega) {
			g.Expect(apiClient.Get(apiCtx, key, instance)).To(Succeed())
			ready := conditionOf(instance, PhaseReady)
			g.Expect(ready).ToNot(BeNil())
			g.Expect(ready.Status).To(Equal(metav1.ConditionTrue), "serving's absence must not change readiness")
			serving := conditionOf(instance, StatusTypeInferenceServicesPresent)
			g.Expect(serving).ToNot(BeNil())
			g.Expect(serving.Reason).To(Equal(StatusReasonServingCRDNotFound))
			g.Expect(serving.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(serving.Message).To(ContainSubstring(inferenceServiceCRDName),
				"the condition has to name the CRD an administrator installs")
		}, defaultRequeueDelay+30*time.Second, defaultPolling).Should(Succeed())
	})
})

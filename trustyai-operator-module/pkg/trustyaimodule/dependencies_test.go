package trustyaimodule

import (
	"context"
	stderrors "errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	common "github.com/opendatahub-io/odh-platform-utilities/api/common"
	"github.com/opendatahub-io/odh-platform-utilities/pkg/cluster"
	"github.com/opendatahub-io/odh-platform-utilities/pkg/controller/action"
	"github.com/opendatahub-io/odh-platform-utilities/pkg/controller/conditions"
	"github.com/opendatahub-io/odh-platform-utilities/pkg/controller/precondition"
	platformv1alpha1 "github.com/trustyai-explainability/trustyai-operator-module/pkg/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var prometheusGVK = schema.GroupVersionKind{
	Group:   "monitoring.coreos.com",
	Version: "v1",
	Kind:    "Prometheus",
}

// stubListErrClient wraps a real client but forces List to return a
// caller-supplied error, used to simulate the Prometheus CRD not being
// installed at all (a scenario the shared envtest cluster - which has the
// CRD installed for the whole suite - cannot otherwise produce).
type stubListErrClient struct {
	client.Client
	err error
}

func (s *stubListErrClient) List(_ context.Context, _ client.ObjectList, _ ...client.ListOption) error {
	return s.err
}

// stubMapperClient answers RESTMapper() with a caller-supplied mapper, which is
// how the optional-API report is driven without installing a serving CRD in the
// shared envtest cluster.
type stubMapperClient struct {
	client.Client
	mapper meta.RESTMapper
}

func (s *stubMapperClient) RESTMapper() meta.RESTMapper {
	return s.mapper
}

// stubRESTMapper returns one error for every mapping, standing in for an API
// server that could not answer.
type stubRESTMapper struct {
	meta.RESTMapper
	err error
}

func (s *stubRESTMapper) RESTMapping(_ schema.GroupKind, _ ...string) (*meta.RESTMapping, error) {
	return nil, s.err
}

// moduleCondition returns the named condition of a module CR, or nil.
func moduleCondition(module *platformv1alpha1.TrustyAI, conditionType string) *common.Condition {
	for _, condition := range module.Status.GetConditions() {
		if condition.Type == conditionType {
			return &condition
		}
	}

	return nil
}

var _ = Describe("Dependency preconditions", func() {
	const depsNamespace = "dependency-test"

	ctx := context.Background()

	BeforeEach(func() {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: depsNamespace}}
		err := k8sClient.Create(ctx, ns)
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}
	})

	AfterEach(func() {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(prometheusGVK)
		Expect(k8sClient.List(ctx, list, client.InNamespace(depsNamespace))).To(Succeed())
		for i := range list.Items {
			Expect(k8sClient.Delete(ctx, &list.Items[i])).To(Succeed())
		}
	})

	Context("checkPrometheus", func() {
		It("fails when the Prometheus CRD is not installed", func() {
			stub := &stubListErrClient{
				err: &meta.NoKindMatchError{GroupKind: prometheusGVK.GroupKind()},
			}
			rr := &action.ReconciliationRequest{Client: stub}

			result, err := checkPrometheus(ctx, rr)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Pass).To(BeFalse())
			Expect(result.Message).To(ContainSubstring("not installed"))
		})

		It("fails when the Prometheus CRD is present but no instance exists", func() {
			rr := &action.ReconciliationRequest{Client: k8sClient}

			result, err := checkPrometheus(ctx, rr)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Pass).To(BeFalse())
			Expect(result.Message).To(ContainSubstring("no Prometheus instance exists"))
		})

		It("passes when a Prometheus instance exists", func() {
			prom := &unstructured.Unstructured{}
			prom.SetGroupVersionKind(prometheusGVK)
			prom.SetName("test-prometheus")
			prom.SetNamespace(depsNamespace)
			Expect(k8sClient.Create(ctx, prom)).To(Succeed())

			rr := &action.ReconciliationRequest{Client: k8sClient}

			result, err := checkPrometheus(ctx, rr)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Pass).To(BeTrue())
		})
	})

	Context("modulePreConditions via RunAll", func() {
		newRequest := func(module *platformv1alpha1.TrustyAI) *action.ReconciliationRequest {
			condMgr := (&TrustyAIModuleReconciler{}).newConditionManager(module)
			return &action.ReconciliationRequest{
				Client:     k8sClient,
				Instance:   module,
				Conditions: condMgr,
			}
		}

		It("stops reconciliation when Prometheus is required but no instance exists", func() {
			module := &platformv1alpha1.TrustyAI{}
			rr := newRequest(module)

			stop := precondition.RunAll(ctx, rr, cluster.ClusterType(""), modulePreConditions)
			Expect(stop).To(BeTrue())

			// Only a required condition can stop the run: the optional serving API is
			// not a precondition at all (see reportServingAPI), so the stop signal
			// cannot come from it.
			depsCond := module.Status.GetConditions()
			found := false
			for _, c := range depsCond {
				if c.Type == ConditionTypeDependenciesAvailable {
					found = true
					Expect(c.Status).To(Equal(metav1.ConditionFalse))
				}
			}
			Expect(found).To(BeTrue())
		})

		It("does not stop reconciliation once a Prometheus instance exists", func() {
			prom := &unstructured.Unstructured{}
			prom.SetGroupVersionKind(prometheusGVK)
			prom.SetName("test-prometheus-runall")
			prom.SetNamespace(depsNamespace)
			Expect(k8sClient.Create(ctx, prom)).To(Succeed())

			module := &platformv1alpha1.TrustyAI{}
			rr := newRequest(module)

			stop := precondition.RunAll(ctx, rr, cluster.ClusterType(""), modulePreConditions)
			Expect(stop).To(BeFalse())

			depsCond := module.Status.GetConditions()
			for _, c := range depsCond {
				if c.Type == ConditionTypeDependenciesAvailable {
					Expect(c.Status).To(Equal(metav1.ConditionTrue))
				}
			}
		})

		It("leaves the optional serving API out of the preconditions entirely", func() {
			prom := &unstructured.Unstructured{}
			prom.SetGroupVersionKind(prometheusGVK)
			prom.SetName("test-prometheus-no-kserve-condition")
			prom.SetNamespace(depsNamespace)
			Expect(k8sClient.Create(ctx, prom)).To(Succeed())

			module := &platformv1alpha1.TrustyAI{}
			rr := newRequest(module)

			Expect(precondition.RunAll(ctx, rr, cluster.ClusterType(""), modulePreConditions)).To(BeFalse())

			// A precondition writes PreConditionFailed when unmet, so the serving API
			// must not be one: the condition is reported by reportServingAPI only.
			Expect(moduleCondition(module, ConditionTypeKServeAvailable)).To(BeNil())
		})
	})

	Context("reportServingAPI", func() {
		newModule := func() (*platformv1alpha1.TrustyAI, *conditions.Manager) {
			module := &platformv1alpha1.TrustyAI{}
			module.Generation = 7

			return module, (&TrustyAIModuleReconciler{}).newConditionManager(module)
		}

		It("reports an absent API as NotInstalled/Info, never as a failed precondition", func() {
			module, condMgr := newModule()

			// An empty mapper answers NoKindMatchError, which is what an install
			// without KServe produces. The suite's own envtest cluster installs the
			// InferenceService CRD (the fixtures need it), so the real mapper cannot
			// produce this case.
			empty := meta.NewDefaultRESTMapper([]schema.GroupVersion{})

			reportServingAPI(ctx, &stubMapperClient{Client: k8sClient, mapper: empty}, module, condMgr)

			condition := moduleCondition(module, ConditionTypeKServeAvailable)
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal(ConditionReasonServingAPINotInstalled))
			Expect(condition.Reason).NotTo(Equal(precondition.PreConditionFailedReason))
			Expect(condition.Severity).To(Equal(common.ConditionSeverityInfo))
			Expect(condition.ObservedGeneration).To(Equal(module.Generation))
			Expect(condition.Message).To(ContainSubstring("not installed"))
			Expect(condition.Message).To(ContainSubstring("stays inactive"))
		})

		It("reports a present API as Available", func() {
			module, condMgr := newModule()
			mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{servingAPIGVK.GroupVersion()})
			mapper.Add(servingAPIGVK, meta.RESTScopeNamespace)

			reportServingAPI(ctx, &stubMapperClient{Client: k8sClient, mapper: mapper}, module, condMgr)

			condition := moduleCondition(module, ConditionTypeKServeAvailable)
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			Expect(condition.Reason).To(Equal(ConditionReasonServingAPIAvailable))
			Expect(condition.Message).To(ContainSubstring("integration is active"))
		})

		It("reports an unreadable API as Unknown with the API server's error", func() {
			module, condMgr := newModule()
			readErr := stderrors.New("the server is currently unable to handle the request")

			reportServingAPI(
				ctx,
				&stubMapperClient{Client: k8sClient, mapper: &stubRESTMapper{err: readErr}},
				module,
				condMgr,
			)

			condition := moduleCondition(module, ConditionTypeKServeAvailable)
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionUnknown))
			Expect(condition.Reason).To(Equal(ConditionReasonServingAPIUnreadable))
			Expect(condition.Message).To(ContainSubstring("unable to handle the request"))
			Expect(condition.Severity).To(Equal(common.ConditionSeverityInfo))
		})

		It("is a no-op without a client or a condition manager", func() {
			module, condMgr := newModule()

			reportServingAPI(ctx, nil, module, condMgr)
			reportServingAPI(ctx, k8sClient, module, nil)

			Expect(moduleCondition(module, ConditionTypeKServeAvailable)).To(BeNil())
		})
	})
})

package trustyaimodule

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/opendatahub-io/odh-platform-utilities/pkg/cluster"
	"github.com/opendatahub-io/odh-platform-utilities/pkg/controller/action"
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

			// KServe and Prometheus write to distinct condition types (KServeAvailable
			// vs DependenciesAvailable), so a KServe failure could never contribute to
			// this stop signal - only the required Prometheus check can.
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

		It("does not stop reconciliation once a Prometheus instance exists, regardless of KServe", func() {
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
	})
})

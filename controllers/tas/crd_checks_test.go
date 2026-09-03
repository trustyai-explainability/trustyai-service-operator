package tas

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// Regression test for a bug where isVirtualServiceCRDPresent checked the
// DestinationRule CRD name instead of its own, so the two checks could never
// disagree even though they gate independent features.
var _ = Describe("Optional CRD presence checks", func() {
	It("checks each optional CRD by its own distinct name", func() {
		crd := func(name string) *apiextensionsv1.CustomResourceDefinition {
			return &apiextensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: name}}
		}

		// Only the DestinationRule CRD exists.
		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(
			crd(destinationRuleCDRName),
		).Build()
		r := &TrustyAIServiceReconciler{
			Client:        fakeClient,
			Scheme:        scheme.Scheme,
			EventRecorder: record.NewFakeRecorder(10),
			Namespace:     operatorNamespace,
		}

		present, err := r.isDestinationRuleCRDPresent(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(present).To(BeTrue(), "DestinationRule CRD should be reported present")

		present, err = r.isVirtualServiceCRDPresent(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(present).To(BeFalse(), "VirtualService CRD should be reported absent when only DestinationRule exists")

		present, err = r.isInferenceServiceCRDPresent(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(present).To(BeFalse(), "InferenceService CRD should be reported absent when only DestinationRule exists")
	})
})

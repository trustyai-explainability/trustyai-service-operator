package controllers

import (
	"context"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"time"
)

var _ = Describe("Route Reconciliation", func() {

	BeforeEach(func() {
		reconciler = &TrustyAIServiceReconciler{
			Client:        k8sClient,
			Scheme:        scheme.Scheme,
			EventRecorder: recorder,
		}
		ctx = context.Background()
	})

	Context("When Route does not exist", func() {
		var instance *trustyaiopendatahubiov1alpha1.TrustyAIService
		It("Should create Route successfully", func() {
			namespace := "route-test-namespace-1"
			instance = createDefaultCR(namespace)

			Eventually(func() error {
				return createNamespace(ctx, k8sClient, namespace)
			}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to create namespace")

			err := reconciler.reconcileRoute(instance, ctx)
			Expect(err).ToNot(HaveOccurred())

			route := &routev1.Route{}
			err = reconciler.Client.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, route)
			Expect(err).ToNot(HaveOccurred())
			Expect(route).ToNot(BeNil())
			Expect(route.Spec.To.Name).To(Equal(instance.Name))
		})
	})

	Context("When Route exists and is the same", func() {
		var instance *trustyaiopendatahubiov1alpha1.TrustyAIService
		It("Should not update Route", func() {
			namespace := "route-test-namespace-2"
			instance = createDefaultCR(namespace)
			Eventually(func() error {
				return createNamespace(ctx, k8sClient, namespace)
			}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to create namespace")

			// Create a Route with the expected spec
			existingRoute := reconciler.createRouteObject(instance)
			Expect(reconciler.Client.Create(ctx, existingRoute)).To(Succeed())

			err := reconciler.reconcileRouteAuth(instance, ctx, reconciler.createRouteObject)
			Expect(err).ToNot(HaveOccurred())

			// Fetch the Route
			route := &routev1.Route{}
			err = reconciler.Client.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, route)
			Expect(err).ToNot(HaveOccurred())
			Expect(route.Spec).To(Equal(existingRoute.Spec))
		})
	})
})

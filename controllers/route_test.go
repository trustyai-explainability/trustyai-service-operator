package controllers

import (
	"context"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
)

func setupAndTestRouteCreation(instance *trustyaiopendatahubiov1alpha1.TrustyAIService, namespace string) {
	WaitFor(func() error {
		return createNamespace(ctx, k8sClient, namespace)
	}, "failed to create namespace")

	err := reconciler.reconcileRouteAuth(instance, ctx, reconciler.createRouteObject)
	Expect(err).ToNot(HaveOccurred())

	route := &routev1.Route{}
	err = reconciler.Client.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, route)
	Expect(err).ToNot(HaveOccurred())
	Expect(route).ToNot(BeNil())
	Expect(route.Spec.To.Name).To(Equal(instance.Name + "-tls"))

}

func setupAndTestSameRouteCreation(instance *trustyaiopendatahubiov1alpha1.TrustyAIService, namespace string) {
	WaitFor(func() error {
		return createNamespace(ctx, k8sClient, namespace)
	}, "failed to create namespace")

	// Create a Route with the expected spec
	existingRoute, err := reconciler.createRouteObject(ctx, instance)
	Expect(err).ToNot(HaveOccurred())
	Expect(reconciler.Client.Create(ctx, existingRoute)).To(Succeed())

	err = reconciler.reconcileRouteAuth(instance, ctx, reconciler.createRouteObject)
	Expect(err).ToNot(HaveOccurred())

	// Fetch the Route
	route := &routev1.Route{}
	err = reconciler.Client.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, route)
	Expect(err).ToNot(HaveOccurred())
	Expect(route.Spec).To(Equal(existingRoute.Spec))
}

var _ = Describe("Route Reconciliation", func() {

	BeforeEach(func() {
		recorder = record.NewFakeRecorder(10)
		reconciler = &TrustyAIServiceReconciler{
			Client:        k8sClient,
			Scheme:        scheme.Scheme,
			EventRecorder: recorder,
		}
		ctx = context.Background()
	})

	Context("When Route does not exist", func() {
		var instance *trustyaiopendatahubiov1alpha1.TrustyAIService
		It("Should create Route successfully in PVC-mode", func() {
			namespace := "route-test-namespace-1-pvc"
			instance = createDefaultPVCCustomResource(namespace)
			setupAndTestRouteCreation(instance, namespace)
		})
		It("Should create Route successfully in DB-mode", func() {
			namespace := "route-test-namespace-1-db"
			instance = createDefaultDBCustomResource(namespace)
			setupAndTestRouteCreation(instance, namespace)
		})
		It("Should create Route successfully in migration-mode", func() {
			namespace := "route-test-namespace-1-migration"
			instance = createDefaultMigrationCustomResource(namespace)
			setupAndTestRouteCreation(instance, namespace)
		})

	})

	Context("When Route exists and is the same", func() {
		var instance *trustyaiopendatahubiov1alpha1.TrustyAIService
		It("Should not update Route in PVC-mode", func() {
			namespace := "route-test-namespace-2-pvc"
			instance = createDefaultPVCCustomResource(namespace)
			setupAndTestSameRouteCreation(instance, namespace)
		})
		It("Should not update Route in DB-mode", func() {
			namespace := "route-test-namespace-2-db"
			instance = createDefaultDBCustomResource(namespace)
			setupAndTestSameRouteCreation(instance, namespace)
		})
		It("Should not update Route in migration-mode", func() {
			namespace := "route-test-namespace-2-migration"
			instance = createDefaultMigrationCustomResource(namespace)
			setupAndTestSameRouteCreation(instance, namespace)
		})

	})
})

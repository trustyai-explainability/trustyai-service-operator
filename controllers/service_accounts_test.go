package controllers

import (
	"context"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
)

var _ = Describe("Service Accounts", func() {

	BeforeEach(func() {
		recorder = record.NewFakeRecorder(10)
		reconciler = &TrustyAIServiceReconciler{
			Client:        k8sClient,
			Scheme:        scheme.Scheme,
			EventRecorder: recorder,
		}
		ctx = context.Background()
	})

	Context("When creating multiple services", func() {
		It("Should create SAs, CRBs successfully", func() {

			namespace1 := "sa-test-namespace-1"
			instance1 := createDefaultPVCCustomResource(namespace1)

			WaitFor(func() error {
				return createNamespace(ctx, k8sClient, namespace1)
			}, "failed to create namespace")

			WaitFor(func() error { return reconciler.createServiceAccount(ctx, instance1) }, "failed to create service account")

			namespace2 := "sa-test-namespace-2"

			instance2 := createDefaultPVCCustomResource(namespace2)

			WaitFor(func() error {
				return createNamespace(ctx, k8sClient, namespace2)
			}, "failed to create namespace")

			WaitFor(func() error { return reconciler.createServiceAccount(ctx, instance2) }, "failed to create service account")

			serviceAccount1 := &corev1.ServiceAccount{}
			serviceAccountName1 := instance1.Name + "-proxy"
			err := k8sClient.Get(ctx, types.NamespacedName{Name: serviceAccountName1, Namespace: namespace1}, serviceAccount1)
			Expect(err).ToNot(HaveOccurred())
			Expect(serviceAccount1).ToNot(BeNil())
			Expect(serviceAccount1.Name).To(Equal(generateServiceAccountName(instance1)))
			Expect(serviceAccountName1).To(Equal(generateServiceAccountName(instance1)))

			serviceAccount2 := &corev1.ServiceAccount{}
			serviceAccountName2 := instance2.Name + "-proxy"
			err = k8sClient.Get(ctx, types.NamespacedName{Name: serviceAccountName2, Namespace: namespace2}, serviceAccount2)
			Expect(err).ToNot(HaveOccurred())
			Expect(serviceAccount2).ToNot(BeNil())
			Expect(serviceAccount2.Name).To(Equal(generateServiceAccountName(instance2)))
			Expect(serviceAccountName2).To(Equal(generateServiceAccountName(instance2)))

			checkServiceAccountAnnotations(ctx, instance1, k8sClient)
			checkServiceAccountAnnotations(ctx, instance2, k8sClient)

			checkClusterRoleBinding(ctx, instance1, k8sClient)
			checkClusterRoleBinding(ctx, instance2, k8sClient)

		})
	})

})

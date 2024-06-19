package controllers

import (
	"context"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("PVC Reconciliation", func() {

	BeforeEach(func() {
		k8sClient = fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
		recorder = record.NewFakeRecorder(10)
		reconciler = &TrustyAIServiceReconciler{
			Client:        k8sClient,
			Scheme:        scheme.Scheme,
			EventRecorder: recorder,
		}
		ctx = context.Background()
	})

	Context("when the PVC does not exist", func() {
		var instance *trustyaiopendatahubiov1alpha1.TrustyAIService
		It("should create a new PVC and emit an event", func() {
			namespace := "pvc-test-namespace-1"
			instance = createDefaultPVCCustomResource(namespace)
			WaitFor(func() error {
				return createNamespace(ctx, k8sClient, namespace)
			}, "failed to create namespace")

			err := reconciler.ensurePVC(ctx, instance)
			Expect(err).ToNot(HaveOccurred())

			pvc := &corev1.PersistentVolumeClaim{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: generatePVCName(instance), Namespace: instance.Namespace}, pvc)
			Expect(err).ToNot(HaveOccurred())
			Expect(pvc).ToNot(BeNil())
			Expect(pvc.Spec.Resources.Requests[corev1.ResourceStorage]).To(Equal(resource.MustParse(instance.Spec.Storage.Size)))

			// Check for event
			Eventually(recorder.Events).Should(Receive(ContainSubstring("Created PVC")))
		})
	})

	Context("when the PVC already exists", func() {
		var instance *trustyaiopendatahubiov1alpha1.TrustyAIService
		It("should not attempt to create the PVC", func() {
			namespace := "pvc-test-namespace-2"
			instance = createDefaultPVCCustomResource(namespace)
			WaitFor(func() error {
				return createNamespace(ctx, k8sClient, namespace)
			}, "failed to create namespace")

			// Simulate existing PVC
			existingPVC := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      generatePVCName(instance),
					Namespace: instance.Namespace,
				},
			}
			err := k8sClient.Create(ctx, existingPVC)
			Expect(err).ToNot(HaveOccurred())

			err = reconciler.ensurePVC(ctx, instance)
			Expect(err).ToNot(HaveOccurred())

			// Check no event related for PVC creation
			Consistently(recorder.Events).ShouldNot(Receive(ContainSubstring("Created PVC")))
		})
	})

	Context("when a migration CR is made", func() {
		var instance *trustyaiopendatahubiov1alpha1.TrustyAIService
		It("Check all fields are correct", func() {
			namespace := "pvc-test-namespace-3"
			instance = createDefaultMigrationCustomResource(namespace)

			Expect(instance.IsMigration()).To(BeTrue())
			Expect(instance.Spec.Storage.IsStoragePVC()).To(BeFalse())
			Expect(instance.Spec.Storage.IsStorageDatabase()).To(BeTrue())
		})
	})

})

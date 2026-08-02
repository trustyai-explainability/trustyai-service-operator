package module

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	modulev1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/module/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

var _ = Describe("SSA Migration", func() {
	var reconciler *TrustyAIReconciler

	BeforeEach(func() {
		reconciler = setupReconciler()
	})

	AfterEach(func() {
		// Clean up all TrustyAI instances
		moduleList := &modulev1alpha1.TrustyAIList{}
		if err := k8sClient.List(ctx, moduleList); err == nil {
			for i := range moduleList.Items {
				module := &moduleList.Items[i]
				if err := k8sClient.Delete(ctx, module); err == nil {
					// Remove finalizer to allow deletion
					module.Finalizers = []string{}
					_ = k8sClient.Update(ctx, module)
				}
			}
		}
	})

	Context("When adopting in-tree resources", func() {
		It("Should mark adoption complete on fresh install", func() {
			module := createModuleInstance("default", modulev1alpha1.ManagementStateManaged)
			Expect(k8sClient.Create(ctx, module)).To(Succeed())

			err := reconciler.adoptInTreeResources(ctx, module)
			Expect(err).NotTo(HaveOccurred())

			// Verify annotation added
			updated := &modulev1alpha1.TrustyAI{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "default"}, updated)).To(Succeed())
			Expect(updated.Annotations).To(HaveKey(SSAAdoptionAnnotationKey))
			Expect(updated.Annotations[SSAAdoptionAnnotationKey]).To(Equal("true"))

			// Cleanup
			Expect(k8sClient.Delete(ctx, module)).To(Succeed())
		})

		It("Should skip adoption when already completed", func() {
			module := createModuleInstance("default", modulev1alpha1.ManagementStateManaged)
			if module.Annotations == nil {
				module.Annotations = make(map[string]string)
			}
			module.Annotations[SSAAdoptionAnnotationKey] = "true"
			Expect(k8sClient.Create(ctx, module)).To(Succeed())

			// Run adoption - should be no-op
			err := reconciler.adoptInTreeResources(ctx, module)
			Expect(err).NotTo(HaveOccurred())

			// Cleanup
			Expect(k8sClient.Delete(ctx, module)).To(Succeed())
		})

		It("Should adopt pre-existing ConfigMaps with in-tree label", func() {
			// Create ConfigMap with in-tree label in test namespace
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "trustyai-config",
					Namespace: reconciler.Namespace,
					Labels: map[string]string{
						InTreeManagedByLabel: "true",
					},
				},
				Data: map[string]string{"key": "value"},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			// Run adoption
			module := createModuleInstance("default", modulev1alpha1.ManagementStateManaged)
			Expect(k8sClient.Create(ctx, module)).To(Succeed())

			err := reconciler.adoptInTreeResources(ctx, module)
			Expect(err).NotTo(HaveOccurred())

			// Verify annotation added to module
			updated := &modulev1alpha1.TrustyAI{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "default"}, updated)).To(Succeed())
			Expect(updated.Annotations).To(HaveKey(SSAAdoptionAnnotationKey))

			// Verify ConfigMap has adoption annotation
			adoptedCM := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "trustyai-config", Namespace: reconciler.Namespace,
			}, adoptedCM)).To(Succeed())
			Expect(adoptedCM.Annotations).To(HaveKey("trustyai.opendatahub.io/adopted-from"))

			// Cleanup
			Expect(k8sClient.Delete(ctx, cm)).To(Succeed())
			Expect(k8sClient.Delete(ctx, module)).To(Succeed())
		})

		It("Should adopt pre-existing Deployments with in-tree label", func() {
			// Create Deployment with in-tree label in test namespace
			deploy := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "trustyai-service-deploy",
					Namespace: reconciler.Namespace,
					Labels: map[string]string{
						InTreeManagedByLabel: "true",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To(int32(1)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "trustyai",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "trustyai",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "trustyai",
									Image: "trustyai:latest",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deploy)).To(Succeed())

			// Run adoption (module must be named "default" due to CRD validation)
			module := createModuleInstance("default", modulev1alpha1.ManagementStateManaged)
			Expect(k8sClient.Create(ctx, module)).To(Succeed())

			err := reconciler.adoptInTreeResources(ctx, module)
			Expect(err).NotTo(HaveOccurred())

			// Verify annotation added to module
			updated := &modulev1alpha1.TrustyAI{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "default"}, updated)).To(Succeed())
			Expect(updated.Annotations).To(HaveKey(SSAAdoptionAnnotationKey))

			// Verify Deployment has adoption annotation
			adoptedDeploy := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "trustyai-service-deploy", Namespace: reconciler.Namespace,
			}, adoptedDeploy)).To(Succeed())
			Expect(adoptedDeploy.Annotations).To(HaveKey("trustyai.opendatahub.io/adopted-from"))

			// Cleanup
			Expect(k8sClient.Delete(ctx, deploy)).To(Succeed())
			Expect(k8sClient.Delete(ctx, module)).To(Succeed())
		})

		It("Should adopt pre-existing Services with in-tree label", func() {
			// Create Service with in-tree label in test namespace
			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "trustyai-api-svc",
					Namespace: reconciler.Namespace,
					Labels: map[string]string{
						InTreeManagedByLabel: "true",
					},
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{
						"app": "trustyai",
					},
					Ports: []corev1.ServicePort{
						{
							Port: 8080,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, svc)).To(Succeed())

			// Run adoption (module must be named "default" due to CRD validation)
			module := createModuleInstance("default", modulev1alpha1.ManagementStateManaged)
			Expect(k8sClient.Create(ctx, module)).To(Succeed())

			err := reconciler.adoptInTreeResources(ctx, module)
			Expect(err).NotTo(HaveOccurred())

			// Verify annotation added to module
			updated := &modulev1alpha1.TrustyAI{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "default"}, updated)).To(Succeed())
			Expect(updated.Annotations).To(HaveKey(SSAAdoptionAnnotationKey))

			// Verify Service has adoption annotation
			adoptedSvc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "trustyai-api-svc", Namespace: reconciler.Namespace,
			}, adoptedSvc)).To(Succeed())
			Expect(adoptedSvc.Annotations).To(HaveKey("trustyai.opendatahub.io/adopted-from"))

			// Cleanup
			Expect(k8sClient.Delete(ctx, svc)).To(Succeed())
			Expect(k8sClient.Delete(ctx, module)).To(Succeed())
		})
	})
})

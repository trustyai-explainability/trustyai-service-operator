/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tapm

import (
	"context"
	trustyaiv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/tapm/v1alpha1"
	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("TrustyAIPipelineManifest Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		kfpipelineimages := &trustyaiv1alpha1.TrustyAIPipelineManifest{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind TrustyAIPipelineManifest")
			err := k8sClient.Get(ctx, typeNamespacedName, kfpipelineimages)
			if err != nil && errors.IsNotFound(err) {
				resource := &trustyaiv1alpha1.TrustyAIPipelineManifest{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					// TODO(user): Specify other spec details if needed.
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &trustyaiv1alpha1.TrustyAIPipelineManifest{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance TrustyAIPipelineManifest")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &TrustyAIPipelineManifestReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})

		AfterEach(func() {
			// Cleanup logic after each test, like removing the resource instance and associated ConfigMap.
			resource := &trustyaiv1alpha1.TrustyAIPipelineManifest{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance TrustyAIPipelineManifest")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			By("Cleanup the ConfigMap created by the controller")
			configMap := &corev1.ConfigMap{}
			configMapName := resourceName // Adjust if the ConfigMap uses a different naming convention
			configMapNamespace := "default"
			err = k8sClient.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: configMapNamespace}, configMap)
			if err == nil {
				Expect(k8sClient.Delete(ctx, configMap)).To(Succeed())
			}
		})
	})
})

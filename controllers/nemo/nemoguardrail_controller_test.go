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

package nemo

//
//var _ = Describe("NemoGuardrails Controller", func() {
//	Context("When reconciling a resource", func() {
//		const resourceName = "test-resource"
//
//		ctx := context.Background()
//
//		typeNamespacedName := types.NamespacedName{
//			Name:      resourceName,
//			Namespace: "default", // TODO(user):Modify as needed
//		}
//		nemoguardrail := &trustyaiv1alpha1.NemoGuardrails{}
//
//		BeforeEach(func() {
//			By("creating the custom resource for the Kind NemoGuardrails")
//			err := k8sClient.Get(ctx, typeNamespacedName, nemoguardrail)
//			if err != nil && errors.IsNotFound(err) {
//				resource := &trustyaiv1alpha1.NemoGuardrails{
//					ObjectMeta: metav1.ObjectMeta{
//						Name:      resourceName,
//						Namespace: "default",
//					},
//					// TODO(user): Specify other spec details if needed.
//				}
//				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
//			}
//			Expect(err).NotTo(HaveOccurred())
//		})
//
//		AfterEach(func() {
//			// TODO(user): Cleanup logic after each test, like removing the resource instance.
//			resource := &trustyaiv1alpha1.NemoGuardrails{}
//			err := k8sClient.Get(ctx, typeNamespacedName, resource)
//			Expect(err).NotTo(HaveOccurred())
//
//			By("Cleanup the specific resource instance NemoGuardrails")
//			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
//		})
//		It("should successfully reconcile the resource", func() {
//			By("Reconciling the created resource")
//			controllerReconciler := &NemoGuardrailsReconciler{
//				Client: k8sClient,
//				Scheme: k8sClient.Scheme(),
//			}
//
//			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
//				NamespacedName: typeNamespacedName,
//			})
//			Expect(err).NotTo(HaveOccurred())
//			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
//			// Example: If you expect a certain status condition after reconciliation, verify it here.
//		})
//	})
//})

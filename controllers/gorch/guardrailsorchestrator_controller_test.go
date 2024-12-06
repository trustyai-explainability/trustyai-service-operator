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

package gorch

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"

	// "golang.org/x/sys/unix"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
)

func createGuardrailsOrchestrator(ctx context.Context, namespace string) error {
	typedNamespacedName := types.NamespacedName{Name: orchestratorName, Namespace: namespace}
	err := k8sClient.Get(ctx, typedNamespacedName, &gorchv1alpha1.GuardrailsOrchestrator{})
	if err != nil && errors.IsNotFound(err) {
		gorch := &gorchv1alpha1.GuardrailsOrchestrator{
			ObjectMeta: metav1.ObjectMeta{
				Name:      typedNamespacedName.Name,
				Namespace: typedNamespacedName.Namespace,
			},
			Spec: gorchv1alpha1.GuardrailsOrchestratorSpec{
				Replicas: 1,
				Generator: gorchv1alpha1.GeneratorSpec{
					Provider: "nlp",
					Service: gorchv1alpha1.ServiceSpec{
						Hostname: "test",
						Port:     8085,
					},
				},
				Detectors: []gorchv1alpha1.DetectorSpec{
					{
						Type: "regex",
						Service: gorchv1alpha1.ServiceSpec{
							Hostname: "test",
							Port:     8000,
						},
						ChunkerName:      "whole_doc_chunker",
						DefaultThreshold: "0.5",
					},
				},
			},
		}
		err = k8sClient.Create(ctx, gorch)
	}
	return err
}

func deleteGuardrailsOrchestrator(ctx context.Context, namespace string) error {
	typedNamespacedName := types.NamespacedName{Name: orchestratorName, Namespace: namespace}
	err := k8sClient.Get(ctx, typedNamespacedName, &gorchv1alpha1.GuardrailsOrchestrator{})
	if err == nil {
		gorch := &gorchv1alpha1.GuardrailsOrchestrator{
			ObjectMeta: metav1.ObjectMeta{
				Name:      typedNamespacedName.Name,
				Namespace: typedNamespacedName.Namespace,
			},
		}
		err = k8sClient.Delete(ctx, gorch)
	}
	return err
}

func testCreateDeleteGuardrailsOrchestrator(namespaceName string) {
	It("Should sucessfully reconcile creating a custom resource for the GuardrailsOrchestrator", func() {
		By("Creating a custom resource for the GuardrailsOrchestrator")
		ctx := context.Background()
		typedNamespacedName := types.NamespacedName{Name: orchestratorName, Namespace: namespaceName}
		err := createGuardrailsOrchestrator(ctx, namespaceName)
		Expect(err).ToNot(HaveOccurred())

		By("Checking if the custom resource was successfully created")
		err = k8sClient.Get(ctx, typedNamespacedName, &gorchv1alpha1.GuardrailsOrchestrator{})
		Expect(err).ToNot(HaveOccurred())

		By("Reconciling the custom resource that was created")
		reconciler := &GuardrailsOrchestratorReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typedNamespacedName})
		Expect(err).ToNot(HaveOccurred())

		By("Checking if resources were successfully created in the reconcilation")
		Eventually(func() error {
			serviceAccount := &corev1.ServiceAccount{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName + "-serviceaccount", Namespace: namespaceName}, serviceAccount); err != nil {
				return err
			}

			deployment := &appsv1.Deployment{}
			if err = k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName, Namespace: namespaceName}, deployment); err != nil {
				return err
			}
			Expect(*deployment.Spec.Replicas).Should(Equal(int32(1)))
			Expect(deployment.Namespace).Should(Equal(namespaceName))
			Expect(deployment.Name).Should(Equal(orchestratorName))
			Expect(deployment.Labels["app"]).Should(Equal(orchestratorName))
			Expect(deployment.Spec.Template.Spec.Containers[0].Image).Should(Equal(defaultContainerImage))

			service := &corev1.Service{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName + "-service", Namespace: namespaceName}, service); err != nil {
				return err
			}
			Expect(service.Namespace).Should(Equal(namespaceName))

			route := &routev1.Route{}
			if err := routev1.AddToScheme(scheme.Scheme); err != nil {
				return err
			}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName + "-route", Namespace: namespaceName}, route); err != nil {
				return err
			}
			return nil
		}, time.Second*10, time.Millisecond*10).Should(Succeed())

		By("Deleting the custom resource for the GuardrailsOrchestrator")
		err = deleteGuardrailsOrchestrator(ctx, namespaceName)
		Expect(err).ToNot(HaveOccurred())

		By("Checking if resources were successfully deleted in the reconcilation")
		err = k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName + "-route", Namespace: namespaceName}, &routev1.Route{})
		Expect(err).ToNot((HaveOccurred()))
	})

}

var _ = Describe("GuardrailsOrchestrator Controller", func() {
	Context("GuardrailsOrchestrator Controller Test", func() {
		testCreateDeleteGuardrailsOrchestrator(namespaceName)
	})
})

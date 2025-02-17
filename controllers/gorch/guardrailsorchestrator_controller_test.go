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
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
)

func createGuardrailsOrchestrator(ctx context.Context, orchestratorConfigMap string) error {
	typedNamespacedName := types.NamespacedName{Name: orchestratorName, Namespace: namespaceName}
	err := k8sClient.Get(ctx, typedNamespacedName, &gorchv1alpha1.GuardrailsOrchestrator{})
	if err != nil && errors.IsNotFound(err) {
		gorch := &gorchv1alpha1.GuardrailsOrchestrator{
			ObjectMeta: metav1.ObjectMeta{
				Name:      typedNamespacedName.Name,
				Namespace: typedNamespacedName.Namespace,
			},
			Spec: gorchv1alpha1.GuardrailsOrchestratorSpec{
				Replicas:           1,
				OrchestratorConfig: &orchestratorConfigMap,
			},
		}
		err = k8sClient.Create(ctx, gorch)
	}
	return err
}

func createGuardrailsOrchestratorSidecar(ctx context.Context, orchestratorConfigMap string) error {
	typedNamespacedName := types.NamespacedName{Name: orchestratorName, Namespace: namespaceName}
	vllmGatewayConfigMap := "vllm-gateway-config"
	err := k8sClient.Get(ctx, typedNamespacedName, &gorchv1alpha1.GuardrailsOrchestrator{})
	if err != nil && errors.IsNotFound(err) {
		gorch := &gorchv1alpha1.GuardrailsOrchestrator{
			ObjectMeta: metav1.ObjectMeta{
				Name:      typedNamespacedName.Name,
				Namespace: typedNamespacedName.Namespace,
			},
			Spec: gorchv1alpha1.GuardrailsOrchestratorSpec{
				Replicas:           1,
				OrchestratorConfig: &orchestratorConfigMap,
				VLLMGatewayConfig:  &vllmGatewayConfigMap,
			},
		}
		err = k8sClient.Create(ctx, gorch)
	}
	return err
}

func createGuardrailsOrchestratorOtelExporter(ctx context.Context, orchestratorConfigMap string) error {
	typedNamespacedName := types.NamespacedName{Name: orchestratorName, Namespace: namespaceName}
	otelExporter := gorchv1alpha1.OtelExporter{
		Protocol:     "grpc",
		OTLPEndpoint: "localhost:4317",
		OTLPExport:   "traces",
	}
	err := k8sClient.Get(ctx, typedNamespacedName, &gorchv1alpha1.GuardrailsOrchestrator{})
	if err != nil && errors.IsNotFound(err) {
		gorch := &gorchv1alpha1.GuardrailsOrchestrator{
			ObjectMeta: metav1.ObjectMeta{
				Name:      typedNamespacedName.Name,
				Namespace: typedNamespacedName.Namespace,
			},
			Spec: gorchv1alpha1.GuardrailsOrchestratorSpec{
				Replicas:           1,
				OrchestratorConfig: &orchestratorConfigMap,
				OtelExporter:       otelExporter,
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
		err = doFinalizerOperationsForOrchestrator(ctx, gorch)
		if err != nil {
			return err
		}
		err = k8sClient.Delete(ctx, gorch)
		if err != nil {
			return err
		}
	}
	return err
}

func testCreateDeleteGuardrailsOrchestrator(namespaceName string) {
	It("Should sucessfully reconcile creating a custom resource for the GuardrailsOrchestrator", func() {
		By("Creating an Orchestrator configmap")
		configMap := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      orchestratorName + "-config",
				Namespace: namespaceName,
			},
		}
		err := k8sClient.Create(ctx, configMap)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a custom resource for the GuardrailsOrchestrator")
		ctx := context.Background()
		typedNamespacedName := types.NamespacedName{Name: orchestratorName, Namespace: namespaceName}
		err = createGuardrailsOrchestrator(ctx, configMap.Name)
		Expect(err).ToNot(HaveOccurred())

		By("Checking if the custom resource was successfully created")
		err = k8sClient.Get(ctx, typedNamespacedName, &gorchv1alpha1.GuardrailsOrchestrator{})
		Expect(err).ToNot(HaveOccurred())

		By("Creating the TrustyAI configmap for testing")
		configMap = &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      constants.ConfigMap,
				Namespace: namespaceName,
			},
			Data: map[string]string{
				orchestratorImageKey: "quay.io/trustyai/ta-guardrails-orchestrator:latest",
			},
		}
		err = k8sClient.Create(ctx, configMap)
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
			configMap := &corev1.ConfigMap{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: constants.ConfigMap, Namespace: namespaceName}, configMap); err != nil {
				return err
			}
			Expect(configMap.Name).Should(Equal(constants.ConfigMap))
			Expect(configMap.Namespace).Should(Equal(namespaceName))
			Expect(configMap.Data[orchestratorImageKey]).ShouldNot(BeEmpty())

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
			Expect(deployment.Spec.Template.Spec.Volumes[0].Name).Should(Equal(orchestratorName + "-config"))
			Expect(deployment.Spec.Template.Spec.Volumes[0].ConfigMap.Name).Should(Equal(orchestratorName + "-config"))
			Expect(deployment.Spec.Template.Spec.Containers[0].Image).Should(Equal("quay.io/trustyai/ta-guardrails-orchestrator:latest"))
			Expect(deployment.Spec.Template.Spec.Containers[0].VolumeMounts[0].Name).Should(Equal(orchestratorName + "-config"))

			service := &corev1.Service{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName + "-service", Namespace: namespaceName}, service); err != nil {
				return err
			}
			Expect(service.Namespace).Should(Equal(namespaceName))

			route := &routev1.Route{}
			if err := routev1.AddToScheme(scheme.Scheme); err != nil {
				return err
			}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName + "-http", Namespace: namespaceName}, route); err != nil {
				return err
			}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName + "-health", Namespace: namespaceName}, route); err != nil {
				return err
			}
			return nil
		}, time.Second*10, time.Millisecond*10).Should(Succeed())

		By("Deleting the custom resource for the GuardrailsOrchestrator")
		err = deleteGuardrailsOrchestrator(ctx, namespaceName)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting the TrustyAI configmap")
		err = k8sClient.Delete(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: constants.ConfigMap, Namespace: namespaceName}})
		Expect(err).ToNot(HaveOccurred())

		By("Deleting the orchestrator configmap")
		err = k8sClient.Delete(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: orchestratorName + "-config", Namespace: namespaceName}})
		Expect(err).ToNot(HaveOccurred())

		By("Reconciling the custom resource that was deleted")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typedNamespacedName})
		Expect(err).ToNot(HaveOccurred())
	})
}

func testCreateDeleteGuardrailsOrchestratorSidecar(namespaceName string) {
	It("Should sucessfully reconcile creating a custom resource for the GuardrailsOrchestrator", func() {
		By("Creating an Orchestrator configmap")
		configMap := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      orchestratorName + "-config",
				Namespace: namespaceName,
			},
		}
		err := k8sClient.Create(ctx, configMap)
		Expect(err).ToNot(HaveOccurred())

		By("Creating an VLLM Gateway configmap")
		configMap = &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      orchestratorName + "-vllm-config",
				Namespace: namespaceName,
			},
		}
		err = k8sClient.Create(ctx, configMap)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a custom resource for the GuardrailsOrchestrator")
		ctx := context.Background()
		typedNamespacedName := types.NamespacedName{Name: orchestratorName, Namespace: namespaceName}
		err = createGuardrailsOrchestratorSidecar(ctx, configMap.Name)
		Expect(err).ToNot(HaveOccurred())

		By("Checking if the custom resource was successfully created")
		err = k8sClient.Get(ctx, typedNamespacedName, &gorchv1alpha1.GuardrailsOrchestrator{})
		Expect(err).ToNot(HaveOccurred())

		By("Creating the TrustyAI configmap with sidecar images")
		configMap = &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      constants.ConfigMap,
				Namespace: namespaceName,
			},
			Data: map[string]string{
				orchestratorImageKey:  "quay.io/trustyai/ta-guardrails-orchestrator:latest",
				vllmGatewayImageKey:   "quay.io/trustyai/ta-guardrails-gateway:latest",
				regexDetectorImageKey: "quay.io/trustyai/ta-guardrails-regex:latest",
			},
		}
		err = k8sClient.Create(ctx, configMap)
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
			configMap := &corev1.ConfigMap{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: constants.ConfigMap, Namespace: namespaceName}, configMap); err != nil {
				return err
			}
			Expect(configMap.Namespace).Should(Equal(namespaceName))
			Expect(configMap.Name).Should(Equal(constants.ConfigMap))

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
			Expect(deployment.Spec.Template.Spec.Volumes[0].Name).Should(Equal(orchestratorName + "-config"))
			Expect(deployment.Spec.Template.Spec.Containers[0].Image).Should(Equal("quay.io/trustyai/ta-guardrails-orchestrator:latest"))
			Expect(deployment.Spec.Template.Spec.Containers[0].VolumeMounts[0].Name).Should(Equal(orchestratorName + "-config"))
			Expect(deployment.Spec.Template.Spec.Containers[1].Image).Should(Equal("quay.io/trustyai/ta-guardrails-gateway:latest"))
			Expect(deployment.Spec.Template.Spec.Containers[2].Image).Should(Equal("quay.io/trustyai/ta-guardrails-regex:latest"))

			service := &corev1.Service{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName + "-service", Namespace: namespaceName}, service); err != nil {
				return err
			}
			Expect(service.Namespace).Should(Equal(namespaceName))

			route := &routev1.Route{}
			if err := routev1.AddToScheme(scheme.Scheme); err != nil {
				return err
			}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName + "-http", Namespace: namespaceName}, route); err != nil {
				return err
			}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName + "-health", Namespace: namespaceName}, route); err != nil {
				return err
			}
			return nil
		}, time.Second*10, time.Millisecond*10).Should(Succeed())

		By("Deleting the custom resource for the GuardrailsOrchestrator")
		err = deleteGuardrailsOrchestrator(ctx, namespaceName)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting the orchestrator configmap")
		err = k8sClient.Delete(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: orchestratorName + "-config", Namespace: namespaceName}})
		Expect(err).ToNot(HaveOccurred())

		By("Deleting the TrustyAI configmap")
		err = k8sClient.Delete(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: constants.ConfigMap, Namespace: namespaceName}})
		Expect(err).ToNot(HaveOccurred())

		By("Reconciling the custom resource that was deleted")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typedNamespacedName})
		Expect(err).ToNot(HaveOccurred())
	})
}

func testCreateDeleteGuardrailsOrchestratorOtelExporter(namespaceName string) {
	It("Should sucessfully reconcile creating a custom resource for the GuardrailsOrchestrator", func() {
		By("Creating an Orchestrator configmap")
		configMap := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      orchestratorName + "-config",
				Namespace: namespaceName,
			},
		}
		err := k8sClient.Create(ctx, configMap)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a custom resource for the GuardrailsOrchestrator")
		ctx := context.Background()
		typedNamespacedName := types.NamespacedName{Name: orchestratorName, Namespace: namespaceName}
		err = createGuardrailsOrchestratorOtelExporter(ctx, configMap.Name)
		Expect(err).ToNot(HaveOccurred())

		By("Checking if the custom resource was successfully created")
		err = k8sClient.Get(ctx, typedNamespacedName, &gorchv1alpha1.GuardrailsOrchestrator{})
		Expect(err).ToNot(HaveOccurred())

		By("Creating the TrustyAI configmap with sidecar images")
		configMap = &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      constants.ConfigMap,
				Namespace: namespaceName,
			},
			Data: map[string]string{
				orchestratorImageKey: "quay.io/trustyai/ta-guardrails-orchestrator:latest",
			},
		}
		err = k8sClient.Create(ctx, configMap)
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
			configMap := &corev1.ConfigMap{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: constants.ConfigMap, Namespace: namespaceName}, configMap); err != nil {
				return err
			}
			Expect(configMap.Namespace).Should(Equal(namespaceName))
			Expect(configMap.Name).Should(Equal(constants.ConfigMap))

			serviceAccount := &corev1.ServiceAccount{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName + "-serviceaccount", Namespace: namespaceName}, serviceAccount); err != nil {
				return err
			}

			deployment := &appsv1.Deployment{}
			if err = k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName, Namespace: namespaceName}, deployment); err != nil {
				return err
			}
			var container *corev1.Container
			var envVar *corev1.EnvVar
			Expect(*deployment.Spec.Replicas).Should(Equal(int32(1)))
			Expect(deployment.Namespace).Should(Equal(namespaceName))
			Expect(deployment.Name).Should(Equal(orchestratorName))
			Expect(deployment.Labels["app"]).Should(Equal(orchestratorName))
			Expect(deployment.Spec.Template.Spec.Volumes[0].Name).Should(Equal(orchestratorName + "-config"))
			container = getContainers(orchestratorName, deployment.Spec.Template.Spec.Containers)
			Expect(container.Image).Should(Equal("quay.io/trustyai/ta-guardrails-orchestrator:latest"))
			Expect(container.VolumeMounts[0].Name).Should(Equal(orchestratorName + "-config"))
			envVar = getEnvVar("OTEL_EXPORTER_OTLP_PROTOCOL", container.Env)
			Expect(envVar).ShouldNot(BeNil())
			Expect(envVar.Value).To(Equal("grpc"))
			envVar = getEnvVar("OTEL_EXPORTER_OTLP_ENDPOINT", container.Env)
			Expect(envVar).ShouldNot(BeNil())
			Expect(envVar.Value).To(Equal("localhost:4317"))
			envVar = getEnvVar("OTLP_EXPORT", container.Env)
			Expect(envVar).ShouldNot(BeNil())
			Expect(envVar.Value).To(Equal("traces"))

			service := &corev1.Service{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName + "-service", Namespace: namespaceName}, service); err != nil {
				return err
			}
			Expect(service.Namespace).Should(Equal(namespaceName))

			route := &routev1.Route{}
			if err := routev1.AddToScheme(scheme.Scheme); err != nil {
				return err
			}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName + "-http", Namespace: namespaceName}, route); err != nil {
				return err
			}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName + "-health", Namespace: namespaceName}, route); err != nil {
				return err
			}
			return nil
		}, time.Second*10, time.Millisecond*10).Should(Succeed())

		By("Deleting the custom resource for the GuardrailsOrchestrator")
		err = deleteGuardrailsOrchestrator(ctx, namespaceName)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting the orchestrator configmap")
		err = k8sClient.Delete(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: orchestratorName + "-config", Namespace: namespaceName}})
		Expect(err).ToNot(HaveOccurred())

		By("Deleting the TrustyAI configmap")
		err = k8sClient.Delete(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: constants.ConfigMap, Namespace: namespaceName}})
		Expect(err).ToNot(HaveOccurred())

		By("Reconciling the custom resource that was deleted")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typedNamespacedName})
		Expect(err).ToNot(HaveOccurred())
	})
}

var _ = Describe("GuardrailsOrchestrator Controller", func() {
	Context("GuardrailsOrchestrator Controller Test", func() {
		testCreateDeleteGuardrailsOrchestrator(namespaceName)
		testCreateDeleteGuardrailsOrchestratorSidecar(namespaceName)
		testCreateDeleteGuardrailsOrchestratorOtelExporter(namespaceName)
	})
})

func testMultipleOrchestratorsInSameNamespace(namespaceName string) {
	It("Should successfully create and manage two GuardrailsOrchestrator instances in the same namespace", func() {
		ctx := context.Background()

		configMap1 := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      orchestratorName + "-config-1",
				Namespace: namespaceName,
			},
		}
		err := k8sClient.Create(ctx, configMap1)
		Expect(err).ToNot(HaveOccurred())

		configMap2 := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      orchestratorName + "-config-2",
				Namespace: namespaceName,
			},
		}
		err = k8sClient.Create(ctx, configMap2)
		Expect(err).ToNot(HaveOccurred())

		trustyAIConfigMap := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      constants.ConfigMap,
				Namespace: namespaceName,
			},
			Data: map[string]string{
				orchestratorImageKey: "quay.io/trustyai/ta-guardrails-orchestrator:latest",
			},
		}
		err = k8sClient.Create(ctx, trustyAIConfigMap)
		Expect(err).ToNot(HaveOccurred())

		orchestrator1 := &gorchv1alpha1.GuardrailsOrchestrator{
			ObjectMeta: metav1.ObjectMeta{
				Name:      orchestratorName + "-1",
				Namespace: namespaceName,
			},
			Spec: gorchv1alpha1.GuardrailsOrchestratorSpec{
				Replicas:           1,
				OrchestratorConfig: &configMap1.Name,
			},
		}
		err = k8sClient.Create(ctx, orchestrator1)
		Expect(err).ToNot(HaveOccurred())

		orchestrator2 := &gorchv1alpha1.GuardrailsOrchestrator{
			ObjectMeta: metav1.ObjectMeta{
				Name:      orchestratorName + "-2",
				Namespace: namespaceName,
			},
			Spec: gorchv1alpha1.GuardrailsOrchestratorSpec{
				Replicas:           1,
				OrchestratorConfig: &configMap2.Name,
			},
		}
		err = k8sClient.Create(ctx, orchestrator2)
		Expect(err).ToNot(HaveOccurred())

		reconciler := &GuardrailsOrchestratorReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}

		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: orchestrator1.Name, Namespace: namespaceName},
		})
		Expect(err).ToNot(HaveOccurred())

		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: orchestrator2.Name, Namespace: namespaceName},
		})
		Expect(err).ToNot(HaveOccurred())

		Eventually(func() error {
			deployment1 := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestrator1.Name, Namespace: namespaceName}, deployment1); err != nil {
				return err
			}
			Expect(deployment1.Spec.Template.Spec.Containers[0].Image).Should(Equal("quay.io/trustyai/ta-guardrails-orchestrator:latest"))

			service1 := &corev1.Service{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestrator1.Name + "-service", Namespace: namespaceName}, service1); err != nil {
				return err
			}

			deployment2 := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestrator2.Name, Namespace: namespaceName}, deployment2); err != nil {
				return err
			}
			Expect(deployment2.Spec.Template.Spec.Containers[0].Image).Should(Equal("quay.io/trustyai/ta-guardrails-orchestrator:latest"))

			service2 := &corev1.Service{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestrator2.Name + "-service", Namespace: namespaceName}, service2); err != nil {
				return err
			}

			return nil
		}, time.Second*10, time.Millisecond*10).Should(Succeed())

		By("Deleting the first GuardrailsOrchestrator instance")
		err = k8sClient.Delete(ctx, orchestrator1)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting the second GuardrailsOrchestrator instance")
		err = k8sClient.Delete(ctx, orchestrator2)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting the config maps")
		err = k8sClient.Delete(ctx, configMap1)
		Expect(err).ToNot(HaveOccurred())
		err = k8sClient.Delete(ctx, configMap2)
		Expect(err).ToNot(HaveOccurred())
		err = k8sClient.Delete(ctx, trustyAIConfigMap)
		Expect(err).ToNot(HaveOccurred())
	})
}

func testMultipleOrchestratorsInDifferentNamespaces() {
	It("Should successfully create and manage GuardrailsOrchestrator instances in different namespaces", func() {
		ctx := context.Background()
		namespace1 := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-namespace-1",
			},
		}
		namespace2 := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-namespace-2",
			},
		}

		err := k8sClient.Create(ctx, namespace1)
		Expect(err).ToNot(HaveOccurred())
		err = k8sClient.Create(ctx, namespace2)
		Expect(err).ToNot(HaveOccurred())

		configMap1 := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      orchestratorName + "-config",
				Namespace: namespace1.Name,
			},
		}
		err = k8sClient.Create(ctx, configMap1)
		Expect(err).ToNot(HaveOccurred())

		configMap2 := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      orchestratorName + "-config",
				Namespace: namespace2.Name,
			},
		}
		err = k8sClient.Create(ctx, configMap2)
		Expect(err).ToNot(HaveOccurred())

		trustyAIConfigMap1 := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      constants.ConfigMap,
				Namespace: namespace1.Name,
			},
			Data: map[string]string{
				orchestratorImageKey: "quay.io/trustyai/ta-guardrails-orchestrator:latest",
			},
		}
		err = k8sClient.Create(ctx, trustyAIConfigMap1)
		Expect(err).ToNot(HaveOccurred())

		trustyAIConfigMap2 := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      constants.ConfigMap,
				Namespace: namespace2.Name,
			},
			Data: map[string]string{
				orchestratorImageKey: "quay.io/trustyai/ta-guardrails-orchestrator:latest",
			},
		}
		err = k8sClient.Create(ctx, trustyAIConfigMap2)
		Expect(err).ToNot(HaveOccurred())

		orchestrator1 := &gorchv1alpha1.GuardrailsOrchestrator{
			ObjectMeta: metav1.ObjectMeta{
				Name:      orchestratorName,
				Namespace: namespace1.Name,
			},
			Spec: gorchv1alpha1.GuardrailsOrchestratorSpec{
				Replicas:           1,
				OrchestratorConfig: &configMap1.Name,
			},
		}
		err = k8sClient.Create(ctx, orchestrator1)
		Expect(err).ToNot(HaveOccurred())

		orchestrator2 := &gorchv1alpha1.GuardrailsOrchestrator{
			ObjectMeta: metav1.ObjectMeta{
				Name:      orchestratorName,
				Namespace: namespace2.Name,
			},
			Spec: gorchv1alpha1.GuardrailsOrchestratorSpec{
				Replicas:           1,
				OrchestratorConfig: &configMap2.Name,
			},
		}
		err = k8sClient.Create(ctx, orchestrator2)
		Expect(err).ToNot(HaveOccurred())

		reconciler := &GuardrailsOrchestratorReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}

		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: orchestrator1.Name, Namespace: namespace1.Name},
		})
		Expect(err).ToNot(HaveOccurred())

		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: orchestrator2.Name, Namespace: namespace2.Name},
		})
		Expect(err).ToNot(HaveOccurred())

		Eventually(func() error {
			deployment1 := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName, Namespace: namespace1.Name}, deployment1); err != nil {
				return err
			}
			Expect(deployment1.Spec.Template.Spec.Containers[0].Image).Should(Equal("quay.io/trustyai/ta-guardrails-orchestrator:latest"))

			service1 := &corev1.Service{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName + "-service", Namespace: namespace1.Name}, service1); err != nil {
				return err
			}

			deployment2 := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName, Namespace: namespace2.Name}, deployment2); err != nil {
				return err
			}
			Expect(deployment2.Spec.Template.Spec.Containers[0].Image).Should(Equal("quay.io/trustyai/ta-guardrails-orchestrator:latest"))

			service2 := &corev1.Service{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName + "-service", Namespace: namespace2.Name}, service2); err != nil {
				return err
			}

			return nil
		}, time.Second*10, time.Millisecond*10).Should(Succeed())

		// Cleanup
		By("Deleting the GuardrailsOrchestrator instances")
		err = k8sClient.Delete(ctx, orchestrator1)
		Expect(err).ToNot(HaveOccurred())
		err = k8sClient.Delete(ctx, orchestrator2)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting the config maps")
		err = k8sClient.Delete(ctx, configMap1)
		Expect(err).ToNot(HaveOccurred())
		err = k8sClient.Delete(ctx, configMap2)
		Expect(err).ToNot(HaveOccurred())
		err = k8sClient.Delete(ctx, trustyAIConfigMap1)
		Expect(err).ToNot(HaveOccurred())
		err = k8sClient.Delete(ctx, trustyAIConfigMap2)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting the test namespaces")
		err = k8sClient.Delete(ctx, namespace1)
		Expect(err).ToNot(HaveOccurred())
		err = k8sClient.Delete(ctx, namespace2)
		Expect(err).ToNot(HaveOccurred())
	})
}

var _ = Describe("Multiple GuardrailsOrchestrator Tests", func() {
	Context("Multiple GuardrailsOrchestrator Controller Test", func() {
		testMultipleOrchestratorsInSameNamespace(namespaceName)
		testMultipleOrchestratorsInDifferentNamespaces()
	})
})

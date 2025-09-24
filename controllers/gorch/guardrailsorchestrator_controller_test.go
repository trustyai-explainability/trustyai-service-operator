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

func createGuardrailsOrchestrator(ctx context.Context, orchestratorConfigMap string, name string, namespace string) error {
	typedNamespacedName := types.NamespacedName{Name: name, Namespace: namespace}
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

func createGuardrailsOrchestratorSidecar(ctx context.Context, orchestratorConfigMap string, guardrailsGatewayConfigMap string) error {
	typedNamespacedName := types.NamespacedName{Name: orchestratorName, Namespace: namespaceName}
	err := k8sClient.Get(ctx, typedNamespacedName, &gorchv1alpha1.GuardrailsOrchestrator{})
	if err != nil && errors.IsNotFound(err) {
		gorch := &gorchv1alpha1.GuardrailsOrchestrator{
			ObjectMeta: metav1.ObjectMeta{
				Name:      typedNamespacedName.Name,
				Namespace: typedNamespacedName.Namespace,
			},
			Spec: gorchv1alpha1.GuardrailsOrchestratorSpec{
				Replicas:                1,
				OrchestratorConfig:      &orchestratorConfigMap,
				SidecarGatewayConfig:    &guardrailsGatewayConfigMap,
				EnableBuiltInDetectors:  true,
				EnableGuardrailsGateway: true,
			},
		}
		err = k8sClient.Create(ctx, gorch)
	}
	return err
}

func createGuardrailsOrchestratorOtelExporter(ctx context.Context, orchestratorConfigMap string) error {
	typedNamespacedName := types.NamespacedName{Name: orchestratorName, Namespace: namespaceName}
	otelExporter := gorchv1alpha1.OTelExporter{
		OTLPProtocol:        "grpc",
		OTLPTracesEndpoint:  "localhost:4317",
		OTLPMetricsEndpoint: "localhost:4318",
		EnableTraces:        true,
		EnableMetrics:       true,
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
				OTelExporter:       otelExporter,
			},
		}
		err = k8sClient.Create(ctx, gorch)
	}
	return err
}

func deleteGuardrailsOrchestrator(ctx context.Context, name string, namespace string) error {
	typedNamespacedName := types.NamespacedName{Name: name, Namespace: namespace}
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

		By("Creating a custom resource for the GuardrailsOrchestrator")
		ctx := context.Background()
		typedNamespacedName := types.NamespacedName{Name: orchestratorName, Namespace: namespaceName}
		err := createGuardrailsOrchestrator(ctx, orchestratorName+"-config", orchestratorName, namespaceName)
		Expect(err).ToNot(HaveOccurred())

		By("Checking if the custom resource was successfully created")
		err = k8sClient.Get(ctx, typedNamespacedName, &gorchv1alpha1.GuardrailsOrchestrator{})
		Expect(err).ToNot(HaveOccurred())

		By("Reconciling the custom resource that was created")
		reconciler := &GuardrailsOrchestratorReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			Namespace: namespaceName,
		}

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typedNamespacedName})
		Expect(err).ToNot(HaveOccurred())

		By("Checking if resources were successfully created in the reconciliation")
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
			Expect((service.Spec.Ports[0].Name)).Should(Equal("https"))
			Expect((service.Spec.Ports[0].Port)).Should(Equal(int32(8032)))

			route := &routev1.Route{}
			if err := routev1.AddToScheme(scheme.Scheme); err != nil {
				return err
			}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName, Namespace: namespaceName}, route); err != nil {
				return err
			}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName + "-health", Namespace: namespaceName}, route); err != nil {
				return err
			}
			return nil
		}, time.Second*10, time.Millisecond*10).Should(Succeed())

		By("Deleting the custom resource for the GuardrailsOrchestrator")
		err = deleteGuardrailsOrchestrator(ctx, orchestratorName, namespaceName)
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
		By("Creating an guardrails sidecar gateway configmap")
		configMap := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      orchestratorName + "-sidecar-gateway-config",
				Namespace: namespaceName,
			},
		}
		err := k8sClient.Create(ctx, configMap)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a custom resource for the GuardrailsOrchestrator")
		ctx := context.Background()
		typedNamespacedName := types.NamespacedName{Name: orchestratorName, Namespace: namespaceName}
		err = createGuardrailsOrchestratorSidecar(ctx, orchestratorName+"-config", configMap.Name)
		Expect(err).ToNot(HaveOccurred())

		By("Checking if the custom resource was successfully created")
		err = k8sClient.Get(ctx, typedNamespacedName, &gorchv1alpha1.GuardrailsOrchestrator{})
		Expect(err).ToNot(HaveOccurred())

		By("Reconciling the custom resource that was created")
		reconciler := &GuardrailsOrchestratorReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			Namespace: namespaceName,
		}

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typedNamespacedName})
		Expect(err).ToNot(HaveOccurred())

		By("Checking if resources were successfully created in the reconciliation")
		Eventually(func() error {
			configMap := &corev1.ConfigMap{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName + "-config", Namespace: namespaceName}, configMap); err != nil {
				return err
			}
			Expect(configMap.Namespace).Should(Equal(namespaceName))
			Expect(configMap.Name).Should(Equal(orchestratorName + "-config"))

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
			Expect(service.Spec.Ports[0].Name).Should(Equal("gateway"))
			Expect(service.Spec.Ports[0].Port).Should(Equal(int32(8090)))

			route := &routev1.Route{}
			if err := routev1.AddToScheme(scheme.Scheme); err != nil {
				return err
			}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName, Namespace: namespaceName}, route); err != nil {
				return err
			}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName + "-health", Namespace: namespaceName}, route); err != nil {
				return err
			}
			return nil
		}, time.Second*10, time.Millisecond*10).Should(Succeed())

		By("Deleting the custom resource for the GuardrailsOrchestrator")
		err = deleteGuardrailsOrchestrator(ctx, orchestratorName, namespaceName)
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

		By("Creating a custom resource for the GuardrailsOrchestrator")
		ctx := context.Background()
		typedNamespacedName := types.NamespacedName{Name: orchestratorName, Namespace: namespaceName}
		err := createGuardrailsOrchestratorOtelExporter(ctx, orchestratorName+"-config")
		Expect(err).ToNot(HaveOccurred())

		By("Checking if the custom resource was successfully created")
		err = k8sClient.Get(ctx, typedNamespacedName, &gorchv1alpha1.GuardrailsOrchestrator{})
		Expect(err).ToNot(HaveOccurred())

		By("Reconciling the custom resource that was created")
		reconciler := &GuardrailsOrchestratorReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			Namespace: namespaceName,
		}

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typedNamespacedName})
		Expect(err).ToNot(HaveOccurred())

		By("Checking if resources were successfully created in the reconciliation")
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
			envVar = getEnvVar("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", container.Env)
			Expect(envVar).ShouldNot(BeNil())
			Expect(envVar.Value).To(Equal("localhost:4317"))
			envVar = getEnvVar("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", container.Env)
			Expect(envVar).ShouldNot(BeNil())
			Expect(envVar.Value).To(Equal("localhost:4318"))
			envVar = getEnvVar("OTLP_EXPORT", container.Env)
			Expect(envVar).ShouldNot(BeNil())
			Expect(envVar.Value).To(Equal("traces,metrics"))

			service := &corev1.Service{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName + "-service", Namespace: namespaceName}, service); err != nil {
				return err
			}
			Expect(service.Namespace).Should(Equal(namespaceName))

			route := &routev1.Route{}
			if err := routev1.AddToScheme(scheme.Scheme); err != nil {
				return err
			}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName, Namespace: namespaceName}, route); err != nil {
				return err
			}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName + "-health", Namespace: namespaceName}, route); err != nil {
				return err
			}
			return nil
		}, time.Second*10, time.Millisecond*10).Should(Succeed())

		By("Deleting the custom resource for the GuardrailsOrchestrator")
		err = deleteGuardrailsOrchestrator(ctx, orchestratorName, namespaceName)
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

func testCreateTwoGuardrailsOrchestratorsInSameNamespace(namespaceName string) {
	It("Should successfully reconcile two custom resources for the GuardrailsOrchestrator in the same namespace", func() {
		By("Creating the first custom resource for the GuardrailsOrchestrator")
		ctx := context.Background()
		firstOrchestratorName := "first-orchestrator"
		firstConfigMapName := firstOrchestratorName + "-config"
		firstTypedNamespacedName := types.NamespacedName{Name: firstOrchestratorName, Namespace: namespaceName}

		firstOrchConfig := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      firstConfigMapName,
				Namespace: namespaceName,
			},
		}
		err := k8sClient.Create(ctx, firstOrchConfig)
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).ToNot(HaveOccurred())
		}

		err = createGuardrailsOrchestrator(ctx, firstConfigMapName, firstOrchestratorName, namespaceName)
		Expect(err).ToNot(HaveOccurred())

		By("Checking if the first custom resource was successfully created")
		err = k8sClient.Get(ctx, firstTypedNamespacedName, &gorchv1alpha1.GuardrailsOrchestrator{})
		Expect(err).ToNot(HaveOccurred())

		By("Creating the second custom resource for the GuardrailsOrchestrator")
		secondOrchestratorName := "second-orchestrator"
		secondConfigMapName := secondOrchestratorName + "-config"
		secondTypedNamespacedName := types.NamespacedName{Name: secondOrchestratorName, Namespace: namespaceName}

		secondOrchConfig := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      secondConfigMapName,
				Namespace: namespaceName,
			},
		}
		err = k8sClient.Create(ctx, secondOrchConfig)
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).ToNot(HaveOccurred())
		}

		err = createGuardrailsOrchestrator(ctx, secondConfigMapName, secondOrchestratorName, namespaceName)
		Expect(err).ToNot(HaveOccurred())

		By("Checking if the second custom resource was successfully created")
		err = k8sClient.Get(ctx, secondTypedNamespacedName, &gorchv1alpha1.GuardrailsOrchestrator{})
		Expect(err).ToNot(HaveOccurred())

		By("Reconciling the first custom resource")
		reconciler := &GuardrailsOrchestratorReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			Namespace: namespaceName,
		}

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: firstTypedNamespacedName})
		Expect(err).ToNot(HaveOccurred())

		By("Reconciling the second custom resource")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: secondTypedNamespacedName})
		Expect(err).ToNot(HaveOccurred())

		By("Checking if resources for the first orchestrator were successfully created")
		Eventually(func() error {
			firstDeployment := &appsv1.Deployment{}
			if err = k8sClient.Get(ctx, types.NamespacedName{Name: firstOrchestratorName, Namespace: namespaceName}, firstDeployment); err != nil {
				return err
			}
			Expect(firstDeployment.Name).Should(Equal(firstOrchestratorName))
			Expect(firstDeployment.Namespace).Should(Equal(namespaceName))

			firstService := &corev1.Service{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: firstOrchestratorName + "-service", Namespace: namespaceName}, firstService); err != nil {
				return err
			}
			Expect(firstService.Name).Should(Equal(firstOrchestratorName + "-service"))

			return nil
		}, time.Second*10, time.Millisecond*250).Should(Succeed())

		By("Checking if resources for the second orchestrator were successfully created")
		Eventually(func() error {
			secondDeployment := &appsv1.Deployment{}
			if err = k8sClient.Get(ctx, types.NamespacedName{Name: secondOrchestratorName, Namespace: namespaceName}, secondDeployment); err != nil {
				return err
			}
			Expect(secondDeployment.Name).Should(Equal(secondOrchestratorName))
			Expect(secondDeployment.Namespace).Should(Equal(namespaceName))

			secondService := &corev1.Service{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: secondOrchestratorName + "-service", Namespace: namespaceName}, secondService); err != nil {
				return err
			}
			Expect(secondService.Name).Should(Equal(secondOrchestratorName + "-service"))

			return nil
		}, time.Second*10, time.Millisecond*250).Should(Succeed())

		By("Verifying routes for both orchestrators")
		if err := routev1.AddToScheme(scheme.Scheme); err != nil {
			Expect(err).ToNot(HaveOccurred())
		}

		Eventually(func() error {
			route1 := &routev1.Route{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: firstOrchestratorName, Namespace: namespaceName}, route1); err != nil {
				return err
			}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: firstOrchestratorName + "-health", Namespace: namespaceName}, route1); err != nil {
				return err
			}

			route2 := &routev1.Route{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: secondOrchestratorName, Namespace: namespaceName}, route2); err != nil {
				return err
			}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: secondOrchestratorName + "-health", Namespace: namespaceName}, route2); err != nil {
				return err
			}
			return nil
		}, time.Second*10, time.Millisecond*250).Should(Succeed())

		By("Cleaning up the first custom resource and its resources")
		err = deleteGuardrailsOrchestrator(ctx, firstOrchestratorName, namespaceName)
		Expect(err).ToNot(HaveOccurred())

		By("Cleaning up the second custom resource and its resources")
		err = deleteGuardrailsOrchestrator(ctx, secondOrchestratorName, namespaceName)
		Expect(err).ToNot(HaveOccurred())

		By("Deleting the first orchestrator configmap")
		err = k8sClient.Delete(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: firstConfigMapName, Namespace: namespaceName}})
		Expect(err).ToNot(HaveOccurred())

		By("Deleting the second orchestrator configmap")
		err = k8sClient.Delete(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: secondConfigMapName, Namespace: namespaceName}})
		Expect(err).ToNot(HaveOccurred())

		By("Deleting the TrustyAI configmap")
		err = k8sClient.Delete(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: constants.ConfigMap, Namespace: namespaceName}})
		Expect(err).ToNot(HaveOccurred())
	})
}

func testCreateTwoGuardrailsOrchestratorsInDifferentNamespaces(firstNamespace string, secondNamespace string) {
	It("Should successfully reconcile two custom resources for the GuardrailsOrchestrator in different namespaces", func() {
		By("Creating a second namespace")
		secondNamespaceObj := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: secondNamespace,
			},
		}
		err := k8sClient.Create(context.Background(), secondNamespaceObj)
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).ToNot(HaveOccurred())
		}

		By("Creating TrustyAI ConfigMap in both namespaces")
		ctx := context.Background()

		// Create TrustyAI ConfigMap in first namespace (if not exists)
		firstTrustyAIConfigMap := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      constants.ConfigMap,
				Namespace: firstNamespace,
			},
			Data: map[string]string{
				orchestratorImageKey: "quay.io/trustyai/ta-guardrails-orchestrator:latest",
				gatewayImageKey:      "quay.io/trustyai/ta-guardrails-gateway:latest",
				detectorImageKey:     "quay.io/trustyai/ta-guardrails-regex:latest",
			},
		}
		err = k8sClient.Create(ctx, firstTrustyAIConfigMap)
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).ToNot(HaveOccurred())
		}

		secondTrustyAIConfigMap := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      constants.ConfigMap,
				Namespace: secondNamespace,
			},
			Data: map[string]string{
				orchestratorImageKey: "quay.io/trustyai/ta-guardrails-orchestrator:latest",
				gatewayImageKey:      "quay.io/trustyai/ta-guardrails-gateway:latest",
				detectorImageKey:     "quay.io/trustyai/ta-guardrails-regex:latest",
			},
		}
		err = k8sClient.Create(ctx, secondTrustyAIConfigMap)
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).ToNot(HaveOccurred())
		}

		By("Creating the first orchestrator in the first namespace")
		firstOrchConfigName := orchestratorName + "-config-ns1"

		firstOrchConfig := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      firstOrchConfigName,
				Namespace: firstNamespace,
			},
		}
		err = k8sClient.Create(ctx, firstOrchConfig)
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).ToNot(HaveOccurred())
		}

		typedNamespacedName1 := types.NamespacedName{Name: orchestratorName, Namespace: firstNamespace}
		err = createGuardrailsOrchestrator(ctx, firstOrchConfigName, orchestratorName, firstNamespace)
		Expect(err).ToNot(HaveOccurred())

		By("Creating the second orchestrator in the second namespace")
		secondOrchConfigName := orchestratorName + "-config-ns2"

		secondOrchConfig := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      secondOrchConfigName,
				Namespace: secondNamespace,
			},
		}
		err = k8sClient.Create(ctx, secondOrchConfig)
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).ToNot(HaveOccurred())
		}

		typedNamespacedName2 := types.NamespacedName{Name: orchestratorName, Namespace: secondNamespace}
		err = createGuardrailsOrchestrator(ctx, secondOrchConfigName, orchestratorName, secondNamespace)
		Expect(err).ToNot(HaveOccurred())

		By("Reconciling the first orchestrator in namespace 1")
		firstReconciler := &GuardrailsOrchestratorReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			Namespace: firstNamespace,
		}
		_, err = firstReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typedNamespacedName1})
		Expect(err).ToNot(HaveOccurred())

		By("Reconciling the second orchestrator in namespace 2")
		secondReconciler := &GuardrailsOrchestratorReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			Namespace: secondNamespace,
		}
		_, err = secondReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typedNamespacedName2})
		Expect(err).ToNot(HaveOccurred())

		By("Verifying the first orchestrator's resources in namespace 1")
		Eventually(func() error {
			firstDeployment := &appsv1.Deployment{}
			if err = k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName, Namespace: firstNamespace}, firstDeployment); err != nil {
				return err
			}
			Expect(firstDeployment.Namespace).Should(Equal(firstNamespace))

			firstService := &corev1.Service{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName + "-service", Namespace: firstNamespace}, firstService); err != nil {
				return err
			}
			Expect(firstService.Namespace).Should(Equal(firstNamespace))

			return nil
		}, time.Second*10, time.Millisecond*250).Should(Succeed())

		By("Verifying the second orchestrator's resources in namespace 2")
		Eventually(func() error {
			secondDeployment := &appsv1.Deployment{}
			if err = k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName, Namespace: secondNamespace}, secondDeployment); err != nil {
				return err
			}
			Expect(secondDeployment.Namespace).Should(Equal(secondNamespace))

			secondService := &corev1.Service{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestratorName + "-service", Namespace: secondNamespace}, secondService); err != nil {
				return err
			}
			Expect(secondService.Namespace).Should(Equal(secondNamespace))

			return nil
		}, time.Second*10, time.Millisecond*250).Should(Succeed())

		By("Cleaning up orchestrator in namespace 1")
		err = deleteGuardrailsOrchestrator(ctx, orchestratorName, firstNamespace)
		Expect(err).ToNot(HaveOccurred())

		By("Cleaning up orchestrator in namespace 2")
		err = deleteGuardrailsOrchestrator(ctx, orchestratorName, secondNamespace)
		Expect(err).ToNot(HaveOccurred())

		By("Cleaning up ConfigMaps in both namespaces")
		err = k8sClient.Delete(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: firstOrchConfigName, Namespace: firstNamespace}})
		Expect(err).ToNot(HaveOccurred())

		err = k8sClient.Delete(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: secondOrchConfigName, Namespace: secondNamespace}})
		Expect(err).ToNot(HaveOccurred())

		err = k8sClient.Delete(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: constants.ConfigMap, Namespace: firstNamespace}})
		Expect(err).ToNot(HaveOccurred())

		err = k8sClient.Delete(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: constants.ConfigMap, Namespace: secondNamespace}})
		Expect(err).ToNot(HaveOccurred())
	})
}

var _ = Describe("GuardrailsOrchestrator Controller", func() {
	var ctx = context.Background()
	BeforeEach(func() {
		By("Creating the operator's ConfigMap")
		configMap := &corev1.ConfigMap{
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
				gatewayImageKey:      "quay.io/trustyai/ta-guardrails-gateway:latest",
				detectorImageKey:     "quay.io/trustyai/ta-guardrails-regex:latest",
			},
		}
		err := k8sClient.Create(ctx, configMap)

		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).ToNot(HaveOccurred())
		}

		orchConfig := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      orchestratorName + "-config",
				Namespace: namespaceName,
			},
			Data: map[string]string{
				"config.yaml": `
					chat_generation:
				  		service:
						hostname: llm-predictor.guardrails-test.svc.cluster.local
						port: 8032
				  	detectors:
						regex:
							type: text_contents
							service:
								hostname: "127.0.0.1"
								port: 8080
							chunker_id: whole_doc_chunker
							default_threshold: 0.5
				`,
			},
		}
		err = k8sClient.Create(ctx, orchConfig)
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).ToNot(HaveOccurred())
		}

	})

	Context("GuardrailsOrchestrator Controller Test", func() {
		testCreateDeleteGuardrailsOrchestrator(namespaceName)
		testCreateDeleteGuardrailsOrchestratorSidecar(namespaceName)
		testCreateDeleteGuardrailsOrchestratorOtelExporter(namespaceName)
		testCreateTwoGuardrailsOrchestratorsInSameNamespace(namespaceName)
		testCreateTwoGuardrailsOrchestratorsInDifferentNamespaces(namespaceName, secondNamespaceName)
	})
})

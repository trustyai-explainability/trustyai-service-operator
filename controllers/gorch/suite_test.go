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
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	routev1 "github.com/openshift/api/route/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var ctx context.Context
var cancel context.CancelFunc

const namespaceName = "test"
const secondNamespaceName = "second-test"

func getContainers(containerName string, containers []corev1.Container) *corev1.Container {
	for _, container := range containers {
		if container.Name == containerName {
			return &container
		}
	}
	return nil
}
func getEnvVar(envVarName string, envVars []corev1.EnvVar) *corev1.EnvVar {
	for _, envVar := range envVars {
		if envVar.Name == envVarName {
			return &envVar
		}
	}
	return nil
}

func deleteServiceAccount(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) (err error) {
	obj := client.ObjectKey{Name: orchestrator.Name + "-serviceaccount", Namespace: orchestrator.Namespace}
	orig := &corev1.ServiceAccount{}
	log := log.FromContext(ctx).WithValues("GuardrailsOrchestratorReconciler.deleteServiceAccount", obj)
	err = k8sClient.Get(ctx, obj, orig)
	if errors.IsNotFound(err) {
		return nil
	} else if err != nil {
		log.Error(err, "Get", "ServiceAccount", obj)
		return err
	}

	err = k8sClient.Delete(ctx, orig, &client.DeleteOptions{})
	if err != nil && errors.IsNotFound(err) {
		log.Error(err, "Delete", "ServiceAccount", obj)
		return err
	}
	log.Info("Delete", "ServiceAccount", obj)
	return nil
}

func deleteDeployment(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) (err error) {
	obj := client.ObjectKey{Name: orchestrator.Name, Namespace: orchestrator.Namespace}
	orig := &appsv1.Deployment{}
	log := log.FromContext(ctx).WithValues("GuardrailsOrchestratorReconciler.deleteDeployment", obj)
	err = k8sClient.Get(ctx, obj, orig)
	if errors.IsNotFound(err) {
		return nil
	} else if err != nil {
		log.Error(err, "Get", "Deployment", obj)
		return err
	}

	err = k8sClient.Delete(ctx, orig, &client.DeleteOptions{})
	if err != nil && errors.IsNotFound(err) {
		log.Error(err, "Delete", "Deployment", obj)
		return err
	}
	log.Info("Delete", "Deployment", obj)
	return nil
}

func deleteService(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) (err error) {
	obj := client.ObjectKey{Name: orchestrator.Name + "-service", Namespace: orchestrator.Namespace}
	orig := &corev1.Service{}
	log := log.FromContext(ctx).WithValues("GuardrailsOrchestratorReconciler.deleteService", obj)
	err = k8sClient.Get(ctx, obj, orig)
	if errors.IsNotFound(err) {
		return nil
	} else if err != nil {
		log.Error(err, "Get", "Service", obj)
		return err
	}

	err = k8sClient.Delete(ctx, orig, &client.DeleteOptions{})
	if err != nil && errors.IsNotFound(err) {
		log.Error(err, "Delete", "Service", obj)
		return err
	}
	log.Info("Delete", "Service", obj)
	return nil
}

func deleteRoute(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) (err error) {
	obj := client.ObjectKey{Name: orchestrator.Name + "-route", Namespace: orchestrator.Namespace}
	orig := &routev1.Route{}
	log := log.FromContext(ctx).WithValues("GuardrailsOrchestratorReconciler.deleteRoute", obj)
	err = k8sClient.Get(ctx, obj, orig)
	if errors.IsNotFound(err) {
		return nil
	} else if err != nil {
		log.Error(err, "Get", "Route", obj)
		return err
	}

	err = k8sClient.Delete(ctx, orig, &client.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		log.Error(err, "Delete", "Route", obj)
		return err
	}
	log.Info("Delete", "Route", obj)
	return nil
}

func doFinalizerOperationsForOrchestrator(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) (err error) {
	if err = deleteServiceAccount(ctx, orchestrator); err != nil {
		return err
	}
	if err = deleteDeployment(ctx, orchestrator); err != nil {
		return err
	}
	if err = deleteService(ctx, orchestrator); err != nil {
		return err
	}
	if err = deleteRoute(ctx, orchestrator); err != nil {
		return err
	}
	return
}

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "config", "crd", "bases", "trustyai.opendatahub.io_guardrailsorchestrators.yaml"),
			filepath.Join("..", "..", "tests", "crds"),
		},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	// Add route to scheme
	err = routev1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// Add monitoring to scheme
	err = monitoringv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = gorchv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	// +kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	By("Creating the Namespace to perform the tests")
	err = k8sClient.Create(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespaceName}})
	Expect(err).To(Not(HaveOccurred()))
})

var _ = AfterSuite(func() {
	By("Deleting the Namespace to perform the tests")
	err := k8sClient.Delete(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespaceName}})
	Expect(err).To(Not(HaveOccurred()))

	By("tearing down the test environment")
	cancel()
	err = testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

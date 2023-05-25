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

package controllers

import (
	"context"
	"fmt"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	trustyaiopendatahubiov1alpha1 "github.com/ruivieira/trustyai-service-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"path/filepath"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"testing"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var ctx context.Context
var cancel context.CancelFunc

const (
	name      = "example-trustyai-service"
	namespace = "default"
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "config", "crd", "bases"),
			filepath.Join("..", "config", "prometheus")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = trustyaiopendatahubiov1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// Add Monitoring
	err = monitoringv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())

	err = (&TrustyAIServiceReconciler{
		Client: k8sManager.GetClient(),
		//Log:    ctrl.Log.WithName("controllers").WithName("YourController"),
		Scheme: k8sManager.GetScheme(),
	}).SetupWithManager(k8sManager)
	Expect(err).NotTo(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()

})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

var _ = Describe("TrustyAI operator", func() {

	Context("Testing deployment with defaults", func() {
		var service *trustyaiopendatahubiov1alpha1.TrustyAIService
		BeforeEach(func() {
			service = &trustyaiopendatahubiov1alpha1.TrustyAIService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: trustyaiopendatahubiov1alpha1.TrustyAIServiceSpec{
					Storage: trustyaiopendatahubiov1alpha1.StorageSpec{
						Format: "PVC",
						Folder: "/data",
					},
					Data: trustyaiopendatahubiov1alpha1.DataSpec{
						Filename: "data.csv",
						Format:   "CSV",
					},
					Metrics: trustyaiopendatahubiov1alpha1.MetricsSpec{
						Schedule: "5s",
					},
				},
			}
		})

		It("should deploy the service with defaults", func() {
			fmt.Println(service)
			//ctx = context.Background()
			//Expect(k8sClient.Create(ctx, service)).Should(Succeed())
			//
			//deployment := &appsv1.Deployment{}
			//Eventually(func() error {
			//	// Define name for the deployment created by the operator
			//	namespacedNamed := types.NamespacedName{
			//		Namespace: namespace,
			//		Name:      name,
			//	}
			//	return k8sClient.Get(ctx, namespacedNamed, deployment)
			//}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to get Deployment")
			//
			//Expect(*deployment.Spec.Replicas).Should(Equal(int32(1)))
			//Expect(deployment.Namespace).Should(Equal(namespace))
			//Expect(deployment.Name).Should(Equal(name))
			//Expect(deployment.Labels["app"]).Should(Equal(name))
			//Expect(deployment.Labels["app.kubernetes.io/name"]).Should(Equal(name))
			//Expect(deployment.Labels["app.kubernetes.io/instance"]).Should(Equal(name))
			//Expect(deployment.Labels["app.kubernetes.io/part-of"]).Should(Equal(name))
			//Expect(deployment.Labels["app.kubernetes.io/version"]).Should(Equal("0.1.0"))

		})

	})
})

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
	kservev1alpha1 "github.com/kserve/kserve/pkg/apis/serving/v1alpha1"
	kservev1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"path/filepath"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"testing"
	"time"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg        *rest.Config
	k8sClient  client.Client
	testEnv    *envtest.Environment
	ctx        context.Context
	cancel     context.CancelFunc
	reconciler *TrustyAIServiceReconciler
	instance   *trustyaiopendatahubiov1alpha1.TrustyAIService
	recorder   *record.FakeRecorder
)

const (
	name      = "example-trustyai-service"
	namespace = "trustyai"
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

func createDefaultCR(namespaceCurrent string) *trustyaiopendatahubiov1alpha1.TrustyAIService {
	service := trustyaiopendatahubiov1alpha1.TrustyAIService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespaceCurrent,
		},
		Spec: trustyaiopendatahubiov1alpha1.TrustyAIServiceSpec{
			Storage: trustyaiopendatahubiov1alpha1.StorageSpec{
				Format: "PVC",
				Folder: "/data",
				Size:   "1Gi",
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
	return &service
}

func createNamespace(ctx context.Context, k8sClient client.Client, namespace string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}

	err := k8sClient.Create(ctx, ns)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// Handle the case where the namespace already exists
			return nil
		}
		// Handle other errors
		return fmt.Errorf("failed to create namespace: %w", err)
	}

	return nil
}

func createMockPV(ctx context.Context, k8sClient client.Client, pvName string, size string) error {
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: pvName,
		},
		Spec: corev1.PersistentVolumeSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse(size),
			},
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/tmp/" + pvName,
				},
			},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimDelete,
		},
	}

	err := k8sClient.Create(ctx, pv)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// PV already exists
			return nil
		}
		// Other errors
		return fmt.Errorf("failed to create PV: %w", err)
	}

	return nil
}

func removeFinalizerAndDeleteInstance(ctx context.Context, k8sClient client.Client, instance *trustyaiopendatahubiov1alpha1.TrustyAIService, finalizerName string) {
	// Get the latest state of the TrustyAIService instance
	_ = k8sClient.Get(ctx, client.ObjectKey{Name: instance.Name, Namespace: instance.Namespace}, instance)

	// Remove the finalizer from the TrustyAIService instance
	finalizerIndex := -1
	for i, f := range instance.Finalizers {
		if f == finalizerName {
			finalizerIndex = i
			break
		}
	}
	if finalizerIndex >= 0 {
		instance.Finalizers = append(instance.Finalizers[:finalizerIndex], instance.Finalizers[finalizerIndex+1:]...)
		_ = k8sClient.Update(ctx, instance)
	}

	// Delete the TrustyAIService instance
	_ = k8sClient.Delete(ctx, instance)
}

// Function to create the InferenceService
func createInferenceService(name string, namespace string) *kservev1beta1.InferenceService {
	return &kservev1beta1.InferenceService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: kservev1beta1.InferenceServiceSpec{
			Predictor: kservev1beta1.PredictorSpec{
				Model: &kservev1beta1.ModelSpec{
					ModelFormat: kservev1beta1.ModelFormat{
						Name: "sklearn",
					},
				},
			},
		},
	}
}

func checkTrustyAIServiceCondition(client client.Client, instance *trustyaiopendatahubiov1alpha1.TrustyAIService, expectedType string, expectedStatus corev1.ConditionStatus, expectedReason string) error {
	err := client.Get(context.TODO(), types.NamespacedName{
		Namespace: instance.Namespace,
		Name:      instance.Name,
	}, instance)

	if err != nil {
		return err
	}

	for _, condition := range instance.Status.Conditions {
		if condition.Type == expectedType && condition.Status == expectedStatus && condition.Reason == expectedReason {
			return nil // Condition matches expectations
		}
	}

	return fmt.Errorf("Condition did not match expectations. Expected Type: %s, Status: %s, Reason: %s", expectedType, expectedStatus, expectedReason)
}

func checkTrustyAIServiceReadyStatus(client client.Client, instance *trustyaiopendatahubiov1alpha1.TrustyAIService, expectedStatus corev1.ConditionStatus) error {
	err := client.Get(context.TODO(), types.NamespacedName{
		Namespace: instance.Namespace,
		Name:      instance.Name,
	}, instance)

	if err != nil {
		return err
	}

	if instance.Status.Ready == expectedStatus {
		return nil // Ready status matches expectations
	}

	return fmt.Errorf("Ready status did not match expectations. Expected: %s, Actual: %s", expectedStatus, instance.Status.Ready)
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "config", "crd", "bases"),
			filepath.Join("..", "config", "prometheus"),
			filepath.Join("..", "tests", "crds")},
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

	// Add InferenceServices
	err = kservev1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = kservev1beta1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// Add Routes
	err = routev1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())

	recorder := k8sManager.GetEventRecorderFor("trustyai-service-operator")

	err = (&TrustyAIServiceReconciler{
		Client:        k8sManager.GetClient(),
		Scheme:        k8sManager.GetScheme(),
		EventRecorder: recorder,
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

	Context("Testing deployment with defaults and no InferenceService", func() {
		var instance *trustyaiopendatahubiov1alpha1.TrustyAIService
		BeforeEach(func() {
			Eventually(func() error {
				return createMockPV(ctx, k8sClient, "mypv", "1Gi")
			}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to create PV")
		})

		It("should deploy the service with defaults", func() {
			ctx = context.Background()
			thisNamespace := "trusty-ns-1"
			instance = createDefaultCR(thisNamespace)
			Eventually(func() error {
				return createNamespace(ctx, k8sClient, thisNamespace)
			}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to create namespace")

			Expect(k8sClient.Create(ctx, instance)).Should(Succeed())

			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				// Define name for the deployment created by the operator
				namespacedNamed := types.NamespacedName{
					Namespace: thisNamespace,
					Name:      name,
				}
				return k8sClient.Get(ctx, namespacedNamed, deployment)
			}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to get Deployment")

			Expect(*deployment.Spec.Replicas).Should(Equal(int32(1)))
			Expect(deployment.Namespace).Should(Equal(thisNamespace))
			Expect(deployment.Name).Should(Equal(name))
			Expect(deployment.Labels["app"]).Should(Equal(name))
			Expect(deployment.Labels["app.kubernetes.io/name"]).Should(Equal(name))
			Expect(deployment.Labels["app.kubernetes.io/instance"]).Should(Equal(name))
			Expect(deployment.Labels["app.kubernetes.io/part-of"]).Should(Equal(componentName))
			Expect(deployment.Labels["app.kubernetes.io/version"]).Should(Equal("0.1.0"))

			Expect(deployment.Spec.Template.Spec.Containers[0].Image).Should(Equal("quay.io/trustyai/trustyai-service:latest"))

			service := &corev1.Service{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: thisNamespace}, service)
			}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to get Service")

			Expect(service.Annotations["prometheus.io/path"]).Should(Equal("/q/metrics"))
			Expect(service.Annotations["prometheus.io/scheme"]).Should(Equal("http"))
			Expect(service.Annotations["prometheus.io/scrape"]).Should(Equal("true"))
			Expect(service.Namespace).Should(Equal(thisNamespace))

			Eventually(func() error {
				return checkTrustyAIServiceCondition(k8sClient, instance, StatusTypePVCAvailable, corev1.ConditionTrue, StatusReasonPVCFound)
			}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to get the correct condition")

			// No InferenceService deployed yet
			Eventually(func() error {
				return checkTrustyAIServiceCondition(k8sClient, instance, StatusTypeInferenceServicesPresent, corev1.ConditionFalse, StatusReasonInferenceServicesNotFound)
			}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to get the correct condition")
		})

		It("should deploy the service with defaults and one InferenceService", func() {
			ctx = context.Background()
			thisNamespace := "trusty-ns-2"
			instance = createDefaultCR(thisNamespace)
			Eventually(func() error {
				return createNamespace(ctx, k8sClient, thisNamespace)
			}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to create namespace")

			// Creating the InferenceService
			inferenceService := createInferenceService("my-model", thisNamespace)
			Expect(k8sClient.Create(ctx, inferenceService)).Should(Succeed())

			timeout := 10 * time.Second
			interval := 1 * time.Second

			// Wait for the InferenceService to be ready
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Namespace: inferenceService.Namespace,
					Name:      inferenceService.Name,
				}, inferenceService)

				if err != nil {
					return false
				}

				return true // return true when the InferenceService is ready
			}, timeout, interval).Should(BeTrue())

			Expect(k8sClient.Create(ctx, instance)).Should(Succeed())

			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				// Define name for the deployment created by the operator
				namespacedNamed := types.NamespacedName{
					Namespace: thisNamespace,
					Name:      name,
				}
				return k8sClient.Get(ctx, namespacedNamed, deployment)
			}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to get Deployment")

			Expect(*deployment.Spec.Replicas).Should(Equal(int32(1)))
			Expect(deployment.Namespace).Should(Equal(thisNamespace))
			Expect(deployment.Name).Should(Equal(name))
			Expect(deployment.Labels["app"]).Should(Equal(name))
			Expect(deployment.Labels["app.kubernetes.io/name"]).Should(Equal(name))
			Expect(deployment.Labels["app.kubernetes.io/instance"]).Should(Equal(name))
			Expect(deployment.Labels["app.kubernetes.io/part-of"]).Should(Equal(componentName))
			Expect(deployment.Labels["app.kubernetes.io/version"]).Should(Equal("0.1.0"))

			Expect(deployment.Spec.Template.Spec.Containers[0].Image).Should(Equal("quay.io/trustyai/trustyai-service:latest"))

			service := &corev1.Service{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: thisNamespace}, service)
			}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to get Service")

			Expect(service.Annotations["prometheus.io/path"]).Should(Equal("/q/metrics"))
			Expect(service.Annotations["prometheus.io/scheme"]).Should(Equal("http"))
			Expect(service.Annotations["prometheus.io/scrape"]).Should(Equal("true"))
			Expect(service.Namespace).Should(Equal(thisNamespace))

			Eventually(func() error {
				return checkTrustyAIServiceCondition(k8sClient, instance, StatusTypePVCAvailable, corev1.ConditionTrue, StatusReasonPVCFound)
			}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to get the correct condition")

			// No InferenceService deployed yet
			Eventually(func() error {
				return checkTrustyAIServiceCondition(k8sClient, instance, StatusTypeInferenceServicesPresent, corev1.ConditionTrue, StatusReasonInferenceServicesFound)
			}, time.Second*20, time.Millisecond*250).Should(Succeed(), "failed to get the correct condition")

		})

	})

	Context("Testing deployment with defaults in multiple namespaces", func() {
		var instances []*trustyaiopendatahubiov1alpha1.TrustyAIService

		var namespaces = []string{"namespace1", "namespace2", "namespace3"}

		BeforeEach(func() {
			instances = make([]*trustyaiopendatahubiov1alpha1.TrustyAIService, len(namespaces))
			for i, thisNamespace := range namespaces {
				instances[i] = createDefaultCR(thisNamespace)
				instances[i].Namespace = thisNamespace
				Eventually(func() error {
					return createNamespace(ctx, k8sClient, thisNamespace)
				}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to create namespace")
			}
		})

		It("should deploy the services with defaults", func() {
			for _, instance := range instances {
				Expect(k8sClient.Create(ctx, instance)).Should(Succeed())
				deployment := &appsv1.Deployment{}
				Eventually(func() error {
					// Define name for the deployment created by the operator
					namespacedNamed := types.NamespacedName{
						Namespace: instance.Namespace,
						Name:      name,
					}
					return k8sClient.Get(ctx, namespacedNamed, deployment)
				}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to get Deployment")

				Expect(*deployment.Spec.Replicas).Should(Equal(int32(1)))
				Expect(deployment.Namespace).Should(Equal(instance.Namespace))
				Expect(deployment.Name).Should(Equal(name))
				Expect(deployment.Labels["app"]).Should(Equal(name))
				Expect(deployment.Labels["app.kubernetes.io/name"]).Should(Equal(name))
				Expect(deployment.Labels["app.kubernetes.io/instance"]).Should(Equal(name))
				Expect(deployment.Labels["app.kubernetes.io/part-of"]).Should(Equal(componentName))
				Expect(deployment.Labels["app.kubernetes.io/version"]).Should(Equal("0.1.0"))

				Expect(deployment.Spec.Template.Spec.Containers[0].Image).Should(Equal("quay.io/trustyai/trustyai-service:latest"))

				service := &corev1.Service{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: instance.Namespace}, service)
				}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to get Service")

				Expect(service.Annotations["prometheus.io/path"]).Should(Equal("/q/metrics"))
				Expect(service.Annotations["prometheus.io/scheme"]).Should(Equal("http"))
				Expect(service.Annotations["prometheus.io/scrape"]).Should(Equal("true"))
			}
		})

		AfterEach(func() {
			for _, instance := range instances {
				// Delete the TrustyAIService instance
				Expect(k8sClient.Delete(ctx, instance)).Should(Succeed())

				// Delete the namespace
				namespaceObj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: instance.Namespace}}
				Expect(k8sClient.Delete(ctx, namespaceObj)).Should(Succeed())
			}
		})
	})

})

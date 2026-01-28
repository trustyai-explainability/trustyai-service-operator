package evalhub

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var ctx context.Context
var cancel context.CancelFunc

const (
	timeout  = time.Second * 10
	interval = time.Millisecond * 250
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "EvalHub Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = evalhubv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

// Helper functions for tests

// createEvalHubInstance creates a basic EvalHub instance for testing
func createEvalHubInstance(name, namespace string) *evalhubv1alpha1.EvalHub {
	replicas := int32(1)
	return &evalhubv1alpha1.EvalHub{
		TypeMeta: metav1.TypeMeta{
			APIVersion: evalhubv1alpha1.GroupVersion.String(),
			Kind:       "EvalHub",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: evalhubv1alpha1.EvalHubSpec{
			Replicas: &replicas,
			Env: []corev1.EnvVar{
				{
					Name:  "TEST_ENV",
					Value: "test-value",
				},
			},
		},
	}
}

// createNamespace creates a test namespace
func createNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

// createConfigMap creates a test config map with eval hub image
func createConfigMap(name, namespace string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string]string{
			"evalHubImage":    "quay.io/ruimvieira/eval-hub:test",
			"kube-rbac-proxy": "gcr.io/kubebuilder/kube-rbac-proxy:v0.13.1",
		},
	}
}

// setupReconciler creates and returns a EvalHubReconciler for testing
func setupReconciler(namespace string) (*EvalHubReconciler, context.Context) {
	eventRecorder := record.NewFakeRecorder(100)

	reconciler := &EvalHubReconciler{
		Client:        k8sClient,
		Scheme:        scheme.Scheme,
		Namespace:     namespace,
		EventRecorder: eventRecorder,
	}

	return reconciler, ctx
}

// waitForDeployment waits for deployment to be created
func waitForDeployment(name, namespace string) *appsv1.Deployment {
	deployment := &appsv1.Deployment{}
	Eventually(func() bool {
		err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		}, deployment)
		return err == nil
	}, timeout, interval).Should(BeTrue())
	return deployment
}

// waitForService waits for service to be created
func waitForService(name, namespace string) *corev1.Service {
	service := &corev1.Service{}
	Eventually(func() bool {
		err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		}, service)
		return err == nil
	}, timeout, interval).Should(BeTrue())
	return service
}

// waitForConfigMap waits for configmap to be created
func waitForConfigMap(name, namespace string) *corev1.ConfigMap {
	configMap := &corev1.ConfigMap{}
	Eventually(func() bool {
		err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		}, configMap)
		return err == nil
	}, timeout, interval).Should(BeTrue())
	return configMap
}

// waitForEvalHubStatus waits for EvalHub status to be updated
func waitForEvalHubStatus(name, namespace, expectedPhase string) {
	evalHub := &evalhubv1alpha1.EvalHub{}
	Eventually(func() string {
		err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		}, evalHub)
		if err != nil {
			return ""
		}
		return evalHub.Status.Phase
	}, timeout, interval).Should(Equal(expectedPhase))
}

// performReconcile performs a reconcile operation for the given EvalHub
func performReconcile(reconciler *EvalHubReconciler, name, namespace string) (ctrl.Result, error) {
	return reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	})
}

// deleteNamespace deletes a namespace without waiting (faster cleanup)
func deleteNamespace(namespace *corev1.Namespace) {
	if namespace == nil {
		return
	}

	// Delete the namespace - don't wait for completion
	// Unique namespace names prevent conflicts
	err := k8sClient.Delete(ctx, namespace)
	if err != nil && !errors.IsNotFound(err) {
		// Log error but don't fail the test cleanup
		logf.Log.Error(err, "Failed to delete namespace", "namespace", namespace.Name)
	}
}

// cleanupResourcesInNamespace deletes all test resources in a namespace
func cleanupResourcesInNamespace(namespace string, evalHub *evalhubv1alpha1.EvalHub, configMap *corev1.ConfigMap) {
	if evalHub != nil {
		k8sClient.Delete(ctx, evalHub)
	}
	if configMap != nil {
		k8sClient.Delete(ctx, configMap)
	}
}

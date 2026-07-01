package module

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	modulev1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/module/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

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
	RunSpecs(t, "Module Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "components", "module", "crd"),
		},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = modulev1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

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

// Helper functions

// createModuleInstance creates a TrustyAI module instance for testing
func createModuleInstance(name string, managementState modulev1alpha1.ManagementState) *modulev1alpha1.TrustyAI {
	return &modulev1alpha1.TrustyAI{
		TypeMeta: metav1.TypeMeta{
			APIVersion: modulev1alpha1.GroupVersion.String(),
			Kind:       "TrustyAI",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: modulev1alpha1.TrustyAISpec{
			ManagementState: managementState,
			EnabledServices: map[string]bool{
				"service1": true,
				"service2": false,
			},
		},
	}
}

// setupReconciler creates and returns a TrustyAIReconciler for testing
func setupReconciler() *TrustyAIReconciler {
	eventRecorder := record.NewFakeRecorder(100)

	return &TrustyAIReconciler{
		Client:                k8sClient,
		Scheme:                scheme.Scheme,
		Namespace:             "test-namespace",
		OperatorConfigMapName: "test-config",
		EventRecorder:         eventRecorder,
	}
}

// performReconcile performs a reconcile operation for the given module
func performReconcile(reconciler *TrustyAIReconciler, name string) (ctrl.Result, error) {
	return reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: client.ObjectKey{
			Name: name,
		},
	})
}

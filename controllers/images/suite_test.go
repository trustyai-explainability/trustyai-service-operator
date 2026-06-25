package images

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// Test constants
const (
	testNamespace    = "test-namespace"
	testConfigMap    = "test-configmap"
	testImageFromCM  = "quay.io/trustyai/service:from-configmap"
	testImageFromEnv = "quay.io/trustyai/service:from-env-var"
	testFallback     = "quay.io/trustyai/service:fallback"
)

var (
	ctx    context.Context
	scheme *runtime.Scheme
)

func TestImages(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Images Resolver Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	ctx = context.Background()
	scheme = runtime.NewScheme()
	Expect(corev1.AddToScheme(scheme)).To(Succeed())
})

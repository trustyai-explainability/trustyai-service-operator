package trustyaimodule

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	platformv1alpha1 "github.com/trustyai-explainability/trustyai-operator-module/pkg/apis/v1alpha1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

// defaultEnvtestK8sVersion mirrors ENVTEST_K8S_VERSION in the repo root Makefile.
// Kept in sync manually since this file may also run outside the Makefile (e.g.
// `go test` invoked directly, without KUBEBUILDER_ASSETS set).
const defaultEnvtestK8sVersion = "1.29.0"

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment

// envtestK8sVersion returns ENVTEST_K8S_VERSION if set (matching the repo root
// Makefile's env var of the same name), falling back to defaultEnvtestK8sVersion
// otherwise.
func envtestK8sVersion() string {
	if v := os.Getenv("ENVTEST_K8S_VERSION"); v != "" {
		return v
	}
	return defaultEnvtestK8sVersion
}

func TestTrustyAIModule(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "TrustyAI Module Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "crd", "bases"),
			filepath.Join("..", "..", "..", "tests", "crds"),
		},
		ErrorIfCRDPathMissing: true,

		// The BinaryAssetsDirectory is only required if you want to run the tests directly
		// without call the makefile target test. If not informed it will look for the
		// default path defined in controller-runtime which is /usr/local/kubebuilder/.
		// This reuses the envtest binaries downloaded for the main operator module via
		// `make envtest` at the repo root, since both modules test against the same
		// Kubernetes version. Overridden by the KUBEBUILDER_ASSETS env var when set
		// (e.g. by `make test-tom`), so a Makefile ENVTEST_K8S_VERSION bump only
		// affects direct `go test` runs, not the normal make-driven test run.
		BinaryAssetsDirectory: filepath.Join("..", "..", "..", "bin", "k8s",
			fmt.Sprintf("%s-%s-%s", envtestK8sVersion(), runtime.GOOS, runtime.GOARCH)),
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = platformv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

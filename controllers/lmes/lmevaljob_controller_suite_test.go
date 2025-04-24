package lmes_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"

	lmesv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/lmes/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/lmes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
	recorder  *record.FakeRecorder
)

const (
	operatorNamespace = "system"
	testNamespace     = "test"
	defaultTimeout    = time.Second * 10
	defaultPolling    = time.Millisecond * 250
)

// WaitFor is a function that takes a function which returns an error, and an error message.
// It will repeatedly call the provided function until it succeeds or the timeout is reached.
func WaitFor(operation func() error, errorMsg string) {
	Eventually(operation, defaultTimeout, defaultPolling).Should(Succeed(), errorMsg)
}

func TestSource(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "LMEvalJob controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = lmesv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0", // no metric server
		},
	})
	Expect(err).ToNot(HaveOccurred())

	recorder = record.NewFakeRecorder(10)
	Expect(err).NotTo(HaveOccurred())

	WaitFor(func() error {
		return k8sClient.Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: operatorNamespace,
			},
		})
	}, "failed to create operator namespace")

	WaitFor(func() error {
		return k8sClient.Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		})
	}, "failed to create test namespace")

	WaitFor(func() error {
		configMap := defaultConfigMap()
		return k8sClient.Create(ctx, configMap)
	}, "failed to create default ConfigMap")

	err = lmes.ControllerSetUp(k8sManager, operatorNamespace, constants.ConfigMap, recorder)

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

func defaultConfigMap() *corev1.ConfigMap {
	// Define the ConfigMap with the necessary data
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.ConfigMap,
			Namespace: operatorNamespace,
		},
		Data: map[string]string{
			lmes.PodImageKey:            "job-image",
			lmes.DriverImageKey:         "driver-image",
			lmes.PodCheckingIntervalKey: "10s",
			lmes.ImagePullPolicyKey:     "Always",
			lmes.MaxBatchSizeKey:        "24",
			lmes.DefaultBatchSizeKey:    "8",
			lmes.DetectDeviceKey:        "true",
			lmes.AllowOnline:            "true",
			lmes.AllowCodeExecution:     "true",
			lmes.DriverPort:             "18080",
		},
	}
}

var _ = Describe("Simple LMEvalJob", func() {
	Context("Create a LMEvalJob", func() {
		ctx := context.Background()
		trueB := true
		It("Create a pod after for the LMEvalJob", func() {
			lmevalJob := &lmesv1alpha1.LMEvalJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: testNamespace,
				},
				TypeMeta: metav1.TypeMeta{
					Kind:       lmesv1alpha1.KindName,
					APIVersion: lmesv1alpha1.Version,
				},
				Spec: lmesv1alpha1.LMEvalJobSpec{
					AllowOnline:        &trueB,
					AllowCodeExecution: &trueB,
					Model:              "hf",
					ModelArgs: []lmesv1alpha1.Arg{
						{Name: "pretrained", Value: "google/flan-t5-base"},
					},
					TaskList: lmesv1alpha1.TaskList{
						TaskNames: []string{"task1", "task2"},
						TaskRecipes: []lmesv1alpha1.TaskRecipe{
							{
								Card: lmesv1alpha1.Card{
									Name: "cards.wnli",
								},
								Template: &lmesv1alpha1.Template{
									Name: "templates.classification.multi_class.relation.default",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, lmevalJob)).Should(Succeed())

			newjob := &lmesv1alpha1.LMEvalJob{}
			WaitFor(func() error {
				return k8sClient.Get(
					ctx,
					types.NamespacedName{Name: "test", Namespace: testNamespace},
					newjob,
				)
			}, "can't find the LMEvalJob")

			jobpod := &corev1.Pod{}
			WaitFor(func() error {
				return k8sClient.Get(
					ctx,
					types.NamespacedName{Name: "test", Namespace: testNamespace},
					jobpod,
				)
			}, "can't find the job pod")
		})
	})
})

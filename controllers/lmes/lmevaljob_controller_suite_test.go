package lmes_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
			filepath.Join("..", "..", "config", "components", "lmes", "crd"),
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

var _ = Describe("LMEvalJob CA bundle injection", func() {
	ctx := context.Background()
	trueB := true

	It("injects merged CA bundle when base_url uses HTTPS", func() {
		caConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      lmes.DefaultCABundleConfigMapName,
				Namespace: testNamespace,
			},
			Data: map[string]string{
				"ca-bundle.crt": "-----BEGIN CERTIFICATE-----\npublic-ca\n-----END CERTIFICATE-----",
			},
		}
		if err := k8sClient.Create(ctx, caConfigMap); err != nil {
			Expect(apierrors.IsAlreadyExists(err)).To(BeTrue(),
				"unexpected error creating CA ConfigMap: %v", err)
		}

		job := &lmesv1alpha1.LMEvalJob{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ca-bundle",
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
					{Name: "base_url", Value: "https://model.example.com"},
				},
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"task1"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, job)).Should(Succeed())

		mergedCMName := "test-ca-bundle" + lmes.MergedCAConfigMapSuffix

		// Verify the merged ConfigMap was created
		mergedCM := &corev1.ConfigMap{}
		WaitFor(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{
				Name: mergedCMName, Namespace: testNamespace,
			}, mergedCM)
		}, "merged CA ConfigMap was not created")
		Expect(mergedCM.Data).To(HaveKey(lmes.MergedCABundleKey))
		Expect(mergedCM.Data[lmes.MergedCABundleKey]).To(ContainSubstring("public-ca"))

		pod := &corev1.Pod{}
		WaitFor(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{
				Name: "test-ca-bundle", Namespace: testNamespace,
			}, pod)
		}, "pod was not created for HTTPS job")

		// Verify CA bundle volume points to merged ConfigMap
		foundVolume := false
		for _, v := range pod.Spec.Volumes {
			if v.Name == lmes.CABundleVolumeName {
				foundVolume = true
				Expect(v.VolumeSource.ConfigMap).NotTo(BeNil())
				Expect(v.VolumeSource.ConfigMap.Name).To(Equal(mergedCMName))
			}
		}
		Expect(foundVolume).To(BeTrue(), "CA bundle volume not found on pod")

		// Verify volume mount uses merged key
		mainContainer := pod.Spec.Containers[0]
		foundMount := false
		for _, m := range mainContainer.VolumeMounts {
			if m.Name == lmes.CABundleVolumeName {
				foundMount = true
				Expect(m.MountPath).To(Equal(lmes.CABundleMountPath))
				Expect(m.SubPath).To(Equal(lmes.MergedCABundleKey))
				Expect(m.ReadOnly).To(BeTrue())
			}
		}
		Expect(foundMount).To(BeTrue(), "CA bundle volume mount not found on main container")

		// Verify REQUESTS_CA_BUNDLE env var
		foundEnv := false
		for _, env := range mainContainer.Env {
			if env.Name == "REQUESTS_CA_BUNDLE" {
				foundEnv = true
				Expect(env.Value).To(Equal(lmes.CABundleMountPath))
			}
		}
		Expect(foundEnv).To(BeTrue(), "REQUESTS_CA_BUNDLE env var not found")
	})

	It("merges both odh-trusted-ca-bundle and openshift-service-ca.crt when both exist", func() {
		odhCAConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      lmes.DefaultCABundleConfigMapName,
				Namespace: testNamespace,
			},
			Data: map[string]string{
				"ca-bundle.crt": "-----BEGIN CERTIFICATE-----\npublic-ca\n-----END CERTIFICATE-----",
			},
		}
		if err := k8sClient.Create(ctx, odhCAConfigMap); err != nil {
			Expect(apierrors.IsAlreadyExists(err)).To(BeTrue(),
				"unexpected error creating ODH CA ConfigMap: %v", err)
		}

		svcCAConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      lmes.ServiceCAConfigMapName,
				Namespace: testNamespace,
			},
			Data: map[string]string{
				lmes.ServiceCAKey: "-----BEGIN CERTIFICATE-----\nservice-ca\n-----END CERTIFICATE-----",
			},
		}
		if err := k8sClient.Create(ctx, svcCAConfigMap); err != nil {
			Expect(apierrors.IsAlreadyExists(err)).To(BeTrue(),
				"unexpected error creating service CA ConfigMap: %v", err)
		}

		job := &lmesv1alpha1.LMEvalJob{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ca-merged",
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
					{Name: "base_url", Value: "https://model.svc.cluster.local:8443"},
				},
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"task1"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, job)).Should(Succeed())

		mergedCMName := "test-ca-merged" + lmes.MergedCAConfigMapSuffix

		mergedCM := &corev1.ConfigMap{}
		WaitFor(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{
				Name: mergedCMName, Namespace: testNamespace,
			}, mergedCM)
		}, "merged CA ConfigMap was not created")

		Expect(mergedCM.Data[lmes.MergedCABundleKey]).To(ContainSubstring("public-ca"),
			"merged bundle should contain public CAs from odh-trusted-ca-bundle")
		Expect(mergedCM.Data[lmes.MergedCABundleKey]).To(ContainSubstring("service-ca"),
			"merged bundle should contain service-serving CA from openshift-service-ca.crt")
	})

	It("does not inject CA bundle when base_url uses HTTP", func() {
		job := &lmesv1alpha1.LMEvalJob{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-no-ca-http",
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
					{Name: "base_url", Value: "http://model.example.com"},
				},
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"task1"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, job)).Should(Succeed())

		pod := &corev1.Pod{}
		WaitFor(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{
				Name: "test-no-ca-http", Namespace: testNamespace,
			}, pod)
		}, "pod was not created for HTTP job")

		for _, v := range pod.Spec.Volumes {
			Expect(v.Name).NotTo(Equal(lmes.CABundleVolumeName), "unexpected CA bundle volume on HTTP job")
		}
		for _, env := range pod.Spec.Containers[0].Env {
			Expect(env.Name).NotTo(Equal("REQUESTS_CA_BUNDLE"), "unexpected REQUESTS_CA_BUNDLE on HTTP job")
		}
	})

	It("does not inject CA bundle when verify_certificate is explicitly set", func() {
		job := &lmesv1alpha1.LMEvalJob{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-no-ca-explicit",
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
					{Name: "base_url", Value: "https://model.example.com"},
					{Name: "verify_certificate", Value: "false"},
				},
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"task1"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, job)).Should(Succeed())

		pod := &corev1.Pod{}
		WaitFor(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{
				Name: "test-no-ca-explicit", Namespace: testNamespace,
			}, pod)
		}, "pod was not created for job with explicit verify_certificate")

		for _, v := range pod.Spec.Volumes {
			Expect(v.Name).NotTo(Equal(lmes.CABundleVolumeName), "unexpected CA bundle volume when verify_certificate is set")
		}
		for _, env := range pod.Spec.Containers[0].Env {
			Expect(env.Name).NotTo(Equal("REQUESTS_CA_BUNDLE"), "unexpected REQUESTS_CA_BUNDLE when verify_certificate is set")
		}
	})
})

var _ = Describe("LMEvalJob re-run after spec change", func() {
	ctx := context.Background()
	trueB := true

	It("resets a completed job when the spec is updated", func() {
		job := &lmesv1alpha1.LMEvalJob{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-rerun",
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
					TaskNames: []string{"task1"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, job)).Should(Succeed())

		// Wait for the pod to be created and the job to reach Scheduled state
		WaitFor(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{
				Name: "test-rerun", Namespace: testNamespace,
			}, &corev1.Pod{})
		}, "initial pod was not created")

		WaitFor(func() error {
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Name: "test-rerun", Namespace: testNamespace,
			}, job); err != nil {
				return err
			}
			if job.Status.State != lmesv1alpha1.ScheduledJobState {
				return fmt.Errorf("expected Scheduled, got %s", job.Status.State)
			}
			return nil
		}, "job did not reach Scheduled state")

		Expect(job.Annotations).To(HaveKey(lmes.LastScheduledGenerationAnnotation))

		// Manually mark the job as Complete (no kubelet in envtest to do this)
		job.Status.State = lmesv1alpha1.CompleteJobState
		job.Status.Reason = lmesv1alpha1.SucceedReason
		now := metav1.Now()
		job.Status.CompleteTime = &now
		Expect(k8sClient.Status().Update(ctx, job)).Should(Succeed())

		// Update the spec to bump metadata.generation
		Eventually(func() error {
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Name: "test-rerun", Namespace: testNamespace,
			}, job); err != nil {
				return err
			}
			job.Spec.ModelArgs = []lmesv1alpha1.Arg{
				{Name: "pretrained", Value: "google/flan-t5-small"},
			}
			return k8sClient.Update(ctx, job)
		}, defaultTimeout, defaultPolling).Should(Succeed(), "failed to update job spec")

		// The controller should detect the generation change and reset the job
		WaitFor(func() error {
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Name: "test-rerun", Namespace: testNamespace,
			}, job); err != nil {
				return err
			}
			if job.Status.State == lmesv1alpha1.CompleteJobState {
				return fmt.Errorf("job is still Complete, waiting for re-run reset")
			}
			return nil
		}, "job was not reset after spec change")
	})

	It("does not reset a completed job when the spec is unchanged", func() {
		job := &lmesv1alpha1.LMEvalJob{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-no-rerun",
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
					TaskNames: []string{"task1"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, job)).Should(Succeed())

		WaitFor(func() error {
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Name: "test-no-rerun", Namespace: testNamespace,
			}, job); err != nil {
				return err
			}
			if job.Status.State != lmesv1alpha1.ScheduledJobState {
				return fmt.Errorf("expected Scheduled, got %s", job.Status.State)
			}
			return nil
		}, "job did not reach Scheduled state")

		// Mark as Complete without changing the spec
		job.Status.State = lmesv1alpha1.CompleteJobState
		job.Status.Reason = lmesv1alpha1.SucceedReason
		now := metav1.Now()
		job.Status.CompleteTime = &now
		Expect(k8sClient.Status().Update(ctx, job)).Should(Succeed())

		// Verify the job stays Complete (no re-run triggered)
		Consistently(func() error {
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Name: "test-no-rerun", Namespace: testNamespace,
			}, job); err != nil {
				return err
			}
			if job.Status.State != lmesv1alpha1.CompleteJobState {
				return fmt.Errorf("expected Complete, got %s", job.Status.State)
			}
			return nil
		}, time.Second*3, defaultPolling).Should(Succeed(),
			"completed job should not be reset when spec is unchanged")
	})

	It("updates the merged CA ConfigMap when an HTTPS job is re-run after spec change", func() {
		// Ensure CA source ConfigMap exists (may already exist from CA injection tests)
		caConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      lmes.DefaultCABundleConfigMapName,
				Namespace: testNamespace,
			},
			Data: map[string]string{
				"ca-bundle.crt": "-----BEGIN CERTIFICATE-----\npublic-ca\n-----END CERTIFICATE-----",
			},
		}
		err := k8sClient.Create(ctx, caConfigMap)
		if err != nil {
			Expect(apierrors.IsAlreadyExists(err)).To(BeTrue(),
				"unexpected error creating CA ConfigMap: %v", err)
		}

		job := &lmesv1alpha1.LMEvalJob{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-rerun-ca",
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
					{Name: "base_url", Value: "https://model.svc.cluster.local:8443"},
				},
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"task1"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, job)).Should(Succeed())

		mergedCMName := "test-rerun-ca" + lmes.MergedCAConfigMapSuffix

		// Wait for initial merged ConfigMap and pod
		mergedCM := &corev1.ConfigMap{}
		WaitFor(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{
				Name: mergedCMName, Namespace: testNamespace,
			}, mergedCM)
		}, "initial merged CA ConfigMap was not created")

		WaitFor(func() error {
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Name: "test-rerun-ca", Namespace: testNamespace,
			}, job); err != nil {
				return err
			}
			if job.Status.State != lmesv1alpha1.ScheduledJobState {
				return fmt.Errorf("expected Scheduled, got %s", job.Status.State)
			}
			return nil
		}, "job did not reach Scheduled state")

		pod := &corev1.Pod{}
		WaitFor(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{
				Name: "test-rerun-ca", Namespace: testNamespace,
			}, pod)
		}, "initial pod was not created")

		// Verify initial pod has CA volume
		foundVolume := false
		for _, v := range pod.Spec.Volumes {
			if v.Name == lmes.CABundleVolumeName {
				foundVolume = true
				Expect(v.VolumeSource.ConfigMap.Name).To(Equal(mergedCMName))
			}
		}
		Expect(foundVolume).To(BeTrue(), "initial pod missing CA bundle volume")

		// Mark as Complete, then update the spec to trigger re-run
		job.Status.State = lmesv1alpha1.CompleteJobState
		job.Status.Reason = lmesv1alpha1.SucceedReason
		now := metav1.Now()
		job.Status.CompleteTime = &now
		Expect(k8sClient.Status().Update(ctx, job)).Should(Succeed())

		Eventually(func() error {
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Name: "test-rerun-ca", Namespace: testNamespace,
			}, job); err != nil {
				return err
			}
			job.Spec.ModelArgs = []lmesv1alpha1.Arg{
				{Name: "pretrained", Value: "google/flan-t5-small"},
				{Name: "base_url", Value: "https://model.svc.cluster.local:8443"},
			}
			return k8sClient.Update(ctx, job)
		}, defaultTimeout, defaultPolling).Should(Succeed(), "failed to update HTTPS job spec")

		// Wait for the job to be reset and re-scheduled
		WaitFor(func() error {
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Name: "test-rerun-ca", Namespace: testNamespace,
			}, job); err != nil {
				return err
			}
			if job.Status.State != lmesv1alpha1.ScheduledJobState {
				return fmt.Errorf("expected Scheduled after re-run, got %s", job.Status.State)
			}
			return nil
		}, "job was not re-scheduled after spec change")

		// Verify the merged ConfigMap still exists with correct data after re-run
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: mergedCMName, Namespace: testNamespace,
		}, mergedCM)).Should(Succeed())
		Expect(mergedCM.Data).To(HaveKey(lmes.MergedCABundleKey))
		Expect(mergedCM.Data[lmes.MergedCABundleKey]).To(ContainSubstring("public-ca"),
			"re-run merged bundle should still contain public CAs")
	})
})

package tas

import (
	"context"

	kservev1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("KServe logger HTTP annotation", func() {
	var (
		testReconciler *TrustyAIServiceReconciler
		testCtx        context.Context
	)

	BeforeEach(func() {
		testReconciler = &TrustyAIServiceReconciler{
			Client:        fake.NewClientBuilder().WithScheme(scheme.Scheme).Build(),
			Scheme:        scheme.Scheme,
			EventRecorder: record.NewFakeRecorder(10),
			Namespace:     operatorNamespace,
		}
		testCtx = context.Background()
	})

	Context("When patchKServe is called with useHTTPS=true (default for Raw)", func() {
		It("should set an HTTPS logger URL on the InferenceService", func() {
			namespace := "trusty-kserve-https-test"
			Expect(createNamespace(testCtx, testReconciler.Client, namespace)).To(Succeed())

			instance := createDefaultPVCCustomResource(namespace)
			Expect(createTestPVC(testCtx, testReconciler.Client, instance)).To(Succeed())

			inferenceService := createInferenceService("my-model-https", namespace)
			Expect(testReconciler.Client.Create(testCtx, inferenceService)).To(Succeed())

			// Call patchKServe with useHTTPS=true (default Raw behavior)
			Expect(testReconciler.patchKServe(testCtx, instance, *inferenceService, namespace, instance.Name, false, true)).To(Succeed())

			// Fetch the updated InferenceService
			updated := &kservev1beta1.InferenceService{}
			Expect(testReconciler.Client.Get(testCtx, types.NamespacedName{Name: "my-model-https", Namespace: namespace}, updated)).To(Succeed())

			Expect(updated.Spec.Predictor.Logger).NotTo(BeNil())
			expectedURL := utils.GenerateHTTPSKServeLoggerURL(instance.Name, namespace)
			Expect(*updated.Spec.Predictor.Logger.URL).To(Equal(expectedURL))
			Expect(*updated.Spec.Predictor.Logger.URL).To(HavePrefix("https://"))
		})
	})

	Context("When patchKServe is called with useHTTPS=false (annotation-driven)", func() {
		It("should set an HTTP logger URL on the InferenceService", func() {
			namespace := "trusty-kserve-http-test"
			Expect(createNamespace(testCtx, testReconciler.Client, namespace)).To(Succeed())

			instance := createDefaultPVCCustomResource(namespace)
			Expect(createTestPVC(testCtx, testReconciler.Client, instance)).To(Succeed())

			inferenceService := createInferenceService("my-model-http", namespace)
			Expect(testReconciler.Client.Create(testCtx, inferenceService)).To(Succeed())

			// Call patchKServe with useHTTPS=false (HTTP logger annotation behavior)
			Expect(testReconciler.patchKServe(testCtx, instance, *inferenceService, namespace, instance.Name, false, false)).To(Succeed())

			// Fetch the updated InferenceService
			updated := &kservev1beta1.InferenceService{}
			Expect(testReconciler.Client.Get(testCtx, types.NamespacedName{Name: "my-model-http", Namespace: namespace}, updated)).To(Succeed())

			Expect(updated.Spec.Predictor.Logger).NotTo(BeNil())
			expectedURL := utils.GenerateKServeLoggerURL(instance.Name, namespace)
			Expect(*updated.Spec.Predictor.Logger.URL).To(Equal(expectedURL))
			Expect(*updated.Spec.Predictor.Logger.URL).To(HavePrefix("http://"))
		})
	})

	Context("When handleInferenceServices processes a Raw deployment without the annotation", func() {
		It("should default to HTTPS logger URL", func() {
			namespace := "trusty-kserve-raw-default"
			Expect(createNamespace(testCtx, testReconciler.Client, namespace)).To(Succeed())

			instance := createDefaultPVCCustomResource(namespace)
			// No annotation set — default behavior

			// Create a Raw deployment InferenceService
			infService := &kservev1beta1.InferenceService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "raw-model",
					Namespace: namespace,
					Annotations: map[string]string{
						"serving.kserve.io/deploymentMode": DEPLOYMENT_MODE_RAW,
					},
				},
				Spec: kservev1beta1.InferenceServiceSpec{
					Predictor: kservev1beta1.PredictorSpec{
						Model: &kservev1beta1.ModelSpec{
							ModelFormat: kservev1beta1.ModelFormat{Name: "sklearn"},
						},
					},
				},
			}
			Expect(testReconciler.Client.Create(testCtx, infService)).To(Succeed())

			_, err := testReconciler.handleInferenceServices(testCtx, instance, namespace, modelMeshLabelKey, modelMeshLabelValue, payloadProcessorName, instance.Name, false)
			Expect(err).ToNot(HaveOccurred())

			// Fetch updated InferenceService — should have HTTPS URL
			updated := &kservev1beta1.InferenceService{}
			Expect(testReconciler.Client.Get(testCtx, types.NamespacedName{Name: "raw-model", Namespace: namespace}, updated)).To(Succeed())

			Expect(updated.Spec.Predictor.Logger).NotTo(BeNil())
			Expect(*updated.Spec.Predictor.Logger.URL).To(HavePrefix("https://"))
		})
	})

	Context("When handleInferenceServices processes a Raw deployment with the HTTP annotation", func() {
		It("should use HTTP logger URL", func() {
			namespace := "trusty-kserve-raw-http"
			Expect(createNamespace(testCtx, testReconciler.Client, namespace)).To(Succeed())

			instance := createDefaultPVCCustomResource(namespace)
			// Set the HTTP logger annotation on the TrustyAIService CR
			instance.Annotations = map[string]string{
				kserveLoggerHTTPAnnotationKey: "true",
			}

			// Create a Raw deployment InferenceService
			infService := &kservev1beta1.InferenceService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "raw-model-http",
					Namespace: namespace,
					Annotations: map[string]string{
						"serving.kserve.io/deploymentMode": DEPLOYMENT_MODE_RAW,
					},
				},
				Spec: kservev1beta1.InferenceServiceSpec{
					Predictor: kservev1beta1.PredictorSpec{
						Model: &kservev1beta1.ModelSpec{
							ModelFormat: kservev1beta1.ModelFormat{Name: "sklearn"},
						},
					},
				},
			}
			Expect(testReconciler.Client.Create(testCtx, infService)).To(Succeed())

			_, err := testReconciler.handleInferenceServices(testCtx, instance, namespace, modelMeshLabelKey, modelMeshLabelValue, payloadProcessorName, instance.Name, false)
			Expect(err).ToNot(HaveOccurred())

			// Fetch updated InferenceService — should have HTTP URL
			updated := &kservev1beta1.InferenceService{}
			Expect(testReconciler.Client.Get(testCtx, types.NamespacedName{Name: "raw-model-http", Namespace: namespace}, updated)).To(Succeed())

			Expect(updated.Spec.Predictor.Logger).NotTo(BeNil())
			Expect(*updated.Spec.Predictor.Logger.URL).To(HavePrefix("http://"))
			expectedURL := utils.GenerateKServeLoggerURL(instance.Name, namespace)
			Expect(*updated.Spec.Predictor.Logger.URL).To(Equal(expectedURL))
		})
	})

	Context("When handleInferenceServices processes a Raw deployment with annotation set to non-true value", func() {
		It("should default to HTTPS logger URL", func() {
			namespace := "trusty-kserve-raw-nontrue"
			Expect(createNamespace(testCtx, testReconciler.Client, namespace)).To(Succeed())

			instance := createDefaultPVCCustomResource(namespace)
			// Set annotation to a non-"true" value
			instance.Annotations = map[string]string{
				kserveLoggerHTTPAnnotationKey: "false",
			}

			infService := &kservev1beta1.InferenceService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "raw-model-nontrue",
					Namespace: namespace,
					Annotations: map[string]string{
						"serving.kserve.io/deploymentMode": DEPLOYMENT_MODE_RAW,
					},
				},
				Spec: kservev1beta1.InferenceServiceSpec{
					Predictor: kservev1beta1.PredictorSpec{
						Model: &kservev1beta1.ModelSpec{
							ModelFormat: kservev1beta1.ModelFormat{Name: "sklearn"},
						},
					},
				},
			}
			Expect(testReconciler.Client.Create(testCtx, infService)).To(Succeed())

			_, err := testReconciler.handleInferenceServices(testCtx, instance, namespace, modelMeshLabelKey, modelMeshLabelValue, payloadProcessorName, instance.Name, false)
			Expect(err).ToNot(HaveOccurred())

			updated := &kservev1beta1.InferenceService{}
			Expect(testReconciler.Client.Get(testCtx, types.NamespacedName{Name: "raw-model-nontrue", Namespace: namespace}, updated)).To(Succeed())

			Expect(updated.Spec.Predictor.Logger).NotTo(BeNil())
			Expect(*updated.Spec.Predictor.Logger.URL).To(HavePrefix("https://"))
		})
	})
})

package images

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("Image Resolver", func() {
	var cleanupEnvVars = func() {
		_ = os.Unsetenv(RelatedImageTrustyAIService)
	}

	AfterEach(func() {
		cleanupEnvVars()
	})

	Describe("ResolveImage", func() {
		Context("when environment variable is set", func() {
			It("should use the environment variable value over ConfigMap", func() {
				// Create a fake client with a ConfigMap containing an image
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testConfigMap,
						Namespace: testNamespace,
					},
					Data: map[string]string{
						TrustyAIServiceImageKey: testImageFromCM,
					},
				}

				fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(configMap).Build()

				// Set the environment variable
				Expect(os.Setenv(RelatedImageTrustyAIService, testImageFromEnv)).To(Succeed())

				// Resolve the image
				image, err := ResolveImage(ctx, fakeClient, TrustyAIServiceImageKey, testConfigMap, testNamespace, "")

				Expect(err).NotTo(HaveOccurred())
				Expect(image).To(Equal(testImageFromEnv))
			})
		})

		Context("when environment variable is not set", func() {
			It("should use ConfigMap value", func() {
				// Create a fake client with a ConfigMap
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testConfigMap,
						Namespace: testNamespace,
					},
					Data: map[string]string{
						TrustyAIServiceImageKey: testImageFromCM,
					},
				}

				fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(configMap).Build()

				// Ensure env var is NOT set
				cleanupEnvVars()

				// Resolve the image
				image, err := ResolveImage(ctx, fakeClient, TrustyAIServiceImageKey, testConfigMap, testNamespace, "")

				Expect(err).NotTo(HaveOccurred())
				Expect(image).To(Equal(testImageFromCM))
			})
		})

		Context("when environment variable is empty", func() {
			It("should use ConfigMap value", func() {
				// Create a fake client with a ConfigMap
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testConfigMap,
						Namespace: testNamespace,
					},
					Data: map[string]string{
						TrustyAIServiceImageKey: testImageFromCM,
					},
				}

				fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(configMap).Build()

				// Set env var to empty string
				Expect(os.Setenv(RelatedImageTrustyAIService, "")).To(Succeed())

				// Resolve the image
				image, err := ResolveImage(ctx, fakeClient, TrustyAIServiceImageKey, testConfigMap, testNamespace, "")

				Expect(err).NotTo(HaveOccurred())
				Expect(image).To(Equal(testImageFromCM))
			})
		})

		Context("when neither env var nor ConfigMap are available", func() {
			It("should use fallback value", func() {
				// Create a fake client with NO ConfigMap
				fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

				// Ensure env var is NOT set
				cleanupEnvVars()

				// Resolve the image with fallback
				image, err := ResolveImage(ctx, fakeClient, TrustyAIServiceImageKey, testConfigMap, testNamespace, testFallback)

				Expect(err).NotTo(HaveOccurred())
				Expect(image).To(Equal(testFallback))
			})

			It("should return error when no fallback is provided", func() {
				// Create a fake client with NO ConfigMap
				fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

				// Ensure env var is NOT set
				cleanupEnvVars()

				// Resolve the image without fallback
				_, err := ResolveImage(ctx, fakeClient, TrustyAIServiceImageKey, testConfigMap, testNamespace, "")

				Expect(err).To(HaveOccurred())
			})
		})

		Context("when ConfigMap exists but is missing the key", func() {
			It("should use fallback value", func() {
				// Create a ConfigMap without the expected key
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testConfigMap,
						Namespace: testNamespace,
					},
					Data: map[string]string{
						"some-other-key": "some-value",
					},
				}

				fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(configMap).Build()

				// Ensure env var is NOT set
				cleanupEnvVars()

				// Resolve the image with fallback
				image, err := ResolveImage(ctx, fakeClient, TrustyAIServiceImageKey, testConfigMap, testNamespace, testFallback)

				Expect(err).NotTo(HaveOccurred())
				Expect(image).To(Equal(testFallback))
			})
		})
	})

	Describe("Image Mappings", func() {
		It("should have all 10 expected image mappings", func() {
			expectedMappings := map[string]string{
				TrustyAIServiceImageKey:           RelatedImageTrustyAIService,
				EvalHubImageKey:                   RelatedImageEvalHub,
				KubeRBACProxyKey:                  RelatedImageKubeRBACProxy,
				LMESPodImageKey:                   RelatedImageLMESJob,
				LMESDriverImageKey:                RelatedImageLMESDriver,
				GuardrailsOrchestratorImageKey:    RelatedImageGuardrailsOrchestrator,
				GuardrailsBuiltInDetectorImageKey: RelatedImageBuiltInDetector,
				GuardrailsSidecarGatewayImageKey:  RelatedImageVLLMOrchestratorGateway,
				GarakProviderImageKey:             RelatedImageGarakLLSProviderDSP,
				NemoGuardrailsImageKey:            RelatedImageNemoGuardrailsServer,
			}

			Expect(imageMapping).To(HaveLen(len(expectedMappings)))

			for key, expectedEnvVar := range expectedMappings {
				Expect(imageMapping).To(HaveKeyWithValue(key, expectedEnvVar))
			}
		})
	})

	Describe("Legacy Functions", func() {
		Describe("GetImageFromConfigMap", func() {
			It("should maintain backward compatibility", func() {
				// Create a ConfigMap
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testConfigMap,
						Namespace: testNamespace,
					},
					Data: map[string]string{
						TrustyAIServiceImageKey: testImageFromCM,
					},
				}

				fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(configMap).Build()

				// Ensure env var is NOT set
				cleanupEnvVars()

				// Call the legacy function
				image, err := GetImageFromConfigMap(ctx, fakeClient, TrustyAIServiceImageKey, testConfigMap, testNamespace)

				Expect(err).NotTo(HaveOccurred())
				Expect(image).To(Equal(testImageFromCM))
			})
		})

		Describe("GetImageFromConfigMapWithFallback", func() {
			It("should use fallback when ConfigMap is missing", func() {
				// Create a fake client with NO ConfigMap
				fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

				// Ensure env var is NOT set
				cleanupEnvVars()

				// Call the legacy function with fallback
				image, err := GetImageFromConfigMapWithFallback(ctx, fakeClient, TrustyAIServiceImageKey, testConfigMap, testNamespace, testFallback)

				Expect(err).NotTo(HaveOccurred())
				Expect(image).To(Equal(testFallback))
			})

			It("should return fallback immediately when namespace is empty", func() {
				fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

				// When namespace is empty, should return fallback immediately
				image, err := GetImageFromConfigMapWithFallback(ctx, fakeClient, TrustyAIServiceImageKey, testConfigMap, "", testFallback)

				Expect(err).NotTo(HaveOccurred())
				Expect(image).To(Equal(testFallback))
			})
		})
	})
})

package controllers

import (
	"context"
	"fmt"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("ConfigMap tests", func() {

	BeforeEach(func() {
		recorder = record.NewFakeRecorder(10)
		k8sClient = fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
		reconciler = &TrustyAIServiceReconciler{
			Client:        k8sClient,
			Scheme:        scheme.Scheme,
			EventRecorder: recorder,
			Namespace:     operatorNamespace,
		}
		ctx = context.Background()
	})

	AfterEach(func() {
		// Attempt to delete the ConfigMap
		configMap := &corev1.ConfigMap{}
		err := k8sClient.Get(ctx, types.NamespacedName{
			Namespace: operatorNamespace,
			Name:      imageConfigMap,
		}, configMap)

		// If the ConfigMap exists, delete it
		if err == nil {
			Expect(k8sClient.Delete(ctx, configMap)).To(Succeed())
		} else if !apierrors.IsNotFound(err) {
			Fail(fmt.Sprintf("Unexpected error while getting ConfigMap: %s", err))
		}

		// Attempt to delete the custom ConfigMap
		configMapCustom := &corev1.ConfigMap{}
		errCustom := k8sClient.Get(ctx, types.NamespacedName{
			Namespace: operatorNamespace,
			Name:      imageConfigMapCustom,
		}, configMapCustom)

		// If the ConfigMap exists, delete it
		if errCustom == nil {
			Expect(k8sClient.Delete(ctx, configMapCustom)).To(Succeed())
		} else if !apierrors.IsNotFound(errCustom) {
			Fail(fmt.Sprintf("Unexpected error while getting custom ConfigMap: %s", err))
		}
	})

	Context("When deploying a stock ConfigMap to the operator's namespace", func() {

		It("Should get back the correct values", func() {

			serviceImage := "custom-service-image:foo"
			oauthImage := "custom-oauth-proxy:bar"

			WaitFor(func() error {
				configMap := createConfigMap(operatorNamespace, oauthImage, serviceImage, false)
				return k8sClient.Create(ctx, configMap)
			}, "failed to create ConfigMap")

			var actualOAuthImage string
			var actualServiceImage string

			WaitFor(func() error {
				var err error
				actualOAuthImage, err = reconciler.getImageFromConfigMap(ctx, configMapOAuthProxyImageKey, defaultOAuthProxyImage)
				return err
			}, "failed to get oauth image from ConfigMap")

			WaitFor(func() error {
				var err error
				actualServiceImage, err = reconciler.getImageFromConfigMap(ctx, configMapServiceImageKey, defaultImage)
				return err
			}, "failed to get service image from ConfigMap")

			Expect(actualOAuthImage).Should(Equal(oauthImage))
			Expect(actualServiceImage).Should(Equal(serviceImage))
		})
	})

	Context("When deploying a Custom ConfigMap to the operator's namespace", func() {

		It("Should get back the correct custom values, over the default values", func() {

			serviceImage := "custom-service-image:foo_custom"
			oauthImage := "custom-oauth-proxy:bar_custom"
			serviceImageStock := "custom-service-image:foo"
			oauthImageStock := "custom-oauth-proxy:bar"

			WaitFor(func() error {
				configMap := createConfigMap(operatorNamespace, oauthImage, serviceImage, true)
				return k8sClient.Create(ctx, configMap)
			}, "failed to create ConfigMap")

			WaitFor(func() error {
				configMap := createConfigMap(operatorNamespace, oauthImageStock, serviceImageStock, false)
				return k8sClient.Create(ctx, configMap)
			}, "failed to create ConfigMap")

			var actualOAuthImage string
			var actualServiceImage string

			WaitFor(func() error {
				var err error
				actualOAuthImage, err = reconciler.getImageFromConfigMap(ctx, configMapOAuthProxyImageKey, defaultOAuthProxyImage)
				return err
			}, "failed to get oauth image from ConfigMap")

			WaitFor(func() error {
				var err error
				actualServiceImage, err = reconciler.getImageFromConfigMap(ctx, configMapServiceImageKey, defaultImage)
				return err
			}, "failed to get service image from ConfigMap")

			Expect(actualOAuthImage).Should(Equal(oauthImage))
			Expect(actualServiceImage).Should(Equal(serviceImage))
		})
	})

	Context("When no ConfigMap in the operator's namespace", func() {

		It("Should get back the default values", func() {

			var actualOAuthImage string
			var actualServiceImage string

			WaitFor(func() error {
				var err error
				actualOAuthImage, err = reconciler.getImageFromConfigMap(ctx, configMapOAuthProxyImageKey, defaultOAuthProxyImage)
				return err
			}, "failed to get oauth image from ConfigMap")

			WaitFor(func() error {
				var err error
				actualServiceImage, err = reconciler.getImageFromConfigMap(ctx, configMapServiceImageKey, defaultImage)
				return err
			}, "failed to get service image from ConfigMap")

			Expect(actualOAuthImage).Should(Equal(defaultOAuthProxyImage))
			Expect(actualServiceImage).Should(Equal(defaultImage))
		})
	})

	Context("When deploying a stock ConfigMap to the operator's namespace with the wrong keys", func() {

		It("Should get back the default values", func() {

			serviceImage := "custom-service-image:foo_custom"
			oauthImage := "custom-oauth-proxy:bar_custom"

			// create default CM
			WaitFor(func() error {
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      imageConfigMap,
						Namespace: operatorNamespace,
					},
					Data: map[string]string{
						"foo-oauth-image": oauthImage,
						"foo-image":       serviceImage,
					},
				}
				return k8sClient.Create(ctx, configMap)
			}, "failed to create ConfigMap")

			var actualOAuthImage string
			var actualServiceImage string

			configMapPath := operatorNamespace + "/" + imageConfigMap

			Eventually(func() error {
				var err error
				actualOAuthImage, err = reconciler.getImageFromConfigMap(ctx, configMapOAuthProxyImageKey, defaultOAuthProxyImage)
				return err
			}, defaultTimeout, defaultPolling).Should(MatchError(fmt.Sprintf("configmap %s does not contain necessary keys", configMapPath)), "failed to get oauth image from ConfigMap")

			Eventually(func() error {
				var err error
				actualServiceImage, err = reconciler.getImageFromConfigMap(ctx, configMapServiceImageKey, defaultImage)
				return err
			}, defaultTimeout, defaultPolling).Should(MatchError(fmt.Sprintf("configmap %s does not contain necessary keys", configMapPath)), "failed to get oauth image from ConfigMap")

			Expect(actualOAuthImage).Should(Equal(defaultOAuthProxyImage))
			Expect(actualServiceImage).Should(Equal(defaultImage))
		})
	})

	Context("When deploying a custom ConfigMap to the operator's namespace with the wrong keys, and a valid stock CM", func() {

		It("Should get back the stock CM values", func() {

			serviceImage := "custom-service-image:foo"
			oauthImage := "custom-oauth-proxy:bar"
			stockServiceImage := "stock-service-image:foo"
			stockOauthImage := "stock-oauth-proxy:bar"

			// create invalid custom CM
			WaitFor(func() error {
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      imageConfigMapCustom,
						Namespace: operatorNamespace,
					},
					Data: map[string]string{
						"foo-oauth-image": oauthImage,
						"foo-image":       serviceImage,
					},
				}
				return k8sClient.Create(ctx, configMap)
			}, "failed to create ConfigMap")

			// create valid stock CM
			WaitFor(func() error {
				configMap := createConfigMap(operatorNamespace, stockOauthImage, stockServiceImage, false)
				return k8sClient.Create(ctx, configMap)
			}, "failed to create ConfigMap")

			var actualOAuthImage string
			var actualServiceImage string

			configMapPathCustom := operatorNamespace + "/" + imageConfigMapCustom
			configMapPath := operatorNamespace + "/" + imageConfigMap

			Eventually(func() error {
				var err error
				actualOAuthImage, err = reconciler.getImageFromConfigMap(ctx, configMapOAuthProxyImageKey, defaultOAuthProxyImage)
				return err
			}, defaultTimeout, defaultPolling).Should(MatchError(fmt.Sprintf("configmap %s does not contain necessary keys, failing back to %s", configMapPathCustom, configMapPath)), "failed to get oauth image from ConfigMap")

			Eventually(func() error {
				var err error
				actualServiceImage, err = reconciler.getImageFromConfigMap(ctx, configMapServiceImageKey, defaultImage)
				return err
			}, defaultTimeout, defaultPolling).Should(MatchError(fmt.Sprintf("configmap %s does not contain necessary keys, failing back to %s", configMapPathCustom, configMapPath)), "failed to get oauth image from ConfigMap")

			Expect(actualOAuthImage).Should(Equal(stockOauthImage))
			Expect(actualServiceImage).Should(Equal(stockServiceImage))
		})
	})

	Context("When neither the custom nor Stock ConfigMap in the operator's namespace has valid keys", func() {

		It("Should get back the default values", func() {

			serviceImage := "custom-service-image:foo"
			oauthImage := "custom-oauth-proxy:bar"
			stockServiceImage := "stock-service-image:foo"
			stockOauthImage := "stock-oauth-proxy:bar"

			// create invalid custom CM
			WaitFor(func() error {
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      imageConfigMapCustom,
						Namespace: operatorNamespace,
					},
					Data: map[string]string{
						"foo-oauth-image": oauthImage,
						"foo-image":       serviceImage,
					},
				}
				return k8sClient.Create(ctx, configMap)
			}, "failed to create ConfigMap")

			// create invalid stock CM
			WaitFor(func() error {
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      imageConfigMap,
						Namespace: operatorNamespace,
					},
					Data: map[string]string{
						"foo-oauth-image": stockOauthImage,
						"foo-image":       stockServiceImage,
					},
				}
				return k8sClient.Create(ctx, configMap)
			}, "failed to create ConfigMap")

			var actualOAuthImage string
			var actualServiceImage string

			configMapPathCustom := operatorNamespace + "/" + imageConfigMapCustom
			configMapPath := operatorNamespace + "/" + imageConfigMap

			Eventually(func() error {
				var err error
				actualOAuthImage, err = reconciler.getImageFromConfigMap(ctx, configMapOAuthProxyImageKey, defaultOAuthProxyImage)
				return err
			}, defaultTimeout, defaultPolling).Should(MatchError(fmt.Sprintf("configmap %s does not contain necessary keys, failing back to %s. While retrieving %s, another error occured: configmap %s does not contain necessary keys", configMapPathCustom, configMapPath, configMapPath, configMapPath)), "failed to get oauth image from ConfigMap")

			Eventually(func() error {
				var err error
				actualServiceImage, err = reconciler.getImageFromConfigMap(ctx, configMapServiceImageKey, defaultImage)
				return err
			}, defaultTimeout, defaultPolling).Should(MatchError(fmt.Sprintf("configmap %s does not contain necessary keys, failing back to %s. While retrieving %s, another error occured: configmap %s does not contain necessary keys", configMapPathCustom, configMapPath, configMapPath, configMapPath)), "failed to get oauth image from ConfigMap")

			Expect(actualOAuthImage).Should(Equal(defaultOAuthProxyImage))
			Expect(actualServiceImage).Should(Equal(defaultImage))
		})
	})
})

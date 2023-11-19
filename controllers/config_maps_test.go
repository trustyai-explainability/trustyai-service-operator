package controllers

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

	Context("When deploying a ConfigMap to the operator's namespace", func() {

		It("Should get back the correct values", func() {

			serviceImage := "custom-service-image:foo"
			oauthImage := "custom-oauth-proxy:bar"
			WaitFor(func() error {
				configMap := createConfigMap(operatorNamespace, oauthImage, serviceImage)
				return k8sClient.Create(ctx, configMap)
			}, "failed to create ConfigMap")

			var actualOAuthImage string

			WaitFor(func() error {
				var err error
				actualOAuthImage, err = reconciler.getImageFromConfigMap(ctx, oauthImage, defaultOAuthProxyImage)
				return err
			}, "failed to get image from ConfigMap")

			Expect(actualOAuthImage).Should(Equal(oauthImage))

		})
	})

})

package tas

import (
	"context"
	"fmt"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
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
			Name:      constants.ConfigMap,
		}, configMap)

		// If the ConfigMap exists, delete it
		if err == nil {
			Expect(k8sClient.Delete(ctx, configMap)).To(Succeed())
		} else if !apierrors.IsNotFound(err) {
			Fail(fmt.Sprintf("Unexpected error while getting ConfigMap: %s", err))
		}
	})

	Context("When deploying a ConfigMap to the operator's namespace", func() {

		It("Should get back the correct values", func() {

			serviceImage := "custom-service-image:foo"
			kubeRBACProxyImage := "custom-kube-rbac-proxy:bar"

			WaitFor(func() error {
				configMap := createConfigMap(operatorNamespace, kubeRBACProxyImage, serviceImage)
				return k8sClient.Create(ctx, configMap)
			}, "failed to create ConfigMap")

			var actualKubeRBACProxyImage string
			var actualServiceImage string

			WaitFor(func() error {
				var err error
				actualKubeRBACProxyImage, err = utils.GetImageFromConfigMapWithFallback(ctx, k8sClient, configMapKubeRBACProxyImageKey, constants.ConfigMap, operatorNamespace, defaultKubeRBACProxyImage)
				return err
			}, "failed to get kube-rbac-proxy image from ConfigMap")

			WaitFor(func() error {
				var err error
				actualServiceImage, err = utils.GetImageFromConfigMapWithFallback(ctx, k8sClient, configMapServiceImageKey, constants.ConfigMap, operatorNamespace, defaultImage)
				return err
			}, "failed to get service image from ConfigMap")

			Expect(actualKubeRBACProxyImage).Should(Equal(kubeRBACProxyImage))
			Expect(actualServiceImage).Should(Equal(serviceImage))
		})
	})

	Context("When no ConfigMap in the operator's namespace", func() {

		It("Should get back the default values", func() {

			var actualKubeRBACProxyImage string
			var actualServiceImage string

			WaitFor(func() error {
				var err error
				actualKubeRBACProxyImage, err = utils.GetImageFromConfigMapWithFallback(ctx, k8sClient, configMapKubeRBACProxyImageKey, constants.ConfigMap, operatorNamespace, defaultKubeRBACProxyImage)
				if err != nil {
					Expect(err).To(MatchError(fmt.Sprintf("configmap %s not found in namespace %s", constants.ConfigMap, operatorNamespace)))
					return nil
				}
				return nil
			}, "failed to get kube-rbac-proxy image from ConfigMap")

			WaitFor(func() error {
				var err error
				actualServiceImage, err = utils.GetImageFromConfigMapWithFallback(ctx, k8sClient, configMapServiceImageKey, constants.ConfigMap, operatorNamespace, defaultImage)
				if err != nil {
					Expect(err).To(MatchError(fmt.Sprintf("configmap %s not found in namespace %s", constants.ConfigMap, operatorNamespace)))
					return nil
				}
				return nil
			}, "failed to get service image from ConfigMap")

			Expect(actualKubeRBACProxyImage).Should(Equal(defaultKubeRBACProxyImage))
			Expect(actualServiceImage).Should(Equal(defaultImage))
		})
	})

	Context("When deploying a ConfigMap to the operator's namespace with the wrong keys", func() {

		It("Should get back the default values", func() {

			serviceImage := "custom-service-image:foo"
			kubeRBACProxyImage := "custom-kube-rbac-proxy:bar"

			WaitFor(func() error {
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      constants.ConfigMap,
						Namespace: operatorNamespace,
					},
					Data: map[string]string{
						"foo-kube-rbac-proxy-image": kubeRBACProxyImage,
						"foo-image":                 serviceImage,
					},
				}
				return k8sClient.Create(ctx, configMap)
			}, "failed to create ConfigMap")

			var actualKubeRBACProxyImage string
			var actualServiceImage string

			Eventually(func() error {
				var err error
				actualKubeRBACProxyImage, err = utils.GetImageFromConfigMapWithFallback(ctx, k8sClient, configMapKubeRBACProxyImageKey, constants.ConfigMap, operatorNamespace, defaultKubeRBACProxyImage)
				return err
			}, defaultTimeout, defaultPolling).Should(MatchError(fmt.Sprintf("configmap %s in namespace %s does not contain key %s", constants.ConfigMap, operatorNamespace, configMapKubeRBACProxyImageKey)), "failed to get kube-rbac-proxy image from ConfigMap")

			Eventually(func() error {
				var err error
				actualServiceImage, err = utils.GetImageFromConfigMapWithFallback(ctx, k8sClient, configMapServiceImageKey, constants.ConfigMap, operatorNamespace, defaultImage)
				return err
			}, defaultTimeout, defaultPolling).Should(MatchError(fmt.Sprintf("configmap %s in namespace %s does not contain key %s", constants.ConfigMap, operatorNamespace, configMapServiceImageKey)), "failed to get service image from ConfigMap")

			Expect(actualKubeRBACProxyImage).Should(Equal(defaultKubeRBACProxyImage))
			Expect(actualServiceImage).Should(Equal(defaultImage))
		})
	})

})

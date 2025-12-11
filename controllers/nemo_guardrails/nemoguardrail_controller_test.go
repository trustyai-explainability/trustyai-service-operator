package nemo_guardrails

import (
	"context"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("NemoGuardrails Controller", func() {
	const (
		resourceName      = "test-nemoguardrails"
		namespace         = "test"
		operatorNamespace = "operator-ns"
	)
	var (
		ctx                = context.Background()
		typeNamespacedName = types.NamespacedName{
			Name:      resourceName,
			Namespace: namespace,
		}
	)

	BeforeEach(func() {
		By("creating the test namespace")
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		err := k8sClient.Create(ctx, ns)
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}

		By("creating the operator namespace")
		op_ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: operatorNamespace,
			},
		}
		err = k8sClient.Create(ctx, op_ns)
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}

		By("creating the trustyai-service-operator-config ConfigMap required by NemoGuardrails")
		operatorConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      constants.ConfigMap,
				Namespace: operatorNamespace,
			},
			Data: map[string]string{
				nemoGuardrailsImageKey: "quay.io/trustyai/nemo-guardrails-server:latest",
			},
		}
		err = k8sClient.Create(ctx, operatorConfigMap)
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}

		By("creating the nemo-config ConfigMap required by NemoGuardrails")
		nemoConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "nemo-config",
				Namespace: namespace,
			},
			Data: map[string]string{
				"actions.py":  "# dummy actions",
				"config.yaml": "dummy: config",
			},
		}
		err = k8sClient.Create(ctx, nemoConfigMap)
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}

		By("creating the custom resource for the Kind NemoGuardrails")
		nemoguardrail := &nemoguardrailsv1alpha1.NemoGuardrails{}
		err = k8sClient.Get(ctx, typeNamespacedName, nemoguardrail)
		if err != nil && errors.IsNotFound(err) {
			resource := &nemoguardrailsv1alpha1.NemoGuardrails{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
				},
				Spec: nemoguardrailsv1alpha1.NemoGuardrailsSpec{
					NemoConfigs: []nemoguardrailsv1alpha1.NemoConfig{
						{
							Name:       "nemo-config",
							ConfigMaps: []string{"nemo-config"},
							Default:    true,
						},
					},
					Env: []corev1.EnvVar{
						{Name: "LOG_LEVEL", Value: "DEBUG"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		}
	})

	AfterEach(func() {
		resource := &nemoguardrailsv1alpha1.NemoGuardrails{}
		err := k8sClient.Get(ctx, typeNamespacedName, resource)
		if err == nil {
			By("Cleanup the specific resource instance NemoGuardrails")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		}
	})

	It("should successfully reconcile the resource and create a Deployment", func() {
		By("Reconciling the created resource")
		controllerReconciler := &NemoGuardrailsReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			Namespace: operatorNamespace,
		}

		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Checking if the Deployment was created")
		Eventually(func() error {
			deployment := &appsv1.Deployment{}
			return k8sClient.Get(ctx, typeNamespacedName, deployment)
		}, time.Second*10, time.Millisecond*100).Should(Succeed())

		By("Checking if the Service was created")
		Eventually(func() error {
			service := &corev1.Service{}
			return k8sClient.Get(ctx, typeNamespacedName, service)
		}, time.Second*10, time.Millisecond*100).Should(Succeed())
	})

	It("should update the CA status after reconciliation", func() {
		controllerReconciler := &NemoGuardrailsReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			Namespace: operatorNamespace,
		}
		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Checking if the CA status is set")
		Eventually(func() bool {
			nemo := &nemoguardrailsv1alpha1.NemoGuardrails{}
			err := k8sClient.Get(ctx, typeNamespacedName, nemo)
			if err != nil {
				return false
			}
			return nemo.Status.CA != nil
		}, time.Second*10, time.Millisecond*100).Should(BeTrue())
	})

	It("should add user environment variables to the deployment", func() {
		controllerReconciler := &NemoGuardrailsReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			Namespace: operatorNamespace,
		}
		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Checking if the Deployment contains the user-provided environment variable")
		Eventually(func() bool {
			deployment := &appsv1.Deployment{}
			err := k8sClient.Get(ctx, typeNamespacedName, deployment)
			if err != nil {
				return false
			}
			for _, env := range deployment.Spec.Template.Spec.Containers[0].Env {
				if env.Name == "LOG_LEVEL" && env.Value == "DEBUG" {
					return true
				}
			}
			return false
		}, time.Second*10, time.Millisecond*100).Should(BeTrue())
	})

	It("should set CONFIG_ID env var to the default config", func() {
		controllerReconciler := &NemoGuardrailsReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			Namespace: operatorNamespace,
		}
		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Checking if the Deployment contains the CONFIG_ID environment variable")
		Eventually(func() bool {
			deployment := &appsv1.Deployment{}
			err := k8sClient.Get(ctx, typeNamespacedName, deployment)
			if err != nil {
				return false
			}
			for _, env := range deployment.Spec.Template.Spec.Containers[0].Env {
				if env.Name == "CONFIG_ID" && env.Value == "nemo-config" {
					return true
				}
			}
			return false
		}, time.Second*10, time.Millisecond*100).Should(BeTrue())
	})
})

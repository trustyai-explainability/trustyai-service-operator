package tas

import (
	"context"
	"strings"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("TrustyAI database tuning ConfigMap", func() {
	BeforeEach(func() {
		ctx = context.Background()
		recorder = record.NewFakeRecorder(10)
		k8sClient = fake.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithStatusSubresource(&trustyaiopendatahubiov1.TrustyAIService{}).
			Build()
		reconciler = &TrustyAIServiceReconciler{
			Client:        k8sClient,
			Scheme:        scheme.Scheme,
			EventRecorder: recorder,
			Namespace:     operatorNamespace,
		}
	})

	It("accepts storage.databaseTuning through the generated CRD", func() {
		apiClient, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
		Expect(err).NotTo(HaveOccurred())

		namespace := "database-tuning-crd-" + uuid.NewString()[:8]
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
		Expect(apiClient.Create(ctx, ns)).To(Succeed())
		DeferCleanup(func() {
			Expect(apiClient.Delete(context.Background(), ns)).To(Succeed())
		})
		Expect(apiClient.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "db-tuning", Namespace: namespace},
			Data:       map[string]string{"DATABASE_POOL_SIZE": "5"},
		})).To(Succeed())

		instance := createDefaultDBCustomResource(namespace)
		instance.Spec.Storage.DatabaseTuning = "db-tuning"
		Expect(apiClient.Create(ctx, instance)).To(Succeed())

		created := &trustyaiopendatahubiov1.TrustyAIService{}
		Expect(apiClient.Get(ctx, client.ObjectKeyFromObject(instance), created)).To(Succeed())
		Expect(created.Spec.Storage.DatabaseTuning).To(Equal("db-tuning"))
	})

	It("imports the ConfigMap and rolls out updated values while preserving Secret-backed credentials", func() {
		instance := createDefaultDBCustomResource("database-tuning-rollout")
		instance.Spec.Storage.DatabaseTuning = "db-tuning"
		Expect(k8sClient.Create(ctx, createConfigMap(operatorNamespace, testKubeRBACProxyImage, testTrustAIServiceImage))).To(Succeed())
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: instance.Spec.Storage.DatabaseTuning, Namespace: instance.Namespace},
			Data:       map[string]string{"DATABASE_POOL_SIZE": "7"},
		}
		Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

		Expect(reconciler.ensureDeployment(ctx, instance, CustomCertificatesBundle{}, false)).To(Succeed())
		deployment := &appsv1.Deployment{}
		key := types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}
		Expect(k8sClient.Get(ctx, key, deployment)).To(Succeed())
		service := serviceContainer(deployment)
		Expect(service).NotTo(BeNil())
		Expect(service.EnvFrom).To(ContainElement(corev1.EnvFromSource{
			ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "db-tuning"}},
		}))
		password := findDatabaseEnv(service.Env, "QUARKUS_DATASOURCE_PASSWORD")
		Expect(password).NotTo(BeNil())
		Expect(password.ValueFrom.SecretKeyRef.Name).To(Equal(defaultDatabaseConfigurationName))
		Expect(password.ValueFrom.SecretKeyRef.Key).To(Equal("databasePassword"))
		oldHash := deployment.Spec.Template.Annotations[databaseTuningHashAnnotation]
		Expect(oldHash).NotTo(BeEmpty())

		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(configMap), configMap)).To(Succeed())
		configMap.Data["DATABASE_POOL_SIZE"] = "9"
		Expect(k8sClient.Update(ctx, configMap)).To(Succeed())
		Expect(reconciler.ensureDeployment(ctx, instance, CustomCertificatesBundle{}, false)).To(Succeed())
		Expect(k8sClient.Get(ctx, key, deployment)).To(Succeed())
		newHash := deployment.Spec.Template.Annotations[databaseTuningHashAnnotation]
		Expect(newHash).NotTo(BeEmpty())
		Expect(newHash).NotTo(Equal(oldHash))
	})

	It("reports a missing ConfigMap in status and returns a useful error", func() {
		instance := createDefaultDBCustomResource("database-tuning-missing")
		instance.Spec.Storage.DatabaseTuning = "missing-db-tuning"
		Expect(k8sClient.Create(ctx, instance)).To(Succeed())

		err := reconciler.validateAndSetDatabaseTuningStatus(ctx, instance)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("ConfigMap \"missing-db-tuning\""))
		Expect(err.Error()).To(ContainSubstring("does not exist"))

		saved := &trustyaiopendatahubiov1.TrustyAIService{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(instance), saved)).To(Succeed())
		condition := databaseTuningCondition(saved)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal("InvalidReference"))
		Expect(condition.Message).To(ContainSubstring("missing-db-tuning"))
		Expect(saved.Status.Ready).To(Equal(corev1.ConditionFalse))
	})

	It("rejects ConfigMaps that would override database credentials or contain unusable keys", func() {
		instance := createDefaultDBCustomResource("database-tuning-invalid")
		instance.Spec.Storage.DatabaseTuning = "bad-db-tuning"
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: instance.Spec.Storage.DatabaseTuning, Namespace: instance.Namespace},
			Data:       map[string]string{"QUARKUS_DATASOURCE_PASSWORD": "plain-text", "DATABASE_POOL_SIZE": "5"},
		}
		Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

		_, err := reconciler.getDatabaseTuningConfigMap(ctx, instance)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("keep connection settings and secrets in storage.databaseConfigurations"))

		configMap.Data = map[string]string{"DATABASE=POOL": "5"}
		Expect(k8sClient.Update(ctx, configMap)).To(Succeed())
		_, err = reconciler.getDatabaseTuningConfigMap(ctx, instance)
		Expect(err).To(HaveOccurred())
		Expect(strings.ToLower(err.Error())).To(ContainSubstring("cannot be imported as an environment variable"))
	})
})

func findDatabaseEnv(env []corev1.EnvVar, name string) *corev1.EnvVar {
	for i := range env {
		if env[i].Name == name {
			return &env[i]
		}
	}
	return nil
}

func databaseTuningCondition(instance *trustyaiopendatahubiov1.TrustyAIService) *trustyaiopendatahubiov1.TASCondition {
	for i := range instance.Status.Conditions {
		if instance.Status.Conditions[i].Type == ConditionTypeDatabaseTuningAvailable {
			return &instance.Status.Conditions[i]
		}
	}
	return nil
}

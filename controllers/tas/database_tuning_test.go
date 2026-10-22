package tas

import (
	"context"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
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

		crd := &apiextensionsv1.CustomResourceDefinition{}
		Expect(apiClient.Get(ctx, client.ObjectKey{Name: "trustyaiservices.trustyai.opendatahub.io"}, crd)).To(Succeed())
		v1HasDatabaseTuning := false
		v1alpha1HasDatabaseTuning := false
		for _, version := range crd.Spec.Versions {
			storageSchema := version.Schema.OpenAPIV3Schema.Properties["spec"].Properties["storage"]
			_, hasDatabaseTuning := storageSchema.Properties["databaseTuning"]
			switch version.Name {
			case "v1":
				v1HasDatabaseTuning = hasDatabaseTuning
			case "v1alpha1":
				v1alpha1HasDatabaseTuning = hasDatabaseTuning
			}
		}
		Expect(v1HasDatabaseTuning).To(BeTrue())
		Expect(v1alpha1HasDatabaseTuning).To(BeFalse())
	})

	It("imports the ConfigMap and leaves updated values for a manual rollout", func() {
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
		Expect(service.EnvFrom).To(BeEmpty())
		for key := range supportedDatabaseTuningKeys {
			tuningEnv := findDatabaseEnv(service.Env, key)
			Expect(tuningEnv).NotTo(BeNil(), key)
			Expect(tuningEnv.ValueFrom.ConfigMapKeyRef.Name).To(Equal("db-tuning"), key)
			Expect(tuningEnv.ValueFrom.ConfigMapKeyRef.Key).To(Equal(key), key)
			Expect(tuningEnv.ValueFrom.ConfigMapKeyRef.Optional).NotTo(BeNil(), key)
			Expect(*tuningEnv.ValueFrom.ConfigMapKeyRef.Optional).To(BeTrue(), key)
		}
		Expect(findDatabaseEnv(service.Env, "UNSUPPORTED_CUSTOM_SETTING")).To(BeNil())
		password := findDatabaseEnv(service.Env, "QUARKUS_DATASOURCE_PASSWORD")
		Expect(password).NotTo(BeNil())
		Expect(password.ValueFrom.SecretKeyRef.Name).To(Equal(defaultDatabaseConfigurationName))
		Expect(password.ValueFrom.SecretKeyRef.Key).To(Equal("databasePassword"))
		originalTemplate := deployment.Spec.Template.DeepCopy()

		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(configMap), configMap)).To(Succeed())
		configMap.Data["DATABASE_POOL_SIZE"] = "9"
		Expect(k8sClient.Update(ctx, configMap)).To(Succeed())
		Expect(reconciler.ensureDeployment(ctx, instance, CustomCertificatesBundle{}, false)).To(Succeed())
		Expect(k8sClient.Get(ctx, key, deployment)).To(Succeed())
		Expect(deployment.Spec.Template).To(Equal(*originalTemplate))

		// A manual rollout annotation is left intact by subsequent reconciliation.
		const restartedAt = "2026-10-22T12:00:00Z"
		deployment.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = restartedAt
		Expect(k8sClient.Update(ctx, deployment)).To(Succeed())
		Expect(reconciler.ensureDeployment(ctx, instance, CustomCertificatesBundle{}, false)).To(Succeed())
		Expect(k8sClient.Get(ctx, key, deployment)).To(Succeed())
		Expect(deployment.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"]).To(Equal(restartedAt))
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
		Expect(condition.Reason).To(Equal("InvalidConfiguration"))
		Expect(condition.Message).To(ContainSubstring("missing-db-tuning"))
		Expect(saved.Status.Ready).To(Equal(corev1.ConditionFalse))
	})

	It("reports unsupported ConfigMap keys in status", func() {
		instance := createDefaultDBCustomResource("database-tuning-unsupported-key")
		instance.Spec.Storage.DatabaseTuning = "db-tuning"
		Expect(k8sClient.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "db-tuning", Namespace: instance.Namespace},
			Data:       map[string]string{"DATABASE_PASSWORD": "should-remain-secret-backed"},
		})).To(Succeed())
		Expect(k8sClient.Create(ctx, instance)).To(Succeed())

		err := reconciler.validateAndSetDatabaseTuningStatus(ctx, instance)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("DATABASE_PASSWORD"))

		saved := &trustyaiopendatahubiov1.TrustyAIService{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(instance), saved)).To(Succeed())
		condition := databaseTuningCondition(saved)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal("InvalidConfiguration"))
		Expect(condition.Message).To(ContainSubstring("DATABASE_PASSWORD"))
		Expect(saved.Status.Ready).To(Equal(corev1.ConditionFalse))
	})

	It("only allows database tuning ConfigMaps for DATABASE storage", func() {
		instance := createDefaultDBCustomResource("database-tuning-invalid")
		instance.Spec.Storage.DatabaseTuning = "db-tuning"
		instance.Spec.Storage.Format = "PVC"
		_, err := reconciler.getDatabaseTuningConfigMap(ctx, instance)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("only when storage.format is DATABASE"))
	})

	It("accepts documented tuning keys and rejects unrelated environment variables", func() {
		for key := range supportedDatabaseTuningKeys {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "db-tuning"},
				Data:       map[string]string{key: "value"},
			}
			Expect(validateDatabaseTuningConfigMap(configMap)).To(Succeed(), key)
		}

		for _, key := range []string{
			"DATABASE_ALLOW_INSECURE_TLS",
			"DATABASE_TLS_CA_CERT",
			"DATABASE_ATTEMPT_MIGRATION",
			"STORAGE_DATABASE_PATH",
			"UNSUPPORTED_CUSTOM_SETTING",
		} {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "db-tuning"},
				Data:       map[string]string{key: "value"},
			}
			err := validateDatabaseTuningConfigMap(configMap)
			Expect(err).To(HaveOccurred(), key)
			Expect(err.Error()).To(ContainSubstring(key))
		}
	})

	It("rejects binary ConfigMap data for database tuning", func() {
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "db-tuning"},
			BinaryData: map[string][]byte{"DATABASE_POOL_SIZE": []byte("5")},
		}

		err := validateDatabaseTuningConfigMap(configMap)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("binaryData"))
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

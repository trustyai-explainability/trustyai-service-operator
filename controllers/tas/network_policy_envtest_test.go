package tas

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("TAS NetworkPolicy controller lifecycle", func() {
	It("selects the Deployment and repairs policy drift and deletion", func() {
		// Use a private API client: other TAS specs replace the suite client with
		// fake clients. The manager started in BeforeSuite remains API-backed.
		apiClient, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
		Expect(err).NotTo(HaveOccurred())
		ctx := context.Background()
		namespace := "tas-networkpolicy-lifecycle"
		Expect(createNamespace(ctx, apiClient, namespace)).To(Succeed())
		owner := createDefaultDBCustomResource(namespace)
		owner.UID = ""
		Expect(apiClient.Create(ctx, createDatabaseConfiguration(namespace, defaultDatabaseConfigurationName, "mariadb", "trustyai"))).To(Succeed())
		Expect(apiClient.Create(ctx, owner)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(apiClient.Delete(ctx, owner))).To(Succeed())
			// envtest has no garbage collector; remove owned resources explicitly.
			Expect(apiClient.DeleteAllOf(ctx, &networkingv1.NetworkPolicy{}, client.InNamespace(namespace))).To(Succeed())
			Expect(apiClient.DeleteAllOf(ctx, &corev1.Pod{}, client.InNamespace(namespace))).To(Succeed())
			Expect(apiClient.DeleteAllOf(ctx, &appsv1.ReplicaSet{}, client.InNamespace(namespace))).To(Succeed())
			Expect(apiClient.DeleteAllOf(ctx, &appsv1.Deployment{}, client.InNamespace(namespace))).To(Succeed())
		})
		identity, err := tasNetworkPolicyIdentity(owner)
		Expect(err).NotTo(HaveOccurred())
		name, err := identity.Name(tasNetworkPolicyPurpose)
		Expect(err).NotTo(HaveOccurred())
		key := client.ObjectKey{Namespace: namespace, Name: name}
		policy := &networkingv1.NetworkPolicy{}
		Eventually(func() error { return apiClient.Get(ctx, key, policy) }, 30*time.Second, defaultPolling).Should(Succeed())
		Expect(metav1.IsControlledBy(policy, owner)).To(BeTrue())
		deployment := &appsv1.Deployment{}
		Eventually(func() error {
			return apiClient.Get(ctx, client.ObjectKeyFromObject(owner), deployment)
		}, 30*time.Second, defaultPolling).Should(Succeed())
		for label, value := range policy.Spec.PodSelector.MatchLabels {
			Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue(label, value))
			Expect(deployment.Spec.Selector.MatchLabels).NotTo(HaveKey(label))
		}
		// A metadata-only watch must still repair Pod identity drift. Real
		// ReplicaSet/Pod objects prove the mapper receives metadata events and
		// can verify the controller chain using uncached API reads.
		replicaSet := &appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{Name: owner.Name + "-watch", Namespace: namespace,
				OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(deployment, appsv1.SchemeGroupVersion.WithKind("Deployment"))}},
			Spec: appsv1.ReplicaSetSpec{Selector: deployment.Spec.Selector.DeepCopy(), Template: *deployment.Spec.Template.DeepCopy()},
		}
		Expect(apiClient.Create(ctx, replicaSet)).To(Succeed())
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: owner.Name + "-watch-pod", Namespace: namespace,
				Labels:          deployment.Spec.Template.DeepCopy().Labels,
				OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(replicaSet, appsv1.SchemeGroupVersion.WithKind("ReplicaSet"))}},
			Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "test", Image: "test"}}},
		}
		Expect(apiClient.Create(ctx, pod)).To(Succeed())
		Eventually(func() error {
			if err := apiClient.Get(ctx, client.ObjectKeyFromObject(pod), pod); err != nil {
				return err
			}
			delete(pod.Labels, utils.NetworkPolicyOwnerUIDLabel)
			return apiClient.Update(ctx, pod)
		}, defaultTimeout, defaultPolling).Should(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(apiClient.Get(ctx, client.ObjectKeyFromObject(pod), pod)).To(Succeed())
			g.Expect(pod.Labels).To(HaveKeyWithValue(utils.NetworkPolicyOwnerUIDLabel, policy.Spec.PodSelector.MatchLabels[utils.NetworkPolicyOwnerUIDLabel]))
		}, 30*time.Second, defaultPolling).Should(Succeed())
		want := policy.Spec.DeepCopy()
		Eventually(func() error {
			if err := apiClient.Get(ctx, key, policy); err != nil {
				return err
			}
			policy.Spec.Ingress = nil
			return apiClient.Update(ctx, policy)
		}, defaultTimeout, defaultPolling).Should(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(apiClient.Get(ctx, key, policy)).To(Succeed())
			g.Expect(policy.Spec).To(Equal(*want))
		}, 30*time.Second, defaultPolling).Should(Succeed())
		oldUID := policy.UID
		Expect(apiClient.Delete(ctx, policy)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(apiClient.Get(ctx, key, policy)).To(Succeed())
			g.Expect(policy.UID).NotTo(Equal(oldUID))
			g.Expect(policy.Spec).To(Equal(*want))
		}, 30*time.Second, defaultPolling).Should(Succeed())
	})
})

package lmes_test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	lmesv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/lmes/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/lmes"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("LMEval execution policy API convergence", func() {
	It("repairs policy deletion/drift and Pod identity drift for a retained completed job", func() {
		ctx := context.Background()
		job := &lmesv1alpha1.LMEvalJob{
			ObjectMeta: metav1.ObjectMeta{Name: "policy-watch-test", Namespace: testNamespace,
				Labels: map[string]string{lmes.LMEvalJobUIDLabel: "spoofed", utils.NetworkPolicyOwnerUIDLabel: "spoofed", utils.NetworkPolicyComponentLabel: "spoofed", utils.NetworkPolicyRoleLabel: "spoofed"}},
			Spec: lmesv1alpha1.LMEvalJobSpec{Model: "hf", ModelArgs: []lmesv1alpha1.Arg{{Name: "pretrained", Value: "google/flan-t5-small"}}, TaskList: lmesv1alpha1.TaskList{TaskNames: []string{"arc_easy"}}},
		}
		Expect(k8sClient.Create(ctx, job)).To(Succeed())
		key := client.ObjectKeyFromObject(job)
		Eventually(func() error {
			if err := k8sClient.Get(ctx, key, job); err != nil {
				return err
			}
			if job.Status.State != lmesv1alpha1.ScheduledJobState || !apimeta.IsStatusConditionTrue(job.Status.Conditions, lmes.NetworkPolicyReadyCondition) {
				return fmt.Errorf("job not scheduled/protected")
			}
			return nil
		}, defaultTimeout, defaultPolling).Should(Succeed())
		identity := utils.NetworkPolicyIdentity{OwnerKind: lmesv1alpha1.GroupVersion.WithKind(lmesv1alpha1.KindName).GroupKind(), OwnerUID: job.UID, Component: "lmes", Role: "evaluation"}
		name, err := identity.Name("execution")
		Expect(err).NotTo(HaveOccurred())
		policyKey := client.ObjectKey{Name: name, Namespace: testNamespace}
		policy := &networkingv1.NetworkPolicy{}
		Expect(k8sClient.Get(ctx, policyKey, policy)).To(Succeed())
		Expect(metav1.IsControlledBy(policy, job)).To(BeTrue())
		Expect(policy.Spec.PolicyTypes).To(Equal([]networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}))
		Expect(policy.Spec.Ingress).To(BeEmpty())
		Expect(policy.Spec.Egress).To(HaveLen(1))
		pod := &corev1.Pod{}
		Expect(k8sClient.Get(ctx, key, pod)).To(Succeed())
		Expect(pod.Labels[lmes.LMEvalJobUIDLabel]).To(Equal(string(job.UID)))
		Expect(pod.Labels[utils.NetworkPolicyOwnerUIDLabel]).To(Equal(string(job.UID)))
		Expect(pod.Labels[utils.NetworkPolicyComponentLabel]).To(Equal("lmes"))
		Expect(pod.Labels[utils.NetworkPolicyRoleLabel]).To(Equal("evaluation"))

		// Envtest has no kubelet. Record completion explicitly, then verify watches
		// still repair a retained execution without resetting its outcome/results.
		Eventually(func() error {
			if err := k8sClient.Get(ctx, key, job); err != nil {
				return err
			}
			job.Status.State = lmesv1alpha1.CompleteJobState
			job.Status.Reason = lmesv1alpha1.SucceedReason
			job.Status.Results = "retained-result"
			now := metav1.Now()
			job.Status.CompleteTime = &now
			return k8sClient.Status().Update(ctx, job)
		}, defaultTimeout, defaultPolling).Should(Succeed())
		for _, label := range []string{lmes.LMEvalJobUIDLabel, utils.NetworkPolicyOwnerUIDLabel, utils.NetworkPolicyComponentLabel, utils.NetworkPolicyRoleLabel} {
			expected := policy.Spec.PodSelector.MatchLabels[label]
			if label == lmes.LMEvalJobUIDLabel {
				expected = string(job.UID)
			}
			Eventually(func() error {
				if err := k8sClient.Get(ctx, key, pod); err != nil {
					return err
				}
				delete(pod.Labels, label)
				return k8sClient.Update(ctx, pod)
			}, defaultTimeout, defaultPolling).Should(Succeed())
			Eventually(func() string {
				if err := k8sClient.Get(ctx, key, pod); err != nil {
					return ""
				}
				return pod.Labels[label]
			}, 3*time.Second, defaultPolling).Should(Equal(expected))
		}
		Expect(k8sClient.Get(ctx, policyKey, policy)).To(Succeed())
		policy.Spec.Egress = nil
		policy.Annotations["test-custom"] = "preserve"
		Expect(k8sClient.Update(ctx, policy)).To(Succeed())
		Eventually(func() int {
			if err := k8sClient.Get(ctx, policyKey, policy); err != nil {
				return -1
			}
			return len(policy.Spec.Egress)
		}, 3*time.Second, defaultPolling).Should(Equal(1))
		Expect(policy.Annotations["test-custom"]).To(Equal("preserve"))
		uid := policy.UID
		Expect(k8sClient.Delete(ctx, policy)).To(Succeed())
		Eventually(func() bool { return k8sClient.Get(ctx, policyKey, policy) == nil && policy.UID != uid }, 3*time.Second, defaultPolling).Should(BeTrue())
		Expect(k8sClient.Get(ctx, key, job)).To(Succeed())
		Expect(job.Status.State).To(Equal(lmesv1alpha1.CompleteJobState))
		Expect(job.Status.Results).To(Equal("retained-result"))
		Expect(apimeta.IsStatusConditionTrue(job.Status.Conditions, lmes.NetworkPolicyReadyCondition)).To(BeTrue())
	})
})

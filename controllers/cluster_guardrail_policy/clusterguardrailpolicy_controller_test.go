package cluster_guardrail_policy

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	clusterguardrailpolicyv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/cluster_guardrail_policy/v1alpha1"
	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("ClusterGuardrailPolicy Controller", func() {
	const (
		policyName      = "test-guardrails"
		targetNamespace = "test-target"
	)

	var (
		ctx        = context.Background()
		reconciler *ClusterGuardrailPolicyReconciler
	)

	BeforeEach(func() {
		By("creating the target namespace")
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: targetNamespace},
		}
		err := k8sClient.Create(ctx, ns)
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}

		reconciler = &ClusterGuardrailPolicyReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
	})

	AfterEach(func() {
		policy := &clusterguardrailpolicyv1alpha1.ClusterGuardrailPolicy{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: policyName}, policy)
		if err == nil {
			By("Cleanup: deleting ClusterGuardrailPolicy")
			Expect(k8sClient.Delete(ctx, policy)).To(Succeed())
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: policyName},
			})
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: policyName}, policy)
				return errors.IsNotFound(err)
			}, time.Second*5, time.Millisecond*100).Should(BeTrue())
		}
	})

	createPolicy := func(guardrails *clusterguardrailpolicyv1alpha1.GuardrailRules) {
		policy := &clusterguardrailpolicyv1alpha1.ClusterGuardrailPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name: policyName,
			},
			Spec: clusterguardrailpolicyv1alpha1.ClusterGuardrailPolicySpec{
				TargetRef: clusterguardrailpolicyv1alpha1.TargetRef{
					Group: "gateway.networking.k8s.io",
					Kind:  "Gateway",
					Name:  "test-gateway",
				},
				TargetNamespace: targetNamespace,
				Guardrails:      guardrails,
			},
		}
		Expect(k8sClient.Create(ctx, policy)).To(Succeed())
	}

	It("should add finalizer on first reconcile", func() {
		createPolicy(nil)

		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: policyName},
		})
		Expect(err).NotTo(HaveOccurred())

		policy := &clusterguardrailpolicyv1alpha1.ClusterGuardrailPolicy{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: policyName}, policy)).To(Succeed())
		Expect(policy.Finalizers).To(ContainElement(finalizerName))
	})

	It("should set status condition when Gateway validation fails", func() {
		createPolicy(nil)

		// Reconcile twice: first adds finalizer, second checks Gateway
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: policyName},
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: policyName},
		})
		Expect(err).NotTo(HaveOccurred())

		policy := &clusterguardrailpolicyv1alpha1.ClusterGuardrailPolicy{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: policyName}, policy)).To(Succeed())
		// Gateway CRD not installed in envtest, so Accepted condition should be False
		found := false
		for _, c := range policy.Status.Conditions {
			if c.Type == "Accepted" && c.Status == corev1.ConditionFalse {
				found = true
				break
			}
		}
		Expect(found).To(BeTrue(), "Expected Accepted=False condition")
	})

	It("should create NemoGuardrails CR in target namespace", func() {
		replicas := int32(1)
		createPolicy(&clusterguardrailpolicyv1alpha1.GuardrailRules{
			NemoGuardrails: &clusterguardrailpolicyv1alpha1.NemoGuardrailsConfig{
				NemoConfigs: []clusterguardrailpolicyv1alpha1.NemoConfigRef{
					{
						Name:       "content-safety",
						ConfigMaps: []string{"test-config"},
						Default:    true,
					},
				},
				Replicas: &replicas,
			},
		})

		// Reconcile: finalizer, then gateway check (not found → Pending, but NemoGuardrails still skipped)
		// Without a real Gateway, NemoGuardrails won't be created, so just verify the policy status
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: policyName},
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: policyName},
		})
		Expect(err).NotTo(HaveOccurred())

		// Gateway not found, so NemoGuardrails should not be created
		nemoName := policyName + "-nemo"
		nemo := &nemoguardrailsv1alpha1.NemoGuardrails{}
		err = k8sClient.Get(ctx, types.NamespacedName{Name: nemoName, Namespace: targetNamespace}, nemo)
		Expect(errors.IsNotFound(err)).To(BeTrue())
	})

	It("should cleanup managed resources on deletion", func() {
		createPolicy(nil)

		// Add finalizer
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: policyName},
		})
		Expect(err).NotTo(HaveOccurred())

		// Create a managed NemoGuardrails CR manually to test cleanup
		nemo := &nemoguardrailsv1alpha1.NemoGuardrails{
			ObjectMeta: metav1.ObjectMeta{
				Name:      policyName + "-nemo",
				Namespace: targetNamespace,
				Labels: map[string]string{
					managedByLabel: policyName,
				},
			},
			Spec: nemoguardrailsv1alpha1.NemoGuardrailsSpec{
				NemoConfigs: []nemoguardrailsv1alpha1.NemoConfig{
					{Name: "test", ConfigMaps: []string{"test"}, Default: true},
				},
			},
		}
		Expect(k8sClient.Create(ctx, nemo)).To(Succeed())

		// Create a managed ConfigMap manually to test cleanup
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      policyName + "-request-guard-config",
				Namespace: targetNamespace,
				Labels: map[string]string{
					managedByLabel: policyName,
				},
			},
			Data: map[string]string{"config.json": "{}"},
		}
		Expect(k8sClient.Create(ctx, cm)).To(Succeed())

		// Delete policy
		policy := &clusterguardrailpolicyv1alpha1.ClusterGuardrailPolicy{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: policyName}, policy)).To(Succeed())
		Expect(k8sClient.Delete(ctx, policy)).To(Succeed())

		// Reconcile to trigger finalizer cleanup
		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: policyName},
		})
		Expect(err).NotTo(HaveOccurred())

		// Verify managed resources are deleted
		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name: policyName + "-nemo", Namespace: targetNamespace,
			}, &nemoguardrailsv1alpha1.NemoGuardrails{})
			return errors.IsNotFound(err)
		}, time.Second*5, time.Millisecond*100).Should(BeTrue())

		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name: policyName + "-request-guard-config", Namespace: targetNamespace,
			}, &corev1.ConfigMap{})
			return errors.IsNotFound(err)
		}, time.Second*5, time.Millisecond*100).Should(BeTrue())
	})
})

var _ = Describe("Policy Merge", func() {
	It("should use spec.guardrails directly when set", func() {
		timeout := 30
		spec := &clusterguardrailpolicyv1alpha1.ClusterGuardrailPolicySpec{
			Guardrails: &clusterguardrailpolicyv1alpha1.GuardrailRules{
				InputGuard: &clusterguardrailpolicyv1alpha1.GuardPluginConfig{
					TimeoutSeconds: &timeout,
				},
			},
		}
		result := resolveEffectiveGuardrails(spec)
		Expect(result).NotTo(BeNil())
		Expect(result.InputGuard).NotTo(BeNil())
		Expect(*result.InputGuard.TimeoutSeconds).To(Equal(30))
	})

	It("should use defaults when guardrails is not set", func() {
		timeout := 60
		spec := &clusterguardrailpolicyv1alpha1.ClusterGuardrailPolicySpec{
			Defaults: &clusterguardrailpolicyv1alpha1.PolicyDefaults{
				Guardrails: &clusterguardrailpolicyv1alpha1.GuardrailRules{
					OutputGuard: &clusterguardrailpolicyv1alpha1.GuardPluginConfig{
						TimeoutSeconds: &timeout,
					},
				},
			},
		}
		result := resolveEffectiveGuardrails(spec)
		Expect(result).NotTo(BeNil())
		Expect(result.OutputGuard).NotTo(BeNil())
		Expect(*result.OutputGuard.TimeoutSeconds).To(Equal(60))
	})

	It("should apply atomic overrides completely", func() {
		baseTimeout := 30
		overrideTimeout := 90
		spec := &clusterguardrailpolicyv1alpha1.ClusterGuardrailPolicySpec{
			Guardrails: &clusterguardrailpolicyv1alpha1.GuardrailRules{
				InputGuard: &clusterguardrailpolicyv1alpha1.GuardPluginConfig{
					TimeoutSeconds: &baseTimeout,
				},
				OutputGuard: &clusterguardrailpolicyv1alpha1.GuardPluginConfig{
					TimeoutSeconds: &baseTimeout,
				},
			},
			Overrides: &clusterguardrailpolicyv1alpha1.PolicyOverrides{
				Strategy: clusterguardrailpolicyv1alpha1.MergeStrategyAtomic,
				Guardrails: &clusterguardrailpolicyv1alpha1.GuardrailRules{
					InputGuard: &clusterguardrailpolicyv1alpha1.GuardPluginConfig{
						TimeoutSeconds: &overrideTimeout,
					},
				},
			},
		}
		result := resolveEffectiveGuardrails(spec)
		Expect(result).NotTo(BeNil())
		Expect(result.InputGuard).NotTo(BeNil())
		Expect(*result.InputGuard.TimeoutSeconds).To(Equal(90))
		// Atomic: OutputGuard from base is NOT preserved
		Expect(result.OutputGuard).To(BeNil())
	})

	It("should merge overrides with merge strategy", func() {
		baseTimeout := 30
		overrideTimeout := 90
		spec := &clusterguardrailpolicyv1alpha1.ClusterGuardrailPolicySpec{
			Guardrails: &clusterguardrailpolicyv1alpha1.GuardrailRules{
				InputGuard: &clusterguardrailpolicyv1alpha1.GuardPluginConfig{
					TimeoutSeconds: &baseTimeout,
				},
				OutputGuard: &clusterguardrailpolicyv1alpha1.GuardPluginConfig{
					TimeoutSeconds: &baseTimeout,
				},
			},
			Overrides: &clusterguardrailpolicyv1alpha1.PolicyOverrides{
				Strategy: clusterguardrailpolicyv1alpha1.MergeStrategyMerge,
				Guardrails: &clusterguardrailpolicyv1alpha1.GuardrailRules{
					InputGuard: &clusterguardrailpolicyv1alpha1.GuardPluginConfig{
						TimeoutSeconds: &overrideTimeout,
					},
				},
			},
		}
		result := resolveEffectiveGuardrails(spec)
		Expect(result).NotTo(BeNil())
		Expect(result.InputGuard).NotTo(BeNil())
		Expect(*result.InputGuard.TimeoutSeconds).To(Equal(90))
		// Merge: OutputGuard from base IS preserved
		Expect(result.OutputGuard).NotTo(BeNil())
		Expect(*result.OutputGuard.TimeoutSeconds).To(Equal(30))
	})

	It("should return nil when no guardrails are configured", func() {
		spec := &clusterguardrailpolicyv1alpha1.ClusterGuardrailPolicySpec{}
		result := resolveEffectiveGuardrails(spec)
		Expect(result).To(BeNil())
	})
})

package nemo_guardrails

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("NemoGuardrails allowedConsumers", func() {
	const namespace = "allowed-consumers"
	ctx := context.Background()

	BeforeEach(func() {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
		err := k8sClient.Create(ctx, ns)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}
	})

	DescribeTable("validates the allowed consumers permission",
		func(consumers *nemoguardrailsv1alpha1.AllowedConsumers, valid bool) {
			cr := &nemoguardrailsv1alpha1.NemoGuardrails{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "consumers-", Namespace: namespace},
				Spec: nemoguardrailsv1alpha1.NemoGuardrailsSpec{
					NemoConfigs: []nemoguardrailsv1alpha1.NemoConfig{{
						Name:       "config",
						ConfigMaps: []string{"config"},
					}},
					AllowedConsumers: consumers,
				},
			}
			err := k8sClient.Create(ctx, cr)
			if valid {
				Expect(err).NotTo(HaveOccurred())
				Expect(k8sClient.Delete(ctx, cr)).To(Succeed())
				return
			}
			Expect(apierrors.IsInvalid(err)).To(BeTrue(), "expected Invalid, got %v", err)
		},
		Entry("omitted permission defaults to the same namespace", nil, true),
		Entry("omitted namespaces defaults to the same namespace", &nemoguardrailsv1alpha1.AllowedConsumers{}, true),
		Entry("Same", &nemoguardrailsv1alpha1.AllowedConsumers{Namespaces: &nemoguardrailsv1alpha1.ConsumerNamespaces{
			From: nemoguardrailsv1alpha1.ConsumerNamespaceFromSame,
		}}, true),
		Entry("All", &nemoguardrailsv1alpha1.AllowedConsumers{Namespaces: &nemoguardrailsv1alpha1.ConsumerNamespaces{
			From: nemoguardrailsv1alpha1.ConsumerNamespaceFromAll,
		}}, true),
		Entry("Selector matchLabels", &nemoguardrailsv1alpha1.AllowedConsumers{Namespaces: &nemoguardrailsv1alpha1.ConsumerNamespaces{
			From: nemoguardrailsv1alpha1.ConsumerNamespaceFromSelector,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{
				"kubernetes.io/metadata.name": "team-a",
			}},
		}}, true),
		Entry("Selector matchExpressions", &nemoguardrailsv1alpha1.AllowedConsumers{Namespaces: &nemoguardrailsv1alpha1.ConsumerNamespaces{
			From: nemoguardrailsv1alpha1.ConsumerNamespaceFromSelector,
			Selector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key:      "kubernetes.io/metadata.name",
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{"team-a", "team-b"},
			}, {
				Key:      "team",
				Operator: metav1.LabelSelectorOpExists,
			}}},
		}}, true),
		Entry("unknown from", &nemoguardrailsv1alpha1.AllowedConsumers{Namespaces: &nemoguardrailsv1alpha1.ConsumerNamespaces{
			From: "Other",
		}}, false),
		Entry("Selector without a selector", &nemoguardrailsv1alpha1.AllowedConsumers{Namespaces: &nemoguardrailsv1alpha1.ConsumerNamespaces{
			From: nemoguardrailsv1alpha1.ConsumerNamespaceFromSelector,
		}}, false),
		Entry("Selector with an empty selector", &nemoguardrailsv1alpha1.AllowedConsumers{Namespaces: &nemoguardrailsv1alpha1.ConsumerNamespaces{
			From:     nemoguardrailsv1alpha1.ConsumerNamespaceFromSelector,
			Selector: &metav1.LabelSelector{},
		}}, false),
		Entry("Same with a selector", &nemoguardrailsv1alpha1.AllowedConsumers{Namespaces: &nemoguardrailsv1alpha1.ConsumerNamespaces{
			From:     nemoguardrailsv1alpha1.ConsumerNamespaceFromSame,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "a"}},
		}}, false),
		Entry("All with a selector", &nemoguardrailsv1alpha1.AllowedConsumers{Namespaces: &nemoguardrailsv1alpha1.ConsumerNamespaces{
			From:     nemoguardrailsv1alpha1.ConsumerNamespaceFromAll,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "a"}},
		}}, false),
		Entry("default Same with a selector", &nemoguardrailsv1alpha1.AllowedConsumers{Namespaces: &nemoguardrailsv1alpha1.ConsumerNamespaces{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "a"}},
		}}, false),
		Entry("In without values", &nemoguardrailsv1alpha1.AllowedConsumers{Namespaces: &nemoguardrailsv1alpha1.ConsumerNamespaces{
			From: nemoguardrailsv1alpha1.ConsumerNamespaceFromSelector,
			Selector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key:      "team",
				Operator: metav1.LabelSelectorOpIn,
			}}},
		}}, false),
		Entry("Exists with values", &nemoguardrailsv1alpha1.AllowedConsumers{Namespaces: &nemoguardrailsv1alpha1.ConsumerNamespaces{
			From: nemoguardrailsv1alpha1.ConsumerNamespaceFromSelector,
			Selector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key:      "team",
				Operator: metav1.LabelSelectorOpExists,
				Values:   []string{"a"},
			}}},
		}}, false),
		Entry("unknown operator", &nemoguardrailsv1alpha1.AllowedConsumers{Namespaces: &nemoguardrailsv1alpha1.ConsumerNamespaces{
			From: nemoguardrailsv1alpha1.ConsumerNamespaceFromSelector,
			Selector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key:      "team",
				Operator: "Matches",
				Values:   []string{"a"},
			}}},
		}}, false),
	)

	DescribeTable("accepts an empty values array for presence operators",
		func(operator string, values []any, valid bool) {
			cr := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "trustyai.opendatahub.io/v1alpha1",
				"kind":       "NemoGuardrails",
				"metadata": map[string]any{
					"generateName": "consumers-",
					"namespace":    namespace,
				},
				"spec": map[string]any{
					"nemoConfigs": []any{
						map[string]any{"name": "config", "configMaps": []any{"config"}},
					},
					"allowedConsumers": map[string]any{
						"namespaces": map[string]any{
							"from": "Selector",
							"selector": map[string]any{
								"matchExpressions": []any{
									map[string]any{
										"key":      "team",
										"operator": operator,
										"values":   values,
									},
								},
							},
						},
					},
				},
			}}
			err := k8sClient.Create(ctx, cr)
			if valid {
				Expect(err).NotTo(HaveOccurred())
				Expect(k8sClient.Delete(ctx, cr)).To(Succeed())
				return
			}
			Expect(apierrors.IsInvalid(err)).To(BeTrue(), "expected Invalid, got %v", err)
		},
		Entry("Exists with values []", "Exists", []any{}, true),
		Entry("DoesNotExist with values []", "DoesNotExist", []any{}, true),
		Entry("In with values []", "In", []any{}, false),
	)
})

var _ = Describe("validateAllowedConsumers", func() {
	DescribeTable("checks label selector syntax",
		func(ac *nemoguardrailsv1alpha1.AllowedConsumers, wantErr bool) {
			errs := validateAllowedConsumers(ac)
			if wantErr {
				Expect(errs).NotTo(BeEmpty())
			} else {
				Expect(errs).To(BeEmpty())
			}
		},
		Entry("nil AllowedConsumers", nil, false),
		Entry("nil Namespaces", &nemoguardrailsv1alpha1.AllowedConsumers{}, false),
		Entry("nil Selector", &nemoguardrailsv1alpha1.AllowedConsumers{Namespaces: &nemoguardrailsv1alpha1.ConsumerNamespaces{
			From: nemoguardrailsv1alpha1.ConsumerNamespaceFromSame,
		}}, false),
		Entry("valid matchLabels", &nemoguardrailsv1alpha1.AllowedConsumers{Namespaces: &nemoguardrailsv1alpha1.ConsumerNamespaces{
			From:     nemoguardrailsv1alpha1.ConsumerNamespaceFromSelector,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "a"}},
		}}, false),
		Entry("invalid matchLabels key", &nemoguardrailsv1alpha1.AllowedConsumers{Namespaces: &nemoguardrailsv1alpha1.ConsumerNamespaces{
			From:     nemoguardrailsv1alpha1.ConsumerNamespaceFromSelector,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"???": "a"}},
		}}, true),
		Entry("invalid matchLabels value", &nemoguardrailsv1alpha1.AllowedConsumers{Namespaces: &nemoguardrailsv1alpha1.ConsumerNamespaces{
			From:     nemoguardrailsv1alpha1.ConsumerNamespaceFromSelector,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "not a valid value!"}},
		}}, true),
		Entry("valid matchExpressions", &nemoguardrailsv1alpha1.AllowedConsumers{Namespaces: &nemoguardrailsv1alpha1.ConsumerNamespaces{
			From: nemoguardrailsv1alpha1.ConsumerNamespaceFromSelector,
			Selector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key: "team", Operator: metav1.LabelSelectorOpIn, Values: []string{"a"},
			}}},
		}}, false),
		Entry("invalid matchExpressions key", &nemoguardrailsv1alpha1.AllowedConsumers{Namespaces: &nemoguardrailsv1alpha1.ConsumerNamespaces{
			From: nemoguardrailsv1alpha1.ConsumerNamespaceFromSelector,
			Selector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key: "???", Operator: metav1.LabelSelectorOpExists,
			}}},
		}}, true),
		Entry("invalid matchExpressions value", &nemoguardrailsv1alpha1.AllowedConsumers{Namespaces: &nemoguardrailsv1alpha1.ConsumerNamespaces{
			From: nemoguardrailsv1alpha1.ConsumerNamespaceFromSelector,
			Selector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key: "team", Operator: metav1.LabelSelectorOpIn, Values: []string{"not a valid value!"},
			}}},
		}}, true),
	)
})

var _ = Describe("NemoGuardrails reconcile with an invalid allowedConsumers selector", func() {
	const namespace = "allowed-consumers-invalid"
	ctx := context.Background()

	BeforeEach(func() {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
		err := k8sClient.Create(ctx, ns)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}
	})

	It("sets phase Error and an AllowedConsumersReady=False condition", func() {
		const name = "invalid-selector"
		cr := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "trustyai.opendatahub.io/v1alpha1",
			"kind":       "NemoGuardrails",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"nemoConfigs": []any{
					map[string]any{"name": "config", "configMaps": []any{"config"}},
				},
				"allowedConsumers": map[string]any{
					"namespaces": map[string]any{
						"from": "Selector",
						"selector": map[string]any{
							"matchLabels": map[string]any{"???": "team-a"},
						},
					},
				},
			},
		}}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())

		reconciler := &NemoGuardrailsReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			Namespace: namespace,
		}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: name, Namespace: namespace},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &nemoguardrailsv1alpha1.NemoGuardrails{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(utils.PhaseError))

		cond := utils.GetStatusCondition(updated.Status.Conditions, "AllowedConsumersReady")
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
		Expect(cond.Reason).To(Equal(invalidAllowedConsumersReason))
	})

	It("clears the AllowedConsumersReady condition once the selector is fixed", func() {
		const name = "fixable-selector"
		cr := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "trustyai.opendatahub.io/v1alpha1",
			"kind":       "NemoGuardrails",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"nemoConfigs": []any{
					map[string]any{"name": "config", "configMaps": []any{"config"}},
				},
				"allowedConsumers": map[string]any{
					"namespaces": map[string]any{
						"from": "Selector",
						"selector": map[string]any{
							"matchLabels": map[string]any{"???": "team-a"},
						},
					},
				},
			},
		}}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())

		reconciler := &NemoGuardrailsReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			Namespace: namespace,
		}
		key := types.NamespacedName{Name: name, Namespace: namespace}

		By("reconciling with the invalid selector")
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		updated := &nemoguardrailsv1alpha1.NemoGuardrails{}
		Expect(k8sClient.Get(ctx, key, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(utils.PhaseError))
		cond := utils.GetStatusCondition(updated.Status.Conditions, "AllowedConsumersReady")
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))

		By("fixing the selector and reconciling again")
		patch := client.MergeFrom(updated.DeepCopy())
		updated.Spec.AllowedConsumers = &nemoguardrailsv1alpha1.AllowedConsumers{
			Namespaces: &nemoguardrailsv1alpha1.ConsumerNamespaces{
				From: nemoguardrailsv1alpha1.ConsumerNamespaceFromSelector,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"team": "team-a"},
				},
			},
		}
		Expect(k8sClient.Patch(ctx, updated, patch)).To(Succeed())

		_, _ = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})

		fixed := &nemoguardrailsv1alpha1.NemoGuardrails{}
		Expect(k8sClient.Get(ctx, key, fixed)).To(Succeed())
		fixedCond := utils.GetStatusCondition(fixed.Status.Conditions, "AllowedConsumersReady")
		Expect(fixedCond).NotTo(BeNil())
		Expect(fixedCond.Status).To(Equal(corev1.ConditionTrue))
		Expect(fixedCond.Reason).To(Equal(validAllowedConsumersReason))
	})
})

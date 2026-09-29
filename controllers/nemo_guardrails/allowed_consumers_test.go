package nemo_guardrails

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	DescribeTable("validates the permission shape",
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
})

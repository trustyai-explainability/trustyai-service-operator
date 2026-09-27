package nemo_guardrails

import (
	"context"
	"fmt"

	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	nemoNetworkPolicyNamespaceGroupLabel = "network.openshift.io/policy-group"
	nemoNetworkPolicyIngressGroup        = "ingress"
)

// buildNemoNetworkPolicy builds an ingress-only policy for one NeMo Guardrails
// instance. It follows the Deployment Service/Route target and allows only
// router traffic to the active listener when the Route is enabled.
func buildNemoNetworkPolicy(instance *nemoguardrailsv1alpha1.NemoGuardrails) (*networkingv1.NetworkPolicy, error) {
	if instance == nil {
		return nil, fmt.Errorf("NemoGuardrails must not be nil")
	}
	if instance.Name == "" || instance.Namespace == "" {
		return nil, fmt.Errorf("NemoGuardrails must have a name and namespace")
	}

	name, err := utils.NetworkPolicyName("nemo-guardrails-" + instance.Name)
	if err != nil {
		return nil, err
	}
	labels, err := utils.NetworkPolicyOwnerLabels(instance)
	if err != nil {
		return nil, err
	}

	var ingress []networkingv1.NetworkPolicyIngressRule
	routeEnabled := instance.Spec.ExposeRoute == nil || *instance.Spec.ExposeRoute
	if routeEnabled {
		port := int32(8000) // Service targetPort in direct mode.
		if utils.RequiresAuth(instance) {
			port = 8443 // kube-rbac-proxy Service targetPort when auth is enabled.
		}
		ingress = append(ingress, networkingv1.NetworkPolicyIngressRule{
			From: []networkingv1.NetworkPolicyPeer{{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
					nemoNetworkPolicyNamespaceGroupLabel: nemoNetworkPolicyIngressGroup,
				}},
			}},
			Ports: []networkingv1.NetworkPolicyPort{nemoTCPPort(port)},
		})
	}

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
			Labels:    labels,
		},
		Spec: networkingv1.NetworkPolicySpec{
			// Match the labels rendered on the NeMo pod template so policies for
			// different CRs in the namespace remain isolated.
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{
				"app":                        instance.Name,
				"component":                  instance.Name,
				"deploy-name":                instance.Name,
				"app.kubernetes.io/instance": instance.Name,
				"app.kubernetes.io/name":     instance.Name,
				"app.kubernetes.io/part-of":  "trustyai",
			}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress:     ingress,
		},
	}, nil
}

func (r *NemoGuardrailsReconciler) reconcileNetworkPolicy(ctx context.Context, instance *nemoguardrailsv1alpha1.NemoGuardrails) error {
	policy, err := buildNemoNetworkPolicy(instance)
	if err != nil {
		return err
	}
	if err := utils.SetNetworkPolicyOwnerReference(policy, instance, r.Scheme); err != nil {
		return err
	}
	return utils.ReconcileNetworkPolicy(ctx, r.Client, policy)
}

func nemoTCPPort(port int32) networkingv1.NetworkPolicyPort {
	protocol := corev1.ProtocolTCP
	value := intstr.FromInt32(port)
	return networkingv1.NetworkPolicyPort{Protocol: &protocol, Port: &value}
}

package tas

import (
	"context"
	"fmt"

	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	networkPolicyNamespaceGroupLabel = "network.openshift.io/policy-group"
	networkPolicyIngressGroup        = "ingress"
	networkPolicyMonitoringGroup     = "monitoring"
	prometheusNameLabel              = "app.kubernetes.io/name"
	prometheusComponentLabel         = "app.kubernetes.io/component"
	prometheusNameValue              = "prometheus"
	prometheusComponentValue         = "prometheus"
	modelMeshServiceLabel            = "modelmesh-service"
	modelMeshServiceValue            = "modelmesh-serving"
	kserveInferenceServiceLabel      = "serving.kserve.io/inferenceservice"
)

// buildTASNetworkPolicy builds the ingress policy for the Deployment managed by
// a TrustyAIService. Destination ports mirror the operator-owned Service,
// Route, and ServiceMonitor targets; the policy does not restrict egress.
func buildTASNetworkPolicy(instance *trustyaiopendatahubiov1.TrustyAIService) (*networkingv1.NetworkPolicy, error) {
	if instance == nil {
		return nil, fmt.Errorf("TrustyAIService must not be nil")
	}
	if instance.Name == "" || instance.Namespace == "" {
		return nil, fmt.Errorf("TrustyAIService must have a name and namespace")
	}

	name, err := utils.NetworkPolicyName(instance.Name)
	if err != nil {
		return nil, err
	}
	labels, err := utils.NetworkPolicyOwnerLabels(instance)
	if err != nil {
		return nil, err
	}

	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
			Labels:    labels,
		},
		Spec: networkingv1.NetworkPolicySpec{
			// Exclude app.kubernetes.io/version: existing Deployments may retain
			// their creation-time version label across an operator upgrade.
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{
				"app":                        instance.Name,
				"app.kubernetes.io/instance": instance.Name,
				"app.kubernetes.io/part-of":  "trustyai",
			}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{{
						NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
							networkPolicyNamespaceGroupLabel: networkPolicyIngressGroup,
						}},
					}},
					Ports: []networkingv1.NetworkPolicyPort{tasTCPPort(8443)},
				},
				{
					From: []networkingv1.NetworkPolicyPeer{{
						NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
							networkPolicyNamespaceGroupLabel: networkPolicyMonitoringGroup,
						}},
						PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
							prometheusNameLabel:      prometheusNameValue,
							prometheusComponentLabel: prometheusComponentValue,
						}},
					}},
					Ports: []networkingv1.NetworkPolicyPort{tasTCPPort(8443)},
				},
				{
					From: []networkingv1.NetworkPolicyPeer{{
						NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
							networkPolicyNamespaceGroupLabel: networkPolicyMonitoringGroup,
						}},
						PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
							prometheusNameLabel:      prometheusNameValue,
							prometheusComponentLabel: prometheusComponentValue,
						}},
					}},
					Ports: []networkingv1.NetworkPolicyPort{tasTCPPort(8080)},
				},
				{
					From: []networkingv1.NetworkPolicyPeer{
						{PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
							modelMeshServiceLabel: modelMeshServiceValue,
						}}},
						{PodSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
							Key:      kserveInferenceServiceLabel,
							Operator: metav1.LabelSelectorOpExists,
						}}}},
					},
					Ports: []networkingv1.NetworkPolicyPort{tasTCPPort(4443)},
				},
			},
		},
	}

	return policy, nil
}

func tasTCPPort(port int32) networkingv1.NetworkPolicyPort {
	protocol := corev1.ProtocolTCP
	value := intstr.FromInt32(port)
	return networkingv1.NetworkPolicyPort{Protocol: &protocol, Port: &value}
}

func (r *TrustyAIServiceReconciler) reconcileNetworkPolicy(instance *trustyaiopendatahubiov1.TrustyAIService, ctx context.Context) error {
	policy, err := buildTASNetworkPolicy(instance)
	if err != nil {
		return err
	}
	if err := utils.SetNetworkPolicyOwnerReference(policy, instance, r.Scheme); err != nil {
		return err
	}
	return utils.ReconcileNetworkPolicy(ctx, r.Client, policy)
}

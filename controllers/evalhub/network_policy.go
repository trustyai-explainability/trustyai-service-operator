package evalhub

import (
	"context"
	"fmt"

	evalhubv1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	evalHubNetworkPolicyNamespaceGroupLabel = "network.openshift.io/policy-group"
	evalHubNetworkPolicyIngressGroup        = "ingress"
	evalHubNetworkPolicyMonitoringGroup     = "monitoring"
	evalHubPrometheusNameLabel              = "app.kubernetes.io/name"
	evalHubPrometheusComponentLabel         = "app.kubernetes.io/component"
	evalHubPrometheusNameValue              = "prometheus"
	evalHubPrometheusComponentValue         = "prometheus"
	evalHubMCPComponentValue                = "mcp"
	evalHubEvaluationJobAppValue            = "evalhub"
	evalHubEvaluationJobComponentValue      = "evaluation-job"
	evalHubEvaluationJobInstanceNameLabel   = "evalhub_instance_name"
	evalHubEvaluationJobNamespaceLabel      = "evalhub_instance_namespace"
)

// buildEvalHubNetworkPolicy builds an ingress-only policy for this EvalHub
// instance's API Deployment. The API Service terminates on kube-rbac-proxy at
// servicePort; same-instance MCP and evaluation-job clients use that API path,
// while Prometheus scrapes the dedicated, unauthenticated metrics port.
func buildEvalHubNetworkPolicy(instance *evalhubv1.EvalHub) (*networkingv1.NetworkPolicy, error) {
	if instance == nil {
		return nil, fmt.Errorf("EvalHub must not be nil")
	}
	if instance.Name == "" || instance.Namespace == "" {
		return nil, fmt.Errorf("EvalHub must have a name and namespace")
	}

	name, err := evalHubNetworkPolicyName(instance)
	if err != nil {
		return nil, err
	}
	labels, err := utils.NetworkPolicyOwnerLabels(instance)
	if err != nil {
		return nil, err
	}

	ingress := []networkingv1.NetworkPolicyIngressRule{
		{
			From: []networkingv1.NetworkPolicyPeer{{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
					evalHubNetworkPolicyNamespaceGroupLabel: evalHubNetworkPolicyIngressGroup,
				}},
			}},
			Ports: []networkingv1.NetworkPolicyPort{evalHubTCPPort(servicePort)},
		},
		{
			From: []networkingv1.NetworkPolicyPeer{
				{PodSelector: &metav1.LabelSelector{MatchLabels: evalHubEvaluationJobPodLabels(instance)}},
				{
					NamespaceSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
						Key:      tenantLabel,
						Operator: metav1.LabelSelectorOpExists,
					}}},
					PodSelector: &metav1.LabelSelector{MatchLabels: evalHubEvaluationJobPodLabels(instance)},
				},
			},
			Ports: []networkingv1.NetworkPolicyPort{evalHubTCPPort(servicePort)},
		},
		{
			From: []networkingv1.NetworkPolicyPeer{{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
					evalHubNetworkPolicyNamespaceGroupLabel: evalHubNetworkPolicyMonitoringGroup,
				}},
				PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
					evalHubPrometheusNameLabel:      evalHubPrometheusNameValue,
					evalHubPrometheusComponentLabel: evalHubPrometheusComponentValue,
				}},
			}},
			Ports: []networkingv1.NetworkPolicyPort{evalHubTCPPort(metricsPort)},
		},
	}
	if instance.Spec.IsMCPEnabled() {
		ingress = append(ingress, networkingv1.NetworkPolicyIngressRule{
			From: []networkingv1.NetworkPolicyPeer{{
				PodSelector: &metav1.LabelSelector{MatchLabels: mcpLabels(instance)},
			}},
			Ports: []networkingv1.NetworkPolicyPort{evalHubTCPPort(servicePort)},
		})
	}

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
			Labels:    labels,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{
				"app":       "eval-hub",
				"instance":  instance.Name,
				"component": "api",
			}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress:     ingress,
		},
	}, nil
}

func evalHubNetworkPolicyName(instance *evalhubv1.EvalHub) (string, error) {
	return utils.NetworkPolicyName("evalhub-" + instance.Name)
}

func evalHubEvaluationJobPodLabels(instance *evalhubv1.EvalHub) map[string]string {
	return map[string]string{
		evalHubAppLabel:                       evalHubEvaluationJobAppValue,
		evalHubComponentLabel:                 evalHubEvaluationJobComponentValue,
		evalHubEvaluationJobInstanceNameLabel: instance.Name,
		evalHubEvaluationJobNamespaceLabel:    instance.Namespace,
	}
}

func evalHubTCPPort(port int32) networkingv1.NetworkPolicyPort {
	protocol := corev1.ProtocolTCP
	value := intstr.FromInt32(port)
	return networkingv1.NetworkPolicyPort{Protocol: &protocol, Port: &value}
}

func (r *EvalHubReconciler) reconcileNetworkPolicy(ctx context.Context, instance *evalhubv1.EvalHub) error {
	policy, err := buildEvalHubNetworkPolicy(instance)
	if err != nil {
		return err
	}
	if err := utils.SetNetworkPolicyOwnerReference(policy, instance, r.Scheme); err != nil {
		return err
	}
	return utils.ReconcileNetworkPolicy(ctx, r.Client, policy)
}

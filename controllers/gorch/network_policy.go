package gorch

import (
	"context"
	"fmt"

	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	gorchNetworkPolicyNamespaceGroupLabel = "network.openshift.io/policy-group"
	gorchNetworkPolicyIngressGroup        = "ingress"
	gorchNetworkPolicyMonitoringGroup     = "monitoring"
	gorchPrometheusNameLabel              = "app.kubernetes.io/name"
	gorchPrometheusComponentLabel         = "app.kubernetes.io/component"
	gorchPrometheusNameValue              = "prometheus"
	gorchPrometheusComponentValue         = "prometheus"
)

// buildGORCHNetworkPolicy builds an ingress-only policy for the single
// Deployment managed by a GuardrailsOrchestrator. The permitted routes and
// detector metrics ports follow the enabled features and auth-proxy mode.
func buildGORCHNetworkPolicy(instance *gorchv1alpha1.GuardrailsOrchestrator) (*networkingv1.NetworkPolicy, error) {
	if instance == nil {
		return nil, fmt.Errorf("GuardrailsOrchestrator must not be nil")
	}
	if instance.Name == "" || instance.Namespace == "" {
		return nil, fmt.Errorf("GuardrailsOrchestrator must have a name and namespace")
	}

	name, err := utils.NetworkPolicyName("gorch-" + instance.Name)
	if err != nil {
		return nil, err
	}
	labels, err := utils.NetworkPolicyOwnerLabels(instance)
	if err != nil {
		return nil, err
	}

	var ingress []networkingv1.NetworkPolicyIngressRule
	addRouterRule := func(port int32) {
		ingress = append(ingress, networkingv1.NetworkPolicyIngressRule{
			From:  []networkingv1.NetworkPolicyPeer{gorchRouterPeer()},
			Ports: []networkingv1.NetworkPolicyPort{gorchTCPPort(port)},
		})
	}

	useAuthProxy := utils.RequiresAuth(instance)
	if !instance.Spec.DisableOrchestrator {
		mainPort := int32(8032)
		if useAuthProxy {
			mainPort = 8432
		}
		addRouterRule(mainPort)
		// The health endpoint is exposed through its own Route only while the
		// main orchestrator is enabled.
		addRouterRule(8034)
	}

	if instance.Spec.EnableGuardrailsGateway {
		gatewayPort := int32(8090)
		if useAuthProxy {
			gatewayPort = 8490
		}
		addRouterRule(gatewayPort)
	}

	if instance.Spec.EnableBuiltInDetectors {
		detectorPort := int32(8080)
		if useAuthProxy {
			detectorPort = 8480
		}
		addRouterRule(detectorPort)
		ingress = append(ingress, networkingv1.NetworkPolicyIngressRule{
			From:  []networkingv1.NetworkPolicyPeer{gorchPrometheusPeer()},
			Ports: []networkingv1.NetworkPolicyPort{gorchTCPPort(8080)},
		})
	}

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
			Labels:    labels,
		},
		Spec: networkingv1.NetworkPolicySpec{
			// Match the complete stable selector used by the GORCH Deployment so
			// policies for separate CRs cannot select each other's operand pods.
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

func gorchRouterPeer() networkingv1.NetworkPolicyPeer {
	return networkingv1.NetworkPolicyPeer{
		NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
			gorchNetworkPolicyNamespaceGroupLabel: gorchNetworkPolicyIngressGroup,
		}},
	}
}

func gorchPrometheusPeer() networkingv1.NetworkPolicyPeer {
	return networkingv1.NetworkPolicyPeer{
		NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
			gorchNetworkPolicyNamespaceGroupLabel: gorchNetworkPolicyMonitoringGroup,
		}},
		PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
			gorchPrometheusNameLabel:      gorchPrometheusNameValue,
			gorchPrometheusComponentLabel: gorchPrometheusComponentValue,
		}},
	}
}

func gorchTCPPort(port int32) networkingv1.NetworkPolicyPort {
	protocol := corev1.ProtocolTCP
	value := intstr.FromInt32(port)
	return networkingv1.NetworkPolicyPort{Protocol: &protocol, Port: &value}
}

func (r *GuardrailsOrchestratorReconciler) reconcileNetworkPolicy(ctx context.Context, instance *gorchv1alpha1.GuardrailsOrchestrator) error {
	policy, err := buildGORCHNetworkPolicy(instance)
	if err != nil {
		return err
	}
	if err := utils.SetNetworkPolicyOwnerReference(policy, instance, r.Scheme); err != nil {
		return err
	}
	return utils.ReconcileNetworkPolicy(ctx, r.Client, policy)
}

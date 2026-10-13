package evalhub

import (
	"context"
	"fmt"

	evalhubv1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
	// evalHubDashboardNamespaceNameLabel is the immutable Kubernetes-provided
	// namespace name label, used to select the applications namespace by name.
	evalHubDashboardNamespaceNameLabel = "kubernetes.io/metadata.name"
	// Stable pod identity of the ODH Dashboard eval-hub-ui module Deployment
	// (manifests/modules/eval-hub/deployment.yaml in opendatahub-io/odh-dashboard).
	// The Dashboard's BFF proxy runs in these Pods and forwards browser requests
	// to the EvalHub API service.
	evalHubDashboardDeploymentLabel = "deployment"
	evalHubDashboardDeploymentValue = "eval-hub-ui"
	evalHubDashboardPartOfLabel     = "app.kubernetes.io/part-of"
	evalHubDashboardPartOfValue     = "odh-dashboard"
)

func evalHubWorkloadNetworkPolicyIdentity(instance *evalhubv1.EvalHub, role string) (utils.NetworkPolicyIdentity, error) {
	if instance == nil || instance.Name == "" || instance.Namespace == "" || instance.UID == "" {
		return utils.NetworkPolicyIdentity{}, fmt.Errorf("persisted EvalHub name, namespace, and UID are required")
	}
	return utils.NetworkPolicyIdentity{
		OwnerKind: schema.GroupKind{Group: evalhubv1.GroupVersion.Group, Kind: "EvalHub"},
		OwnerUID:  instance.UID,
		Component: "evalhub",
		Role:      role,
	}, nil
}

func evalHubNetworkPolicyIdentity(instance *evalhubv1.EvalHub) (utils.NetworkPolicyIdentity, error) {
	return evalHubWorkloadNetworkPolicyIdentity(instance, "api")
}

func evalHubMCPNetworkPolicyIdentity(instance *evalhubv1.EvalHub) (utils.NetworkPolicyIdentity, error) {
	return evalHubWorkloadNetworkPolicyIdentity(instance, "mcp")
}

func evalHubNetworkPolicyOwner(instance *evalhubv1.EvalHub) *evalhubv1.EvalHub {
	owner := instance.DeepCopy()
	owner.GetObjectKind().SetGroupVersionKind(evalhubv1.GroupVersion.WithKind("EvalHub"))
	return owner
}

func evalHubEvaluationJobPodLabels(instance *evalhubv1.EvalHub) map[string]string {
	return map[string]string{
		evalHubAppLabel:               evalHubAppValue,
		evalHubComponentLabel:         evalHubComponentValue,
		evalHubInstanceNameLabel:      instance.Name,
		evalHubInstanceNamespaceLabel: instance.Namespace,
	}
}

func evalHubNetworkPolicyTCPPort(port int32) networkingv1.NetworkPolicyPort {
	protocol := corev1.ProtocolTCP
	value := intstr.FromInt32(port)
	return networkingv1.NetworkPolicyPort{Protocol: &protocol, Port: &value}
}

// evalHubDashboardUIPodLabels returns the stable pod identity of the ODH
// Dashboard's eval-hub-ui module Deployment. The Dashboard's BFF proxy runs in
// these Pods and forwards browser API requests (providers, collections,
// evaluations) to the EvalHub API service, so the API ingress policy must
// authorize them or the EvalHub UI hangs on Loading (RHOAIENG-100577).
func evalHubDashboardUIPodLabels() map[string]string {
	return map[string]string{
		evalHubDashboardDeploymentLabel: evalHubDashboardDeploymentValue,
		evalHubDashboardPartOfLabel:     evalHubDashboardPartOfValue,
	}
}

// buildEvalHubNetworkPolicy builds the ingress-only policy for one EvalHub API
// workload. The API Route, the Dashboard's eval-hub-ui proxy and evaluation-job
// callbacks reach kube-rbac-proxy on servicePort; Prometheus scrapes the
// unauthenticated metrics listener directly. applicationsNamespace is the ODH
// applications namespace (opendatahub on ODH, redhat-ods-applications on RHOAI)
// where the Dashboard deploys the eval-hub-ui module.
func buildEvalHubNetworkPolicy(instance *evalhubv1.EvalHub, applicationsNamespace string) (utils.WorkloadNetworkPolicy, error) {
	identity, err := evalHubNetworkPolicyIdentity(instance)
	if err != nil {
		return utils.WorkloadNetworkPolicy{}, err
	}
	if applicationsNamespace == "" {
		return utils.WorkloadNetworkPolicy{}, fmt.Errorf("applications namespace is required to authorize the Dashboard eval-hub-ui ingress peer")
	}
	identityLabels, err := identity.Labels()
	if err != nil {
		return utils.WorkloadNetworkPolicy{}, err
	}
	name, err := identity.Name("api-ingress")
	if err != nil {
		return utils.WorkloadNetworkPolicy{}, err
	}

	routerNamespaceSelector := metav1.LabelSelector{MatchLabels: map[string]string{
		evalHubNetworkPolicyNamespaceGroupLabel: evalHubNetworkPolicyIngressGroup,
	}}
	dashboardNamespaceSelector := metav1.LabelSelector{MatchLabels: map[string]string{
		evalHubDashboardNamespaceNameLabel: applicationsNamespace,
	}}
	jobLabels := evalHubEvaluationJobPodLabels(instance)
	jobNamespaceSelector := &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
		Key: tenantLabel, Operator: metav1.LabelSelectorOpExists,
	}}}
	prometheusNamespaceSelector := &metav1.LabelSelector{MatchLabels: map[string]string{
		evalHubNetworkPolicyNamespaceGroupLabel: evalHubNetworkPolicyMonitoringGroup,
	}}

	ingress := []networkingv1.NetworkPolicyIngressRule{
		{
			From: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: routerNamespaceSelector.DeepCopy()}},
			Ports: []networkingv1.NetworkPolicyPort{
				evalHubNetworkPolicyTCPPort(servicePort),
			},
		},
		{
			From: []networkingv1.NetworkPolicyPeer{{
				NamespaceSelector: dashboardNamespaceSelector.DeepCopy(),
				PodSelector:       &metav1.LabelSelector{MatchLabels: evalHubDashboardUIPodLabels()},
			}},
			Ports: []networkingv1.NetworkPolicyPort{
				evalHubNetworkPolicyTCPPort(servicePort),
			},
		},
		{
			From: []networkingv1.NetworkPolicyPeer{
				{PodSelector: &metav1.LabelSelector{MatchLabels: jobLabels}},
				{
					NamespaceSelector: jobNamespaceSelector.DeepCopy(),
					PodSelector:       &metav1.LabelSelector{MatchLabels: jobLabels},
				},
			},
			Ports: []networkingv1.NetworkPolicyPort{
				evalHubNetworkPolicyTCPPort(servicePort),
			},
		},
		{
			From: []networkingv1.NetworkPolicyPeer{{
				NamespaceSelector: prometheusNamespaceSelector,
				PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
					evalHubPrometheusNameLabel:      evalHubPrometheusNameValue,
					evalHubPrometheusComponentLabel: evalHubPrometheusComponentValue,
				}},
			}},
			Ports: []networkingv1.NetworkPolicyPort{
				evalHubNetworkPolicyTCPPort(metricsPort),
			},
		},
	}
	intent := &utils.NetworkPolicyIngressIntent{NamespaceOnlyPeers: []utils.NetworkPolicyNamespaceOnlyPeerIntent{{
		NamespaceSelector: routerNamespaceSelector,
		Rationale:         "OpenShift Route traffic is sourced by router Pods in ingress namespaces; no stable supported router Pod selector is assumed.",
		ResidualRisk:      "Any Pod in a namespace carrying the OpenShift ingress policy-group label can reach the EvalHub API on TCP 8443.",
	}}}
	if instance.Spec.IsMCPEnabled() {
		mcpIdentity, err := evalHubMCPNetworkPolicyIdentity(instance)
		if err != nil {
			return utils.WorkloadNetworkPolicy{}, err
		}
		mcpIdentityLabels, err := mcpIdentity.Labels()
		if err != nil {
			return utils.WorkloadNetworkPolicy{}, err
		}
		for key, value := range mcpLabels(instance) {
			mcpIdentityLabels[key] = value
		}
		ingress = append(ingress, networkingv1.NetworkPolicyIngressRule{
			From: []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{MatchLabels: mcpIdentityLabels}}},
			Ports: []networkingv1.NetworkPolicyPort{
				evalHubNetworkPolicyTCPPort(servicePort),
			},
		})
	}

	desired := utils.WorkloadNetworkPolicy{
		Policy: &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: instance.Namespace},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{MatchLabels: identityLabels},
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
				Ingress:     ingress,
			},
		},
		Ingress: intent,
	}
	if err := utils.ValidateWorkloadNetworkPolicyWithIngressIntent(desired.Policy, identity, nil, intent); err != nil {
		return utils.WorkloadNetworkPolicy{}, err
	}
	return desired, nil
}

func (r *EvalHubReconciler) reconcileNetworkPolicy(ctx context.Context, instance *evalhubv1.EvalHub) error {
	identity, err := evalHubNetworkPolicyIdentity(instance)
	if err != nil {
		return err
	}
	desired, err := buildEvalHubNetworkPolicy(instance, r.Namespace)
	if err != nil {
		return err
	}
	// NetworkPolicies protect only the EvalHub's own namespace. Tenant namespaces
	// contribute callback sources but are never controlled by this reconciler.
	authority := utils.NetworkPolicyAuthority{Namespaces: map[string]bool{instance.Namespace: true}}
	return utils.ReconcileWorkloadNetworkPolicy(ctx, r.Client, r.Scheme, instance, identity, desired, authority)
}

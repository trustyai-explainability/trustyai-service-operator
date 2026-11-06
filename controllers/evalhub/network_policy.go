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
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	// Stable pod identity of the trustyai-service-operator manager Pod, which
	// runs the EvalHub failure reconcilers. The label is the Deployment's
	// immutable spec.selector label (config/manager/manager.yaml, mirrored in
	// trustyai-operator-module/config/manifests-template/manager/manager.yaml).
	// Every kustomize overlay keeps it: the rhoai/odh overlays only add
	// app.kubernetes.io/part-of: trustyai and app.opendatahub.io/trustyai.
	evalHubOperatorPodControlPlaneLabel = "control-plane"
	evalHubOperatorPodControlPlaneValue = "trustyai-service-operator"
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

// evalHubOperatorPodLabels returns the stable pod identity of the operator's
// own controller-manager Pod. That Pod runs the EvalHub failure reconcilers
// (evalhub-evaluation-job-failure and the Kueue Workload failure reconciler),
// which POST benchmark failure events to the EvalHub API because the
// evaluation Job containers never reach EvalHub for an unschedulable Pod or a
// failed init container. Without this ingress peer the operator-generated
// policy silently drops those events and the job stays pending
// (RHOAIENG-100960).
func evalHubOperatorPodLabels() map[string]string {
	return map[string]string{
		evalHubOperatorPodControlPlaneLabel: evalHubOperatorPodControlPlaneValue,
	}
}

// buildEvalHubNetworkPolicy builds the ingress-only policy for one EvalHub API
// workload. The API Route, the Dashboard's eval-hub-ui proxy, the operator's
// own failure reconcilers and evaluation-job callbacks reach kube-rbac-proxy on
// servicePort; Prometheus scrapes the unauthenticated metrics listener
// directly. applicationsNamespace is the applications namespace the operator
// runs in (opendatahub on ODH, redhat-ods-applications on RHOAI), where the
// Dashboard deploys the eval-hub-ui module and where the operator manager Pod
// is the EvalHub failure-event client.
func buildEvalHubNetworkPolicy(instance *evalhubv1.EvalHub, applicationsNamespace string) (utils.WorkloadNetworkPolicy, error) {
	identity, err := evalHubNetworkPolicyIdentity(instance)
	if err != nil {
		return utils.WorkloadNetworkPolicy{}, err
	}
	if applicationsNamespace == "" {
		return utils.WorkloadNetworkPolicy{}, fmt.Errorf("applications namespace is required to authorize the Dashboard eval-hub-ui and operator manager Pod ingress peers")
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
	applicationsNamespaceSelector := metav1.LabelSelector{MatchLabels: map[string]string{
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
				NamespaceSelector: applicationsNamespaceSelector.DeepCopy(),
				PodSelector:       &metav1.LabelSelector{MatchLabels: evalHubDashboardUIPodLabels()},
			}},
			Ports: []networkingv1.NetworkPolicyPort{
				evalHubNetworkPolicyTCPPort(servicePort),
			},
		},
		{
			// The operator manager Pod reports failures that the evaluation Job
			// containers cannot (unschedulable Pod, failed init container). It
			// runs in the applications namespace, which is the operator's own
			// namespace in supported deployments and differs per distribution,
			// so the peer is selected by name at reconcile time.
			From: []networkingv1.NetworkPolicyPeer{{
				NamespaceSelector: applicationsNamespaceSelector.DeepCopy(),
				PodSelector:       &metav1.LabelSelector{MatchLabels: evalHubOperatorPodLabels()},
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
				PolicyTypes: []networkingv1.PolicyType{
					networkingv1.PolicyTypeIngress,
					networkingv1.PolicyTypeEgress,
				},
				Ingress: ingress,
				Egress:  []networkingv1.NetworkPolicyEgressRule{{}},
			},
		},
		Ingress: intent,
		// ODH-ADR-Operator-0016 requires every operand workload to be selected by
		// a policy that declares Egress with an explicit behavior. Allow-all is the
		// documented compatibility posture: EvalHub reaches the Kubernetes API
		// server, tenant namespaces, model and judge endpoints, MLflow and object
		// storage whose addresses are resolved from the CR, provider manifests and
		// model auth Secrets at evaluation time, so no complete static allowlist
		// exists. This does not broaden today's posture (an unselected Pod already
		// has unrestricted egress) and is not exfiltration protection: NetworkPolicy
		// is additive, so no other policy can narrow this grant.
		Egress: &utils.NetworkPolicyEgressIntent{
			Mode: utils.NetworkPolicyAllowAll,
			Rationale: "EvalHub reaches the Kubernetes API server, tenant namespaces, model and judge " +
				"endpoints, MLflow and object storage whose addresses are supplied at evaluation time.",
			ResidualRisk: "Outbound traffic is unrestricted, including for job callbacks and provider " +
				"calls. This provides no exfiltration protection and a restricted profile remains a " +
				"follow-up decision.",
		},
	}
	// The set validator requires both directions for the same target selector, so an
	// ingress-only regression cannot be merged silently.
	if err := utils.ValidateWorkloadNetworkPolicySet([]utils.WorkloadNetworkPolicy{desired}, identity); err != nil {
		return utils.WorkloadNetworkPolicy{}, err
	}
	return desired, nil
}

// evalHubMCPNetworkPolicyName returns the generated policy name for the MCP
// workload. Name and namespace are derived from the persisted identity, so the
// delete path and the reconcile path cannot disagree.
func evalHubMCPNetworkPolicyName(instance *evalhubv1.EvalHub) (string, error) {
	identity, err := evalHubMCPNetworkPolicyIdentity(instance)
	if err != nil {
		return "", err
	}
	return identity.Name("mcp-ingress")
}

// buildEvalHubMCPNetworkPolicy authors the policy for the optional EvalHub MCP
// server Deployment. The MCP server is a user-facing endpoint: it is exposed by
// its own Service and OpenShift Route, so it needs its own workload-scoped
// coverage rather than relying on the API policy, which only names the MCP Pod
// as an API caller.
func buildEvalHubMCPNetworkPolicy(instance *evalhubv1.EvalHub) (utils.WorkloadNetworkPolicy, error) {
	identity, err := evalHubMCPNetworkPolicyIdentity(instance)
	if err != nil {
		return utils.WorkloadNetworkPolicy{}, err
	}
	identityLabels, err := identity.Labels()
	if err != nil {
		return utils.WorkloadNetworkPolicy{}, err
	}
	name, err := identity.Name("mcp-ingress")
	if err != nil {
		return utils.WorkloadNetworkPolicy{}, err
	}

	routerNamespaceSelector := metav1.LabelSelector{MatchLabels: map[string]string{
		evalHubNetworkPolicyNamespaceGroupLabel: evalHubNetworkPolicyIngressGroup,
	}}
	intent := &utils.NetworkPolicyIngressIntent{NamespaceOnlyPeers: []utils.NetworkPolicyNamespaceOnlyPeerIntent{{
		NamespaceSelector: routerNamespaceSelector,
		Rationale:         "OpenShift Route traffic is sourced by router Pods in ingress namespaces; no stable supported router Pod selector is assumed.",
		ResidualRisk:      "Any Pod in a namespace carrying the OpenShift ingress policy-group label can reach the EvalHub MCP server on TCP 8443.",
	}}}

	desired := utils.WorkloadNetworkPolicy{
		Policy: &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: instance.Namespace},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{MatchLabels: identityLabels},
				PolicyTypes: []networkingv1.PolicyType{
					networkingv1.PolicyTypeIngress,
					networkingv1.PolicyTypeEgress,
				},
				Ingress: []networkingv1.NetworkPolicyIngressRule{{
					From:  []networkingv1.NetworkPolicyPeer{{NamespaceSelector: routerNamespaceSelector.DeepCopy()}},
					Ports: []networkingv1.NetworkPolicyPort{evalHubNetworkPolicyTCPPort(mcpServicePort)},
				}},
				// The MCP server has no metrics endpoint of its own and no in-cluster
				// callers are part of the supported contract: clients reach it through
				// the Route, and it calls the EvalHub API outbound.
				Egress: []networkingv1.NetworkPolicyEgressRule{{}},
			},
		},
		Ingress: intent,
		Egress: &utils.NetworkPolicyEgressIntent{
			Mode:         utils.NetworkPolicyAllowAll,
			Rationale:    "The MCP server calls the EvalHub API in-cluster and needs Kubernetes API, DNS and model-provider access whose addresses are supplied at evaluation time.",
			ResidualRisk: "Outbound traffic is unrestricted. NetworkPolicy is additive, so no other policy can narrow this grant.",
		},
	}
	if err := utils.ValidateWorkloadNetworkPolicySet([]utils.WorkloadNetworkPolicy{desired}, identity); err != nil {
		return utils.WorkloadNetworkPolicy{}, err
	}
	return desired, nil
}

// deleteEvalHubMCPNetworkPolicy removes the MCP policy when the server is
// disabled. The shared delete helper refuses to adopt or delete an object it does
// not control, so a same-name foreign policy is left in place.
func (r *EvalHubReconciler) deleteEvalHubMCPNetworkPolicy(ctx context.Context, instance *evalhubv1.EvalHub, authority utils.NetworkPolicyAuthority) error {
	name, err := evalHubMCPNetworkPolicyName(instance)
	if err != nil {
		return err
	}
	key := client.ObjectKey{Name: name, Namespace: instance.Namespace}
	existing := &networkingv1.NetworkPolicy{}
	if err := r.Get(ctx, key, existing); err != nil {
		return client.IgnoreNotFound(err)
	}
	return utils.DeleteOwnedWorkloadNetworkPolicy(ctx, r.Client, r.Scheme, evalHubNetworkPolicyOwner(instance), key, existing.UID, authority)
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
	if err := utils.ReconcileWorkloadNetworkPolicy(ctx, r.Client, r.Scheme, instance, identity, desired, authority); err != nil {
		return err
	}

	if !instance.Spec.IsMCPEnabled() {
		return r.deleteEvalHubMCPNetworkPolicy(ctx, instance, authority)
	}
	mcpIdentity, err := evalHubMCPNetworkPolicyIdentity(instance)
	if err != nil {
		return err
	}
	mcpDesired, err := buildEvalHubMCPNetworkPolicy(instance)
	if err != nil {
		return err
	}
	return utils.ReconcileWorkloadNetworkPolicy(ctx, r.Client, r.Scheme, instance, mcpIdentity, mcpDesired, authority)
}

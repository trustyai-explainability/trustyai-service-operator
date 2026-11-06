package nemo_guardrails

import (
	"context"
	"fmt"

	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	nemoNetworkPolicyNamespaceGroupLabel = "network.openshift.io/policy-group"
	nemoNetworkPolicyIngressGroup        = "ingress"
	nemoNetworkPolicyComponent           = "nemo-guardrails"
	nemoNetworkPolicyRole                = "service"
	nemoNetworkPolicyPurpose             = "service"

	// nemoMaaSCallerNamespace is where MaaS runs the gateway data plane and its
	// payload-processing ext_proc Deployments by default
	// (maas-controller --gateway-namespace). A tenant Gateway in another
	// namespace is a documented additionalIngress peer.
	nemoMaaSCallerNamespace = "openshift-ingress"
	nemoGatewayManagedLabel = "gateway.istio.io/managed"
	nemoGatewayManagedValue = "istio.io-gateway-controller"

	// nemoCallerAppLabel selects MaaS payload-processing Pods. The AI Gateway
	// controller (pkg/tenant.nemoCaller) renders these Deployments with the
	// unsuffixed app label and opens their egress to this server, so the
	// matching ingress peer is what makes guardrail callouts work.
	nemoCallerAppLabel          = "app"
	nemoPayloadProcessingApp    = "payload-processing"
	nemoPayloadPreProcessingApp = "payload-pre-processing"

	// nemoAuthProxyPort is the kube-rbac-proxy secure listener used when the
	// CR requests authentication; nemoServerPort is the direct server listener.
	// nemoAuthProxyAdminPort is the authenticated admin listener the Service also
	// publishes. It is granted only by the compatibility profile below.
	nemoAuthProxyPort      = 8443
	nemoAuthProxyAdminPort = 8444
	nemoServerPort         = 8000

	// status condition keys reported for the operand policy. SetResourceCondition
	// appends "Ready" to the component name it is given.
	nemoNetworkPolicyCondition        = "NetworkPolicyReady"
	nemoNetworkPolicyStatusComponent  = "NetworkPolicy"
	nemoNetworkPolicyFailedReason     = "NetworkPolicyReconcileFailed"
	nemoNetworkPolicyReconciledReason = "NetworkPolicyReady"
)

func nemoNetworkPolicyIdentity(instance *nemoguardrailsv1alpha1.NemoGuardrails) (utils.NetworkPolicyIdentity, error) {
	if instance == nil || instance.Name == "" || instance.Namespace == "" || instance.UID == "" {
		return utils.NetworkPolicyIdentity{}, fmt.Errorf("persisted NemoGuardrails name, namespace, and UID are required")
	}
	return utils.NetworkPolicyIdentity{
		OwnerKind: schema.GroupKind{Group: nemoguardrailsv1alpha1.GroupVersion.Group, Kind: "NemoGuardrails"},
		OwnerUID:  instance.UID,
		Component: nemoNetworkPolicyComponent,
		Role:      nemoNetworkPolicyRole,
	}, nil
}

// nemoNetworkPolicyName is the deterministic policy name for one instance. It is
// derived from the owner UID, so a recreated custom resource never inherits a
// previous instance's policy.
func nemoNetworkPolicyName(instance *nemoguardrailsv1alpha1.NemoGuardrails) (string, error) {
	identity, err := nemoNetworkPolicyIdentity(instance)
	if err != nil {
		return "", err
	}
	return identity.Name(nemoNetworkPolicyPurpose)
}

func nemoNetworkPolicyTCPPort(port int32) networkingv1.NetworkPolicyPort {
	protocol := corev1.ProtocolTCP
	value := intstr.FromInt32(port)
	return networkingv1.NetworkPolicyPort{Protocol: &protocol, Port: &value}
}

// nemoIngressListenerPort returns the Pod listener exposed through the
// NemoGuardrails Service: the kube-rbac-proxy secure port when the CR
// requests authentication, otherwise the direct server port.
func nemoIngressListenerPort(instance *nemoguardrailsv1alpha1.NemoGuardrails) int32 {
	if utils.RequiresAuth(instance) {
		return nemoAuthProxyPort
	}
	return nemoServerPort
}

// nemoRouteExposureEnabled reports whether the CR exposes an external Route.
func nemoRouteExposureEnabled(instance *nemoguardrailsv1alpha1.NemoGuardrails) bool {
	return instance.Spec.ExposeRoute == nil || *instance.Spec.ExposeRoute
}

func nemoPayloadProcessingPodSelector() *metav1.LabelSelector {
	return &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
		Key:      nemoCallerAppLabel,
		Operator: metav1.LabelSelectorOpIn,
		Values:   []string{nemoPayloadProcessingApp, nemoPayloadPreProcessingApp},
	}}}
}

func nemoNamespaceNameSelector(namespace string) *metav1.LabelSelector {
	return &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": namespace}}
}

// nemoIngressProfile is the reviewed ingress profile implied by workload placement.
type nemoIngressProfile string

const (
	// nemoIngressProfileCompatibility is the default same-namespace placement: the
	// workload shares its namespace with the callers, so its Service surface stays
	// reachable from that namespace and the documented in-cluster peers are added
	// to it.
	nemoIngressProfileCompatibility nemoIngressProfile = "Compatibility"
	// nemoIngressProfileRestricted is the shared AI Gateway infra placement
	// (spec.aiGatewayMode), where the workload has no co-located callers and only
	// the documented MaaS, gateway and Route peers are allowed.
	nemoIngressProfileRestricted nemoIngressProfile = "Restricted"
)

// nemoIngressProfileFor derives the profile from placement, not from a second
// switch: the same-namespace placement is the historical deployment shape and
// keeps callers the operator cannot enumerate, while the shared AI Gateway
// namespace exists only to serve MaaS callouts.
func nemoIngressProfileFor(instance *nemoguardrailsv1alpha1.NemoGuardrails, targetNamespace string) nemoIngressProfile {
	if instance != nil && targetNamespace == instance.Namespace {
		return nemoIngressProfileCompatibility
	}
	return nemoIngressProfileRestricted
}

// nemoCompatibilityIngressPorts is the Service surface the compatibility
// profile keeps open to the workload namespace: the public listener and, when
// authentication is enabled, the authenticated admin listener the Service
// publishes. Dropping the admin listener here would remove an endpoint that is
// reachable today, with no configuration that restores it.
func nemoCompatibilityIngressPorts(instance *nemoguardrailsv1alpha1.NemoGuardrails) []networkingv1.NetworkPolicyPort {
	ports := []networkingv1.NetworkPolicyPort{nemoNetworkPolicyTCPPort(nemoIngressListenerPort(instance))}
	if utils.RequiresAuth(instance) {
		ports = append(ports, nemoNetworkPolicyTCPPort(nemoAuthProxyAdminPort))
	}
	return ports
}

// buildNemoGuardrailsNetworkPolicy authors the operand NetworkPolicy for one
// NemoGuardrails instance in its workload namespace.
//
// The policy is authored in the namespace the reconciler resolved, so it follows
// the same-namespace default and the opt-in shared AI Gateway placement. Ingress
// depends on that placement:
//
//   - Same namespace (default): a compatibility rule admits the workload's own
//     namespace on the listeners the Service publishes, because the callers
//     share that namespace and cannot be enumerated from the custom resource.
//   - Shared AI Gateway namespace (spec.aiGatewayMode): only documented callers
//     are admitted — MaaS payload processing and Istio-managed gateway Pods — on
//     the public listener; the admin listener is never granted.
//
// Both profiles also admit spec.networkPolicy.additionalIngress peers and, while
// Route exposure is enabled, OpenShift ingress-policy-group namespaces, and both
// keep a namespace-only peer record with its rationale and residual risk on the
// policy.
//
// Egress is the reviewed AllowAll profile in both placements: outbound
// destinations are entirely user-configured, so no complete allowlist can be
// derived from the custom resource. Callers must not treat it as exfiltration
// protection.
func buildNemoGuardrailsNetworkPolicy(instance *nemoguardrailsv1alpha1.NemoGuardrails, targetNamespace string) (utils.WorkloadNetworkPolicy, error) {
	if targetNamespace == "" {
		return utils.WorkloadNetworkPolicy{}, fmt.Errorf("NemoGuardrails workload namespace is required")
	}
	identity, err := nemoNetworkPolicyIdentity(instance)
	if err != nil {
		return utils.WorkloadNetworkPolicy{}, err
	}
	identityLabels, err := identity.Labels()
	if err != nil {
		return utils.WorkloadNetworkPolicy{}, err
	}
	name, err := identity.Name(nemoNetworkPolicyPurpose)
	if err != nil {
		return utils.WorkloadNetworkPolicy{}, err
	}

	listenerPort := nemoIngressListenerPort(instance)
	payloadProcessingSelector := nemoPayloadProcessingPodSelector()
	profile := nemoIngressProfileFor(instance, targetNamespace)

	// The policy selector is exactly the reserved identity labels: the shared
	// validator requires the selector to select the identity and nothing else.
	// Instance scoping comes from the per-CR owner-uid label; workload labels
	// (app, component, deploy-name) stay on the Pods and Deployment template.
	podSelector := identityLabels

	var ingress []networkingv1.NetworkPolicyIngressRule
	var namespaceOnlyPeers []utils.NetworkPolicyNamespaceOnlyPeerIntent
	if profile == nemoIngressProfileCompatibility {
		// The workload shares its namespace with its callers: the custom resource
		// cannot enumerate them, so the placement-scoped compatibility rule keeps
		// the Service surface reachable from that namespace and nothing wider.
		workloadNamespaceSelector := *nemoNamespaceNameSelector(targetNamespace)
		ingress = append(ingress, networkingv1.NetworkPolicyIngressRule{
			From:  []networkingv1.NetworkPolicyPeer{{NamespaceSelector: workloadNamespaceSelector.DeepCopy()}},
			Ports: nemoCompatibilityIngressPorts(instance),
		})
		namespaceOnlyPeers = append(namespaceOnlyPeers, utils.NetworkPolicyNamespaceOnlyPeerIntent{
			NamespaceSelector: workloadNamespaceSelector,
			Rationale: "The default placement runs the workload in the custom resource namespace, and the callers " +
				"that reach it there (application Pods, notebooks, in-namespace MaaS clients) are chosen by the " +
				"user and cannot be enumerated from the custom resource. The peer is the workload's own namespace " +
				"and grants the listeners the Service publishes.",
			ResidualRisk: "Any Pod in the workload namespace can reach the Service surface, including the " +
				"authenticated admin listener when authentication is enabled. Cross-namespace callers are not " +
				"granted by this rule and require spec.networkPolicy.additionalIngress. Enabling spec.aiGatewayMode " +
				"moves the workload into the shared AI Gateway namespace and replaces this rule with the restricted " +
				"peer set. Kubelet probes are node-originated and not constrainable by NetworkPolicy peers.",
		})
	}

	// MaaS payload processing runs next to the gateway and is the component that
	// posts guardrail checks to this server.
	ingress = append(ingress, networkingv1.NetworkPolicyIngressRule{
		From: []networkingv1.NetworkPolicyPeer{{
			NamespaceSelector: nemoNamespaceNameSelector(nemoMaaSCallerNamespace),
			PodSelector:       payloadProcessingSelector.DeepCopy(),
		}},
		Ports: []networkingv1.NetworkPolicyPort{nemoNetworkPolicyTCPPort(listenerPort)},
	})
	if profile == nemoIngressProfileRestricted {
		// A payload-processing Deployment placed in the shared workload namespace
		// itself is the same documented caller, scoped by its Pod labels only.
		ingress = append(ingress, networkingv1.NetworkPolicyIngressRule{
			From:  []networkingv1.NetworkPolicyPeer{{PodSelector: payloadProcessingSelector.DeepCopy()}},
			Ports: []networkingv1.NetworkPolicyPort{nemoNetworkPolicyTCPPort(listenerPort)},
		})
	}
	// Istio-managed gateway Pods in the default MaaS gateway namespace.
	ingress = append(ingress, networkingv1.NetworkPolicyIngressRule{
		From: []networkingv1.NetworkPolicyPeer{{
			NamespaceSelector: nemoNamespaceNameSelector(nemoMaaSCallerNamespace),
			PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
				nemoGatewayManagedLabel: nemoGatewayManagedValue,
			}},
		}},
		Ports: []networkingv1.NetworkPolicyPort{nemoNetworkPolicyTCPPort(listenerPort)},
	})
	if instance.Spec.NetworkPolicy != nil {
		for _, peer := range instance.Spec.NetworkPolicy.AdditionalIngress {
			namespaceSelector := peer.NamespaceSelector.LabelSelector()
			podSelector := peer.PodSelector.LabelSelector()
			ingress = append(ingress, networkingv1.NetworkPolicyIngressRule{
				From: []networkingv1.NetworkPolicyPeer{{
					NamespaceSelector: &namespaceSelector,
					PodSelector:       &podSelector,
				}},
				Ports: []networkingv1.NetworkPolicyPort{nemoNetworkPolicyTCPPort(listenerPort)},
			})
		}
	}
	if nemoRouteExposureEnabled(instance) {
		routerNamespaceSelector := metav1.LabelSelector{MatchLabels: map[string]string{
			nemoNetworkPolicyNamespaceGroupLabel: nemoNetworkPolicyIngressGroup,
		}}
		ingress = append(ingress, networkingv1.NetworkPolicyIngressRule{
			From:  []networkingv1.NetworkPolicyPeer{{NamespaceSelector: routerNamespaceSelector.DeepCopy()}},
			Ports: []networkingv1.NetworkPolicyPort{nemoNetworkPolicyTCPPort(listenerPort)},
		})
		namespaceOnlyPeers = append(namespaceOnlyPeers, utils.NetworkPolicyNamespaceOnlyPeerIntent{
			NamespaceSelector: routerNamespaceSelector,
			Rationale: "OpenShift Route traffic is sourced by router Pods in ingress namespaces; no stable supported " +
				"router Pod selector is assumed. The peer is installed only while Route exposure is enabled.",
			ResidualRisk: "Any Pod in a namespace carrying the OpenShift ingress policy-group label can reach the " +
				"exposed NemoGuardrails public listener. Managed gateways are granted separately; custom callers " +
				"require explicit additionalIngress peers. The authenticated admin listener is not granted. " +
				"Kubelet probes are node-originated and not constrainable by NetworkPolicy peer selectors.",
		})
	}
	var ingressIntent *utils.NetworkPolicyIngressIntent
	if len(namespaceOnlyPeers) > 0 {
		ingressIntent = &utils.NetworkPolicyIngressIntent{NamespaceOnlyPeers: namespaceOnlyPeers}
	}

	egressIntent := &utils.NetworkPolicyEgressIntent{
		Mode: utils.NetworkPolicyAllowAll,
		Rationale: "NemoGuardrails outbound destinations are driven entirely by user-mounted configuration: " +
			"model providers, rails services, action and tool endpoints, and optional telemetry. No complete " +
			"outbound allowlist can be derived from the custom resource.",
		ResidualRisk: "Outbound traffic is unrestricted. This provides no exfiltration protection and can broaden " +
			"customer NetworkPolicies because ordinary grants are additive. All containers share the Pod network " +
			"boundary. A Restricted egress profile requires CR-driven destination inputs and remains a follow-up " +
			"decision with platform and security owners.",
	}

	desired := utils.WorkloadNetworkPolicy{
		Policy: &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: targetNamespace},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{MatchLabels: podSelector},
				PolicyTypes: []networkingv1.PolicyType{
					networkingv1.PolicyTypeIngress,
					networkingv1.PolicyTypeEgress,
				},
				Ingress: ingress,
				Egress:  []networkingv1.NetworkPolicyEgressRule{{}},
			},
		},
		Ingress: ingressIntent,
		Egress:  egressIntent,
	}
	if err := utils.ValidateWorkloadNetworkPolicyWithIngressIntent(desired.Policy, identity, egressIntent, ingressIntent); err != nil {
		return utils.WorkloadNetworkPolicy{}, err
	}
	return desired, nil
}

// reconcileNetworkPolicy creates or repairs the operand policy in the resolved
// workload namespace.
//
// The policy cannot carry a controller reference when the workload namespace
// differs from the custom resource namespace, because Kubernetes rejects
// cross-namespace owner references. Ownership is then recorded with the
// owner-uid label plus owner name/namespace annotations (utils.SetOwnerReference),
// the policy is watched through an explicit mapping, and deletion is performed by
// the controller during operand cleanup. In the same-namespace placement the
// policy keeps the controller reference that garbage collection follows.
// Reconciliation and the managed metadata are owned by
// utils.ReconcileWorkloadNetworkPolicy, so this operand keeps the same validation,
// adoption, intent-annotation and drift rules as the other components.
func (r *NemoGuardrailsReconciler) reconcileNetworkPolicy(ctx context.Context, instance *nemoguardrailsv1alpha1.NemoGuardrails, targetNamespace string) error {
	if targetNamespace == "" {
		return fmt.Errorf("NemoGuardrails workload namespace is required before NetworkPolicy reconciliation")
	}
	if authorized := r.workloadNamespace(instance); targetNamespace != authorized {
		return fmt.Errorf("refusing to reconcile a NetworkPolicy in %q: the authorized workload namespace is %q", targetNamespace, authorized)
	}
	identity, err := nemoNetworkPolicyIdentity(instance)
	if err != nil {
		return err
	}
	desired, err := buildNemoGuardrailsNetworkPolicy(instance, targetNamespace)
	if err != nil {
		return err
	}
	return utils.ReconcileWorkloadNetworkPolicy(ctx, r.Client, r.Scheme, instance, identity, desired, nemoNetworkPolicyAuthority(instance, targetNamespace))
}

// nemoNetworkPolicyAuthority authorises exactly the resolved workload namespace
// and accepts the ownership record that placement allows. The shared AI Gateway
// namespace cannot hold a controller reference to a namespaced custom resource
// in another namespace, so cross-namespace placement relies on the owner-uid
// label plus the owner name/namespace annotations; same-namespace placement
// keeps a real controller reference, so garbage collection follows the operand.
func nemoNetworkPolicyAuthority(instance *nemoguardrailsv1alpha1.NemoGuardrails, namespace string) utils.NetworkPolicyAuthority {
	return utils.NetworkPolicyAuthority{
		Namespaces: map[string]bool{namespace: true},
		Ownership:  nemoNetworkPolicyOwnership(instance, namespace),
	}
}

// nemoNetworkPolicyOwnership is the ownership record available for a policy in
// namespace: a controller reference inside the custom resource namespace, and
// the label plus annotations across namespaces.
func nemoNetworkPolicyOwnership(instance *nemoguardrailsv1alpha1.NemoGuardrails, namespace string) utils.NetworkPolicyOwnership {
	if instance != nil && namespace == instance.Namespace {
		return utils.SameNamespaceOwnership
	}
	return utils.CrossNamespaceOwnership
}

// enqueueNemoFromNetworkPolicy maps a policy event back to its custom resource.
// It replaces Owns() for policies that cannot carry a controller reference
// because they live in the workload namespace rather than the CR namespace.
func (r *NemoGuardrailsReconciler) enqueueNemoFromNetworkPolicy(ctx context.Context, object client.Object) []reconcile.Request {
	if !hasNemoNetworkPolicyIdentity(object) {
		return nil
	}
	annotations := object.GetAnnotations()
	name := annotations[utils.OwnerNameAnnotation]
	namespace := annotations[utils.OwnerNamespaceAnnotation]
	if name == "" || namespace == "" {
		return nil
	}
	// The labels and annotations are writable by anyone who can create a
	// NetworkPolicy in a namespace they control, so the enqueued custom resource must
	// confirm it is the instance the policy records before it reaches the queue.
	ownerUID := object.GetLabels()[utils.NetworkPolicyOwnerUIDLabel]
	if ownerUID == "" {
		return nil
	}
	owner := &nemoguardrailsv1alpha1.NemoGuardrails{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, owner); err != nil {
		return nil
	}
	if utils.OwnerUIDLabelValue(owner) != ownerUID {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: namespace, Name: name}}}
}

func hasNemoNetworkPolicyIdentity(object client.Object) bool {
	if object == nil {
		return false
	}
	labels := object.GetLabels()
	return labels[utils.NetworkPolicyComponentLabel] == nemoNetworkPolicyComponent &&
		labels[utils.NetworkPolicyManagedByLabel] == utils.NetworkPolicyManagedBy
}

// reportNetworkPolicyFailure surfaces a policy failure on the custom resource so
// the component reports not-ready instead of failing silently.
func (r *NemoGuardrailsReconciler) reportNetworkPolicyFailure(ctx context.Context, instance *nemoguardrailsv1alpha1.NemoGuardrails, cause error) {
	if cause == nil {
		return
	}
	message := cause.Error()
	if cond := utils.GetStatusCondition(instance.Status.Conditions, nemoNetworkPolicyCondition); cond != nil &&
		cond.Status == corev1.ConditionFalse && cond.Message == message {
		return
	}
	if _, err := r.updateStatus(ctx, instance, func(saved *nemoguardrailsv1alpha1.NemoGuardrails) {
		utils.SetResourceCondition(&saved.Status.Conditions, nemoNetworkPolicyStatusComponent, nemoNetworkPolicyFailedReason, message, corev1.ConditionFalse)
		utils.SetCompleteCondition(&saved.Status.Conditions, corev1.ConditionFalse, nemoNetworkPolicyFailedReason, message)
		saved.Status.Phase = utils.PhaseError
	}); err != nil {
		log.FromContext(ctx).Error(err, "Failed to report NemoGuardrails NetworkPolicy failure")
	}
}

// reportNetworkPolicyReady clears a previous failure after the desired policy
// reconciled. It does not set the overall Ready condition: that remains the
// Deployment and Route readiness decision.
func (r *NemoGuardrailsReconciler) reportNetworkPolicyReady(ctx context.Context, instance *nemoguardrailsv1alpha1.NemoGuardrails) {
	if cond := utils.GetStatusCondition(instance.Status.Conditions, nemoNetworkPolicyCondition); cond != nil &&
		cond.Status == corev1.ConditionTrue && cond.Reason == nemoNetworkPolicyReconciledReason {
		return
	}
	if _, err := r.updateStatus(ctx, instance, func(saved *nemoguardrailsv1alpha1.NemoGuardrails) {
		utils.SetResourceCondition(&saved.Status.Conditions, nemoNetworkPolicyStatusComponent, nemoNetworkPolicyReconciledReason, "operand NetworkPolicy is reconciled", corev1.ConditionTrue)
		if saved.Status.Phase == utils.PhaseError {
			saved.Status.Phase = utils.PhaseProgressing
		}
	}); err != nil {
		log.FromContext(ctx).Error(err, "Failed to report NemoGuardrails NetworkPolicy readiness")
	}
}

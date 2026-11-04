package nemo_guardrails

import (
	"context"
	"fmt"

	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
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

	// nemoNetworkPolicyManagerLabel marks a policy this controller reconciles.
	// Reconcile adopts an existing policy only when it carries this marker with
	// nemoNetworkPolicyManager, so an object at the deterministic policy name
	// that this operator did not author is never overwritten.
	nemoNetworkPolicyManagerLabel = "trustyai.opendatahub.io/network-policy-managed-by"
	nemoNetworkPolicyManager      = "trustyai-service-operator"

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
	// The admin listener (8444) is never granted.
	nemoAuthProxyPort = 8443
	nemoServerPort    = 8000

	// status condition keys reported for the operand policy. SetResourceCondition
	// appends "Ready" to the component name it is given.
	nemoNetworkPolicyCondition        = "NetworkPolicyReady"
	nemoNetworkPolicyStatusComponent  = "NetworkPolicy"
	nemoNetworkPolicyFailedReason     = "NetworkPolicyReconcileFailed"
	nemoNetworkPolicyReconciledReason = "NetworkPolicyReady"
)

// nemoNetworkPolicyOwner supplies an explicit GVK to shared identity and
// ownership helpers. API reads do not consistently preserve TypeMeta.
func nemoNetworkPolicyOwner(instance *nemoguardrailsv1alpha1.NemoGuardrails) *nemoguardrailsv1alpha1.NemoGuardrails {
	owner := instance.DeepCopy()
	owner.GetObjectKind().SetGroupVersionKind(nemoguardrailsv1alpha1.GroupVersion.WithKind("NemoGuardrails"))
	return owner
}

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

// buildNemoGuardrailsNetworkPolicy authors the operand NetworkPolicy for one
// NemoGuardrails instance in its workload namespace.
//
// Ingress is limited to the listeners the deployment actually exposes (the
// public Service listener; never the admin listener) and to documented callers:
// MaaS payload processing, the Istio-managed gateway data plane, and, while
// Route exposure is enabled, OpenShift ingress-policy-group namespaces. Callers
// outside that set require spec.networkPolicy.additionalIngress.
//
// Egress is the reviewed AllowAll compatibility profile: outbound destinations
// are entirely user-configured, so no complete allowlist can be derived from
// the custom resource. Callers must not treat it as exfiltration protection.
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

	// The policy selector is exactly the reserved identity labels: the shared
	// validator requires the selector to select the identity and nothing else.
	// Instance scoping comes from the per-CR owner-uid label; workload labels
	// (app, component, deploy-name) stay on the Pods and Deployment template.
	podSelector := identityLabels

	// MaaS payload processing runs next to the gateway and is the component that
	// posts guardrail checks to this server.
	ingress := []networkingv1.NetworkPolicyIngressRule{
		{
			From: []networkingv1.NetworkPolicyPeer{{
				NamespaceSelector: nemoNamespaceNameSelector(nemoMaaSCallerNamespace),
				PodSelector:       payloadProcessingSelector.DeepCopy(),
			}},
			Ports: []networkingv1.NetworkPolicyPort{nemoNetworkPolicyTCPPort(listenerPort)},
		},
		{
			// A payload-processing Deployment placed in the workload namespace
			// itself (shared AI Gateway namespace topologies) is the same
			// documented caller, scoped by its Pod labels only.
			From:  []networkingv1.NetworkPolicyPeer{{PodSelector: payloadProcessingSelector.DeepCopy()}},
			Ports: []networkingv1.NetworkPolicyPort{nemoNetworkPolicyTCPPort(listenerPort)},
		},
		{
			// Istio-managed gateway Pods in the default MaaS gateway namespace.
			From: []networkingv1.NetworkPolicyPeer{{
				NamespaceSelector: nemoNamespaceNameSelector(nemoMaaSCallerNamespace),
				PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
					nemoGatewayManagedLabel: nemoGatewayManagedValue,
				}},
			}},
			Ports: []networkingv1.NetworkPolicyPort{nemoNetworkPolicyTCPPort(listenerPort)},
		},
	}
	if instance.Spec.NetworkPolicy != nil {
		for _, peer := range instance.Spec.NetworkPolicy.AdditionalIngress {
			ingress = append(ingress, networkingv1.NetworkPolicyIngressRule{
				From: []networkingv1.NetworkPolicyPeer{{
					NamespaceSelector: peer.NamespaceSelector.DeepCopy(),
					PodSelector:       peer.PodSelector.DeepCopy(),
				}},
				Ports: []networkingv1.NetworkPolicyPort{nemoNetworkPolicyTCPPort(listenerPort)},
			})
		}
	}
	var ingressIntent *utils.NetworkPolicyIngressIntent
	if nemoRouteExposureEnabled(instance) {
		routerNamespaceSelector := metav1.LabelSelector{MatchLabels: map[string]string{
			nemoNetworkPolicyNamespaceGroupLabel: nemoNetworkPolicyIngressGroup,
		}}
		ingress = append(ingress, networkingv1.NetworkPolicyIngressRule{
			From:  []networkingv1.NetworkPolicyPeer{{NamespaceSelector: routerNamespaceSelector.DeepCopy()}},
			Ports: []networkingv1.NetworkPolicyPort{nemoNetworkPolicyTCPPort(listenerPort)},
		})
		ingressIntent = &utils.NetworkPolicyIngressIntent{NamespaceOnlyPeers: []utils.NetworkPolicyNamespaceOnlyPeerIntent{{
			NamespaceSelector: routerNamespaceSelector,
			Rationale: "OpenShift Route traffic is sourced by router Pods in ingress namespaces; no stable supported " +
				"router Pod selector is assumed. The peer is installed only while Route exposure is enabled.",
			ResidualRisk: "Any Pod in a namespace carrying the OpenShift ingress policy-group label can reach the " +
				"exposed NemoGuardrails listener. Managed gateways are granted separately; custom callers require " +
				"explicit additionalIngress peers. The authenticated admin listener is not granted. " +
				"Kubelet probes are node-originated and not constrainable by NetworkPolicy peer selectors.",
		}}}
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
// the controller during operand cleanup.
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

	owner := nemoNetworkPolicyOwner(instance)
	if err := r.decorateOwnedNetworkPolicy(owner, identity, desired.Policy); err != nil {
		return err
	}
	return r.applyNetworkPolicy(ctx, owner, identity, desired.Policy)
}

// decorateOwnedNetworkPolicy records ownership and the managed marker on the
// desired policy. Owner name and namespace annotations are always set, so the
// namespace-scoped and cross-namespace cases share one watch mapping.
func (r *NemoGuardrailsReconciler) decorateOwnedNetworkPolicy(owner *nemoguardrailsv1alpha1.NemoGuardrails, identity utils.NetworkPolicyIdentity, policy *networkingv1.NetworkPolicy) error {
	if err := utils.SetOwnerReference(owner, policy, r.Scheme); err != nil {
		return fmt.Errorf("set NemoGuardrails ownership on NetworkPolicy: %w", err)
	}
	identityLabels, err := identity.Labels()
	if err != nil {
		return err
	}
	labels := policy.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	for key, value := range identityLabels {
		labels[key] = value
	}
	labels[nemoNetworkPolicyManagerLabel] = nemoNetworkPolicyManager
	policy.SetLabels(labels)

	annotations := policy.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[utils.OwnerNameAnnotation] = owner.GetName()
	annotations[utils.OwnerNamespaceAnnotation] = owner.GetNamespace()
	policy.SetAnnotations(annotations)
	return nil
}

func (r *NemoGuardrailsReconciler) applyNetworkPolicy(ctx context.Context, owner *nemoguardrailsv1alpha1.NemoGuardrails, identity utils.NetworkPolicyIdentity, desired *networkingv1.NetworkPolicy) error {
	existing := &networkingv1.NetworkPolicy{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(desired), existing); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get NemoGuardrails NetworkPolicy: %w", err)
		}
		if err := r.Create(ctx, desired); err != nil {
			return fmt.Errorf("create NemoGuardrails NetworkPolicy %s/%s: %w", desired.Namespace, desired.Name, err)
		}
		return nil
	}
	if err := verifyNemoNetworkPolicyOwnership(existing, owner, identity); err != nil {
		return err
	}
	if !existing.DeletionTimestamp.IsZero() {
		return fmt.Errorf("NemoGuardrails NetworkPolicy %s/%s is deleting; retry after deletion completes", existing.Namespace, existing.Name)
	}

	updated := existing.DeepCopy()
	updated.Spec = *desired.Spec.DeepCopy()
	updated.Labels = mergeNemoLabels(existing.Labels, desired.Labels)
	updated.Annotations = mergeNemoLabels(existing.Annotations, desired.Annotations)
	if err := r.decorateOwnedNetworkPolicy(owner, identity, updated); err != nil {
		return err
	}

	baseline := existing.DeepCopy()
	normalizeNemoNetworkPolicy(baseline)
	normalizeNemoNetworkPolicy(updated)
	if apiequality.Semantic.DeepEqual(baseline.Spec, updated.Spec) &&
		apiequality.Semantic.DeepEqual(baseline.Labels, updated.Labels) &&
		apiequality.Semantic.DeepEqual(baseline.Annotations, updated.Annotations) &&
		apiequality.Semantic.DeepEqual(baseline.OwnerReferences, updated.OwnerReferences) {
		return nil
	}
	if err := r.Update(ctx, updated); err != nil {
		return fmt.Errorf("update NemoGuardrails NetworkPolicy %s/%s: %w", updated.Namespace, updated.Name, err)
	}
	return nil
}

// verifyNemoNetworkPolicyOwnership rejects an object that this operator did not
// author for this instance. An ownerless policy is adopted only when it carries
// the managed marker and this instance's identity labels.
func verifyNemoNetworkPolicyOwnership(existing *networkingv1.NetworkPolicy, owner client.Object, identity utils.NetworkPolicyIdentity) error {
	if existing == nil || owner == nil {
		return fmt.Errorf("existing NetworkPolicy and owner are required")
	}
	if ref := metav1.GetControllerOf(existing); ref != nil {
		refGroupVersion, err := schema.ParseGroupVersion(ref.APIVersion)
		if err != nil || ref.UID != owner.GetUID() || ref.Name != owner.GetName() || refGroupVersion.Group != identity.OwnerKind.Group || ref.Kind != identity.OwnerKind.Kind {
			return fmt.Errorf("NetworkPolicy %s/%s has a foreign controller owner", existing.Namespace, existing.Name)
		}
	}
	labels := existing.GetLabels()
	if labels[nemoNetworkPolicyManagerLabel] != nemoNetworkPolicyManager {
		return fmt.Errorf("NetworkPolicy %s/%s is not managed by %s", existing.Namespace, existing.Name, nemoNetworkPolicyManager)
	}
	if labels[utils.NetworkPolicyComponentLabel] != identity.Component || labels[utils.NetworkPolicyRoleLabel] != identity.Role {
		return fmt.Errorf("NetworkPolicy %s/%s identity labels do not match the NemoGuardrails workload identity", existing.Namespace, existing.Name)
	}
	if uid, ok := labels[utils.OwnerUIDLabel]; ok && uid != string(identity.OwnerUID) {
		return fmt.Errorf("NetworkPolicy %s/%s owner label contradicts this NemoGuardrails UID", existing.Namespace, existing.Name)
	}
	annotations := existing.GetAnnotations()
	if name, ok := annotations[utils.OwnerNameAnnotation]; ok && name != owner.GetName() {
		return fmt.Errorf("NetworkPolicy %s/%s owner name annotation contradicts this NemoGuardrails name", existing.Namespace, existing.Name)
	}
	if namespace, ok := annotations[utils.OwnerNamespaceAnnotation]; ok && namespace != owner.GetNamespace() {
		return fmt.Errorf("NetworkPolicy %s/%s owner namespace annotation contradicts this NemoGuardrails namespace", existing.Namespace, existing.Name)
	}
	return nil
}

func mergeNemoLabels(current, required map[string]string) map[string]string {
	if len(required) == 0 {
		return current
	}
	merged := make(map[string]string, len(current)+len(required))
	for key, value := range current {
		merged[key] = value
	}
	for key, value := range required {
		merged[key] = value
	}
	return merged
}

// normalizeNemoNetworkPolicy removes representations the API server may choose
// for empty collections, so drift detection compares meaning, not encoding.
func normalizeNemoNetworkPolicy(policy *networkingv1.NetworkPolicy) {
	if len(policy.Labels) == 0 {
		policy.Labels = nil
	}
	if len(policy.Annotations) == 0 {
		policy.Annotations = nil
	}
	if len(policy.Spec.Ingress) == 0 {
		policy.Spec.Ingress = nil
	}
	if len(policy.Spec.Egress) == 0 {
		policy.Spec.Egress = nil
	}
	for index := range policy.Spec.Egress {
		if len(policy.Spec.Egress[index].To) == 0 {
			policy.Spec.Egress[index].To = nil
		}
		if len(policy.Spec.Egress[index].Ports) == 0 {
			policy.Spec.Egress[index].Ports = nil
		}
	}
}

// stampNemoNetworkPolicyIdentity adds the reserved identity labels to a
// Deployment this controller is about to create or update. Only the template is
// touched: the immutable selector is never modified. Running Pods are replaced by
// the resulting rollout and are never edited in place.
func stampNemoNetworkPolicyIdentity(deployment *appsv1.Deployment, instance *nemoguardrailsv1alpha1.NemoGuardrails, identity utils.NetworkPolicyIdentity) error {
	if deployment == nil || instance == nil {
		return fmt.Errorf("deployment and NemoGuardrails instance are required")
	}
	if identity.OwnerUID != instance.GetUID() {
		return fmt.Errorf("policy identity does not belong to this NemoGuardrails UID")
	}
	if !utils.IsOwnedBy(deployment, instance) {
		return fmt.Errorf("refusing to label a NemoGuardrails Deployment that is not owned by this instance")
	}
	labels, err := identity.Labels()
	if err != nil {
		return err
	}
	if deployment.Spec.Selector == nil {
		return fmt.Errorf("NemoGuardrails Deployment selector is required")
	}
	for key, value := range labels {
		if selected, ok := deployment.Spec.Selector.MatchLabels[key]; ok && selected != value {
			return fmt.Errorf("immutable selector conflicts with reserved label %s", key)
		}
	}
	template := deployment.Spec.Template.DeepCopy()
	if template.Labels == nil {
		template.Labels = map[string]string{}
	}
	for key, value := range labels {
		template.Labels[key] = value
	}
	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil || !selector.Matches(klabels.Set(template.Labels)) {
		return fmt.Errorf("reserved labels conflict with the Deployment selector")
	}
	deployment.Spec.Template = *template
	return nil
}

// enqueueNemoFromNetworkPolicy maps a policy event back to its custom resource.
// It replaces Owns() for policies that cannot carry a controller reference
// because they live in the workload namespace rather than the CR namespace.
func (r *NemoGuardrailsReconciler) enqueueNemoFromNetworkPolicy(_ context.Context, object client.Object) []reconcile.Request {
	if !hasNemoNetworkPolicyIdentity(object) {
		return nil
	}
	annotations := object.GetAnnotations()
	name := annotations[utils.OwnerNameAnnotation]
	namespace := annotations[utils.OwnerNamespaceAnnotation]
	if name == "" || namespace == "" {
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
		labels[nemoNetworkPolicyManagerLabel] == nemoNetworkPolicyManager
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

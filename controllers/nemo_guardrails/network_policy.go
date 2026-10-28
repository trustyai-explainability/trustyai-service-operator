package nemo_guardrails

import (
	"context"
	"fmt"
	"reflect"

	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	// nemoAuthProxyPort is the kube-rbac-proxy secure listener used when the
	// CR requests authentication; nemoServerPort is the direct server listener.
	nemoAuthProxyPort = 8443
	nemoServerPort    = 8000
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
// It mirrors the controller's default: nil is treated as enabled.
func nemoRouteExposureEnabled(instance *nemoguardrailsv1alpha1.NemoGuardrails) bool {
	return instance.Spec.ExposeRoute == nil || *instance.Spec.ExposeRoute
}

// buildNemoGuardrailsNetworkPolicy authors the NemoGuardrails NetworkPolicy:
// a router ingress peer on the exposed listener plus a documented AllowAll
// egress baseline. The egress mode is a reviewed compatibility profile, not
// exfiltration protection; callers must not treat the policy as complete
// workload network isolation.
//
// The router peer is installed only while Route exposure is enabled
// (spec.exposeRoute, defaulting to true, mirroring the controller's Route
// reconciliation). With exposure disabled the policy keeps the Ingress
// direction with no allow rules, so the listener is isolated from every
// peer — the Service remains for in-cluster callers, but no traffic is
// authorized until separately agreed peer rules exist.
func buildNemoGuardrailsNetworkPolicy(instance *nemoguardrailsv1alpha1.NemoGuardrails) (utils.WorkloadNetworkPolicy, error) {
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

	routerNamespaceSelector := metav1.LabelSelector{MatchLabels: map[string]string{
		nemoNetworkPolicyNamespaceGroupLabel: nemoNetworkPolicyIngressGroup,
	}}

	// The policy selector is exactly the reserved identity labels: the shared
	// validator requires the selector to select the identity and nothing else.
	// Instance scoping comes from the per-CR owner-uid label; workload labels
	// (app, component, deploy-name) stay on the Pods and Deployment template.
	podSelector := identityLabels

	// Match the existing MaaS payload-processing NetworkPolicy contract. These
	// are actual traffic sources, not namespaces declaring AIGuardrail resources.
	ingress := []networkingv1.NetworkPolicyIngressRule{{
		From: []networkingv1.NetworkPolicyPeer{{
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "openshift-ingress"}},
			PodSelector:       &metav1.LabelSelector{MatchLabels: map[string]string{"gateway.istio.io/managed": "istio.io-gateway-controller"}},
		}},
		Ports: []networkingv1.NetworkPolicyPort{nemoNetworkPolicyTCPPort(nemoIngressListenerPort(instance))},
	}}
	if instance.Spec.NetworkPolicy != nil {
		for _, peer := range instance.Spec.NetworkPolicy.AdditionalIngress {
			ingress = append(ingress, networkingv1.NetworkPolicyIngressRule{
				From:  []networkingv1.NetworkPolicyPeer{{NamespaceSelector: peer.NamespaceSelector.DeepCopy(), PodSelector: peer.PodSelector.DeepCopy()}},
				Ports: []networkingv1.NetworkPolicyPort{nemoNetworkPolicyTCPPort(nemoIngressListenerPort(instance))},
			})
		}
	}
	var ingressIntent *utils.NetworkPolicyIngressIntent
	if nemoRouteExposureEnabled(instance) {
		ingress = append(ingress, networkingv1.NetworkPolicyIngressRule{
			From:  []networkingv1.NetworkPolicyPeer{{NamespaceSelector: routerNamespaceSelector.DeepCopy()}},
			Ports: []networkingv1.NetworkPolicyPort{nemoNetworkPolicyTCPPort(nemoIngressListenerPort(instance))},
		})
		ingressIntent = &utils.NetworkPolicyIngressIntent{NamespaceOnlyPeers: []utils.NetworkPolicyNamespaceOnlyPeerIntent{{
			NamespaceSelector: routerNamespaceSelector,
			Rationale: "OpenShift Route traffic is sourced by router Pods in ingress namespaces; no stable supported " +
				"router Pod selector is assumed. The peer is installed only while Route exposure is enabled.",
			ResidualRisk: "Any Pod in a namespace carrying the OpenShift ingress policy-group label can reach the " +
				"exposed NemoGuardrails listener. Managed gateways are granted separately; custom callers require " +
				"explicit additionalIngress peers. The authenticated admin listener is not granted. " +
				"Kubelet probes are node-originated and not constrainable by " +
				"NetworkPolicy peer selectors.",
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
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: instance.Namespace},
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

func (r *NemoGuardrailsReconciler) reconcileNetworkPolicy(ctx context.Context, instance *nemoguardrailsv1alpha1.NemoGuardrails) error {
	identity, err := nemoNetworkPolicyIdentity(instance)
	if err != nil {
		return err
	}
	desired, err := buildNemoGuardrailsNetworkPolicy(instance)
	if err != nil {
		return err
	}

	owner := nemoNetworkPolicyOwner(instance)
	deployment := &appsv1.Deployment{}
	key := client.ObjectKey{Name: instance.Name, Namespace: instance.Namespace}
	if err := r.Get(ctx, key, deployment); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get NemoGuardrails Deployment before NetworkPolicy reconciliation: %w", err)
		}
		deployment = nil
	} else if !nemoControllerReferenceMatches(deployment, owner, nemoguardrailsv1alpha1.GroupVersion.Group, "NemoGuardrails") {
		return fmt.Errorf("NemoGuardrails Deployment %s/%s is not controlled by this NemoGuardrails UID", deployment.Namespace, deployment.Name)
	}

	authority := utils.NetworkPolicyAuthority{Namespaces: map[string]bool{instance.Namespace: true}}
	if err := utils.ReconcileWorkloadNetworkPolicy(ctx, r.Client, r.Scheme, owner, identity, desired, authority); err != nil {
		return fmt.Errorf("reconcile NemoGuardrails NetworkPolicy: %w", err)
	}

	// The policy exists before any identity label is added to a running Pod.
	// This makes each Pod enter the policy selector only after its allow rules
	// have converged in the Kubernetes API.
	if deployment != nil {
		if err := r.labelOwnedNemoPods(ctx, instance, deployment, identity); err != nil {
			return err
		}
	}
	return nil
}

// labelOwnedNemoPods upgrades already-running Pods after the identity-selected
// policy has been reconciled. It edits metadata only after validating the
// Deployment -> ReplicaSet -> Pod controller chain. Selected Pods that fail
// the chain are foreign or stale candidates: they are skipped with a
// diagnostic and never labelled, but they do not block reconciliation of the
// owned workload.
func (r *NemoGuardrailsReconciler) labelOwnedNemoPods(ctx context.Context, instance *nemoguardrailsv1alpha1.NemoGuardrails, deployment *appsv1.Deployment, identity utils.NetworkPolicyIdentity) error {
	owner := nemoNetworkPolicyOwner(instance)
	if !nemoControllerReferenceMatches(deployment, owner, nemoguardrailsv1alpha1.GroupVersion.Group, "NemoGuardrails") {
		return fmt.Errorf("refusing to label Pods for an unowned NemoGuardrails Deployment")
	}
	if deployment.Spec.Selector == nil {
		return fmt.Errorf("NemoGuardrails Deployment selector is required to label existing Pods")
	}
	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		return fmt.Errorf("convert NemoGuardrails Deployment selector: %w", err)
	}

	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, client.InNamespace(instance.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return fmt.Errorf("list existing NemoGuardrails Pods for identity migration: %w", err)
	}
	reader := r.APIReader
	if reader == nil {
		reader = r.Client
	}
	identityLabels, err := identity.Labels()
	if err != nil {
		return err
	}

	for index := range pods.Items {
		pod := &pods.Items[index]
		// Pods matching the Deployment's public selector are not necessarily
		// part of this workload: a principal with Pod-create permission can
		// reproduce the selector labels, and stale Pods can outlive the
		// ReplicaSet of a replaced Deployment. Confirmed non-owned candidates
		// are skipped with a diagnostic instead of blocking the instance;
		// they never receive identity labels. Genuine API failures and
		// failures updating verified-owned Pods remain reconcile errors.
		replicaSetRef := metav1.GetControllerOf(pod)
		if replicaSetRef == nil || replicaSetRef.Kind != "ReplicaSet" {
			log.FromContext(ctx).Info("Skipping NemoGuardrails-selected Pod without a ReplicaSet controller during identity migration",
				"pod", client.ObjectKeyFromObject(pod))
			continue
		}
		replicaSetGV, err := schema.ParseGroupVersion(replicaSetRef.APIVersion)
		if err != nil || replicaSetGV.Group != "apps" {
			log.FromContext(ctx).Info("Skipping NemoGuardrails-selected Pod with an invalid ReplicaSet controller reference during identity migration",
				"pod", client.ObjectKeyFromObject(pod))
			continue
		}
		replicaSet := &appsv1.ReplicaSet{}
		if err := reader.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: replicaSetRef.Name}, replicaSet); err != nil {
			if apierrors.IsNotFound(err) {
				log.FromContext(ctx).Info("Skipping stale NemoGuardrails-selected Pod whose ReplicaSet no longer exists during identity migration",
					"pod", client.ObjectKeyFromObject(pod))
				continue
			}
			return fmt.Errorf("verify ReplicaSet for NemoGuardrails Pod %s/%s: %w", pod.Namespace, pod.Name, err)
		}
		if replicaSet.UID != replicaSetRef.UID || !nemoControllerReferenceMatches(replicaSet, deployment, "apps", "Deployment") {
			log.FromContext(ctx).Info("Skipping NemoGuardrails-selected Pod not controlled by the current Deployment during identity migration",
				"pod", client.ObjectKeyFromObject(pod))
			continue
		}

		changed := false
		for key, value := range identityLabels {
			if pod.Labels[key] != value {
				if pod.Labels == nil {
					pod.Labels = map[string]string{}
				}
				pod.Labels[key] = value
				changed = true
			}
		}
		if !changed {
			continue
		}
		if err := r.Update(ctx, pod); err != nil {
			return fmt.Errorf("add reserved NemoGuardrails identity to Pod %s/%s: %w", pod.Namespace, pod.Name, err)
		}
	}
	return nil
}

func nemoControllerReferenceMatches(object metav1.Object, owner metav1.Object, group, kind string) bool {
	ref := metav1.GetControllerOf(object)
	if ref == nil || ref.UID != owner.GetUID() || ref.Name != owner.GetName() || ref.Kind != kind {
		return false
	}
	refGV, err := schema.ParseGroupVersion(ref.APIVersion)
	return err == nil && refGV.Group == group
}

func (r *NemoGuardrailsReconciler) enqueueNemoFromPod(ctx context.Context, object client.Object) []reconcile.Request {
	pod, ok := object.(*corev1.Pod)
	if !ok || pod.Namespace == "" {
		return nil
	}
	reader := r.APIReader
	if reader == nil {
		reader = r.Client
	}
	replicaSetRef := metav1.GetControllerOf(pod)
	if replicaSetRef != nil && replicaSetRef.Kind == "ReplicaSet" {
		groupVersion, err := schema.ParseGroupVersion(replicaSetRef.APIVersion)
		if err == nil && groupVersion.Group == "apps" {
			replicaSet := &appsv1.ReplicaSet{}
			if err := reader.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: replicaSetRef.Name}, replicaSet); err == nil && replicaSet.UID == replicaSetRef.UID {
				deploymentRef := metav1.GetControllerOf(replicaSet)
				if deploymentRef != nil && deploymentRef.Kind == "Deployment" {
					deploymentGroupVersion, parseErr := schema.ParseGroupVersion(deploymentRef.APIVersion)
					if parseErr == nil && deploymentGroupVersion.Group == "apps" {
						deployment := &appsv1.Deployment{}
						if err := reader.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: deploymentRef.Name}, deployment); err == nil &&
							deployment.UID == deploymentRef.UID &&
							nemoControllerReferenceMatches(replicaSet, deployment, "apps", "Deployment") {
							ownerRef := metav1.GetControllerOf(deployment)
							if ownerRef != nil && ownerRef.Kind == "NemoGuardrails" {
								ownerGroupVersion, ownerErr := schema.ParseGroupVersion(ownerRef.APIVersion)
								if ownerErr == nil && ownerGroupVersion.Group == nemoguardrailsv1alpha1.GroupVersion.Group {
									return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: pod.Namespace, Name: ownerRef.Name}}}
								}
							}
						}
					}
				}
			}
		}
	}

	// Labels are only a best-effort enqueue hint. Reconcile re-reads the CR and
	// verifies Deployment ownership before changing any workload object.
	name := pod.Labels["app.kubernetes.io/instance"]
	if name == "" {
		name = pod.Labels["app"]
	}
	if name == "" {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: pod.Namespace, Name: name}}}
}

func hasNemoPodIdentityHint(object client.Object) bool {
	if object == nil {
		return false
	}
	labels := object.GetLabels()
	if labels[utils.NetworkPolicyComponentLabel] == nemoNetworkPolicyComponent {
		return true
	}
	return labels["app.kubernetes.io/part-of"] == "trustyai" &&
		labels["app"] != "" &&
		labels["component"] == labels["app"] &&
		labels["app.kubernetes.io/instance"] == labels["app"] &&
		labels["app.kubernetes.io/name"] == labels["app"]
}

func nemoPodLabelsChanged(oldObject, newObject client.Object) bool {
	return (hasNemoPodIdentityHint(oldObject) || hasNemoPodIdentityHint(newObject)) &&
		!reflect.DeepEqual(nemoObjectLabels(oldObject), nemoObjectLabels(newObject))
}

func nemoObjectLabels(object client.Object) map[string]string {
	if object == nil {
		return nil
	}
	return object.GetLabels()
}

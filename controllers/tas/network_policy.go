package tas

import (
	"context"
	"fmt"
	"reflect"

	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
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
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	tasNetworkPolicyNamespaceGroupLabel = "network.openshift.io/policy-group"
	tasNetworkPolicyIngressGroup        = "ingress"
	tasNetworkPolicyMonitoringGroup     = "monitoring"
	tasPrometheusNameLabel              = "app.kubernetes.io/name"
	tasPrometheusComponentLabel         = "app.kubernetes.io/component"
	tasPrometheusNameValue              = "prometheus"
	tasPrometheusComponentValue         = "prometheus"
	tasModelMeshServiceLabel            = "modelmesh-service"
	tasModelMeshServiceValue            = "modelmesh-serving"
	tasKServeInferenceServiceLabel      = "serving.kserve.io/inferenceservice"
	tasNetworkPolicyRole                = "service"
	tasNetworkPolicyPurpose             = "service"
)

// tasNetworkPolicyOwner supplies an explicit GVK to shared identity and
// ownership helpers. API reads do not consistently preserve TypeMeta.
func tasNetworkPolicyOwner(instance *trustyaiopendatahubiov1.TrustyAIService) *trustyaiopendatahubiov1.TrustyAIService {
	owner := instance.DeepCopy()
	owner.GetObjectKind().SetGroupVersionKind(trustyaiopendatahubiov1.GroupVersion.WithKind("TrustyAIService"))
	return owner
}

func tasNetworkPolicyIdentity(instance *trustyaiopendatahubiov1.TrustyAIService) (utils.NetworkPolicyIdentity, error) {
	if instance == nil || instance.Name == "" || instance.Namespace == "" || instance.UID == "" {
		return utils.NetworkPolicyIdentity{}, fmt.Errorf("persisted TrustyAIService name, namespace, and UID are required")
	}
	return utils.NetworkPolicyIdentity{
		OwnerKind: schema.GroupKind{Group: trustyaiopendatahubiov1.GroupVersion.Group, Kind: "TrustyAIService"},
		OwnerUID:  instance.UID,
		Component: "tas",
		Role:      tasNetworkPolicyRole,
	}, nil
}

func tasNetworkPolicyTCPPort(port int32) networkingv1.NetworkPolicyPort {
	protocol := corev1.ProtocolTCP
	value := intstr.FromInt32(port)
	return networkingv1.NetworkPolicyPort{Protocol: &protocol, Port: &value}
}

// buildTASNetworkPolicy authors the TAS NetworkPolicy: qualified ingress
// peers plus a documented AllowAll egress baseline. The egress mode is a
// reviewed compatibility profile, not exfiltration protection; callers must
// not treat the policy as complete workload network isolation.
func buildTASNetworkPolicy(instance *trustyaiopendatahubiov1.TrustyAIService) (utils.WorkloadNetworkPolicy, error) {
	identity, err := tasNetworkPolicyIdentity(instance)
	if err != nil {
		return utils.WorkloadNetworkPolicy{}, err
	}
	identityLabels, err := identity.Labels()
	if err != nil {
		return utils.WorkloadNetworkPolicy{}, err
	}
	name, err := identity.Name(tasNetworkPolicyPurpose)
	if err != nil {
		return utils.WorkloadNetworkPolicy{}, err
	}

	routerNamespaceSelector := metav1.LabelSelector{MatchLabels: map[string]string{
		tasNetworkPolicyNamespaceGroupLabel: tasNetworkPolicyIngressGroup,
	}}
	monitoringNamespaceSelector := &metav1.LabelSelector{MatchLabels: map[string]string{
		tasNetworkPolicyNamespaceGroupLabel: tasNetworkPolicyMonitoringGroup,
	}}
	prometheusPodSelector := &metav1.LabelSelector{MatchLabels: map[string]string{
		tasPrometheusNameLabel:      tasPrometheusNameValue,
		tasPrometheusComponentLabel: tasPrometheusComponentValue,
	}}

	// The policy selector is exactly the reserved identity labels: the shared
	// validator requires the selector to select the identity and nothing else.
	// Instance scoping comes from the per-CR owner-uid label; workload labels
	// (app, name, part-of) stay on the Pods and Deployment template.
	podSelector := identityLabels

	ingressIntent := &utils.NetworkPolicyIngressIntent{NamespaceOnlyPeers: []utils.NetworkPolicyNamespaceOnlyPeerIntent{{
		NamespaceSelector: routerNamespaceSelector,
		Rationale:         "OpenShift Route traffic is sourced by router Pods in ingress namespaces; a stable supported router Pod selector is not assumed.",
		ResidualRisk:      "Any Pod in a namespace carrying the OpenShift ingress policy-group label can reach the TAS authenticated listener on TCP 8443.",
	}}}

	egressIntent := &utils.NetworkPolicyEgressIntent{
		Mode: utils.NetworkPolicyAllowAll,
		Rationale: "Trusted AI service outbound traffic includes DNS resolution, Kubernetes API " +
			"authorization through the OAuth proxy, optional remote database storage, and " +
			"model-serving or platform destinations that depend on deployment configuration. " +
			"No complete outbound allowlist is established for the supported storage, " +
			"authentication and model-serving modes.",
		ResidualRisk: "Outbound traffic is unrestricted, including in database-storage and " +
			"proxy-authorization modes. This provides no exfiltration protection and can broaden " +
			"customer NetworkPolicies because ordinary grants are additive. All containers share " +
			"the Pod network boundary. A Restricted egress profile remains a follow-up decision " +
			"with platform and security owners.",
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
				Ingress: []networkingv1.NetworkPolicyIngressRule{
					{
						From:  []networkingv1.NetworkPolicyPeer{{NamespaceSelector: routerNamespaceSelector.DeepCopy()}},
						Ports: []networkingv1.NetworkPolicyPort{tasNetworkPolicyTCPPort(8443)},
					},
					{
						From: []networkingv1.NetworkPolicyPeer{{
							NamespaceSelector: monitoringNamespaceSelector.DeepCopy(),
							PodSelector:       prometheusPodSelector.DeepCopy(),
						}},
						Ports: []networkingv1.NetworkPolicyPort{
							tasNetworkPolicyTCPPort(8443),
							tasNetworkPolicyTCPPort(8080),
						},
					},
					{
						From: []networkingv1.NetworkPolicyPeer{
							{PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
								tasModelMeshServiceLabel: tasModelMeshServiceValue,
							}}},
							{PodSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
								Key:      tasKServeInferenceServiceLabel,
								Operator: metav1.LabelSelectorOpExists,
							}}}},
						},
						Ports: []networkingv1.NetworkPolicyPort{tasNetworkPolicyTCPPort(4443)},
					},
				},
				Egress: []networkingv1.NetworkPolicyEgressRule{{}},
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

func (r *TrustyAIServiceReconciler) reconcileNetworkPolicy(ctx context.Context, instance *trustyaiopendatahubiov1.TrustyAIService) error {
	identity, err := tasNetworkPolicyIdentity(instance)
	if err != nil {
		return err
	}
	desired, err := buildTASNetworkPolicy(instance)
	if err != nil {
		return err
	}

	owner := tasNetworkPolicyOwner(instance)
	deployment := &appsv1.Deployment{}
	key := client.ObjectKey{Name: instance.Name, Namespace: instance.Namespace}
	if err := r.Get(ctx, key, deployment); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get TAS Deployment before NetworkPolicy reconciliation: %w", err)
		}
		deployment = nil
	} else if !controllerReferenceMatches(deployment, owner, trustyaiopendatahubiov1.GroupVersion.Group, "TrustyAIService") {
		return fmt.Errorf("TAS Deployment %s/%s is not controlled by this TrustyAIService UID", deployment.Namespace, deployment.Name)
	}

	authority := utils.NetworkPolicyAuthority{Namespaces: map[string]bool{instance.Namespace: true}}
	if err := utils.ReconcileWorkloadNetworkPolicy(ctx, r.Client, r.Scheme, owner, identity, desired, authority); err != nil {
		return fmt.Errorf("reconcile TAS NetworkPolicy: %w", err)
	}

	// The policy exists before any identity label is added to a running Pod.
	// This makes each Pod enter the policy selector only after its allow rules
	// have converged in the Kubernetes API.
	if deployment != nil {
		if err := r.labelOwnedTASPods(ctx, instance, deployment, identity); err != nil {
			return err
		}
	}
	return nil
}

// labelOwnedTASPods upgrades already-running Pods after the identity-selected
// policy has been reconciled. It edits metadata only after validating the
// Deployment -> ReplicaSet -> Pod controller chain.
func (r *TrustyAIServiceReconciler) labelOwnedTASPods(ctx context.Context, instance *trustyaiopendatahubiov1.TrustyAIService, deployment *appsv1.Deployment, identity utils.NetworkPolicyIdentity) error {
	owner := tasNetworkPolicyOwner(instance)
	if !controllerReferenceMatches(deployment, owner, trustyaiopendatahubiov1.GroupVersion.Group, "TrustyAIService") {
		return fmt.Errorf("refusing to label Pods for an unowned TAS Deployment")
	}
	if deployment.Spec.Selector == nil {
		return fmt.Errorf("TAS Deployment selector is required to label existing Pods")
	}
	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		return fmt.Errorf("convert TAS Deployment selector: %w", err)
	}

	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, client.InNamespace(instance.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return fmt.Errorf("list existing TAS Pods for identity migration: %w", err)
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
		replicaSetRef := metav1.GetControllerOf(pod)
		if replicaSetRef == nil || replicaSetRef.Kind != "ReplicaSet" {
			return fmt.Errorf("TAS-selected Pod %s/%s has no ReplicaSet controller; refusing identity migration", pod.Namespace, pod.Name)
		}
		replicaSetGV, err := schema.ParseGroupVersion(replicaSetRef.APIVersion)
		if err != nil || replicaSetGV.Group != "apps" {
			return fmt.Errorf("TAS-selected Pod %s/%s has an invalid ReplicaSet controller reference", pod.Namespace, pod.Name)
		}
		replicaSet := &appsv1.ReplicaSet{}
		if err := reader.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: replicaSetRef.Name}, replicaSet); err != nil {
			return fmt.Errorf("verify ReplicaSet for TAS Pod %s/%s: %w", pod.Namespace, pod.Name, err)
		}
		if replicaSet.UID != replicaSetRef.UID || !controllerReferenceMatches(replicaSet, deployment, "apps", "Deployment") {
			return fmt.Errorf("TAS-selected Pod %s/%s is not controlled by the current Deployment", pod.Namespace, pod.Name)
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
			return fmt.Errorf("add reserved TAS identity to Pod %s/%s: %w", pod.Namespace, pod.Name, err)
		}
	}
	return nil
}

func (r *TrustyAIServiceReconciler) ensureTASDeploymentIdentity(ctx context.Context, instance *trustyaiopendatahubiov1.TrustyAIService, deployment *appsv1.Deployment) error {
	owner := tasNetworkPolicyOwner(instance)
	if !controllerReferenceMatches(deployment, owner, trustyaiopendatahubiov1.GroupVersion.Group, "TrustyAIService") {
		return fmt.Errorf("refusing to label an unowned TAS Deployment")
	}
	identity, err := tasNetworkPolicyIdentity(instance)
	if err != nil {
		return err
	}
	before := deployment.Spec.Template.Labels
	if err := utils.LabelOwnedNetworkPolicyDeployment(deployment, r.Scheme, owner, identity); err != nil {
		return fmt.Errorf("add reserved TAS identity to Deployment template: %w", err)
	}
	if reflect.DeepEqual(before, deployment.Spec.Template.Labels) {
		return nil
	}
	if err := r.Update(ctx, deployment); err != nil {
		return fmt.Errorf("update TAS Deployment with NetworkPolicy identity labels: %w", err)
	}
	return nil
}

func controllerReferenceMatches(object metav1.Object, owner metav1.Object, group, kind string) bool {
	ref := metav1.GetControllerOf(object)
	if ref == nil || ref.UID != owner.GetUID() || ref.Name != owner.GetName() || ref.Kind != kind {
		return false
	}
	refGV, err := schema.ParseGroupVersion(ref.APIVersion)
	return err == nil && refGV.Group == group
}

func (r *TrustyAIServiceReconciler) enqueueTASFromPod(ctx context.Context, object client.Object) []reconcile.Request {
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
							controllerReferenceMatches(replicaSet, deployment, "apps", "Deployment") {
							ownerRef := metav1.GetControllerOf(deployment)
							if ownerRef != nil && ownerRef.Kind == "TrustyAIService" {
								ownerGroupVersion, ownerErr := schema.ParseGroupVersion(ownerRef.APIVersion)
								if ownerErr == nil && ownerGroupVersion.Group == trustyaiopendatahubiov1.GroupVersion.Group {
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

func hasTASPodIdentityHint(object client.Object) bool {
	if object == nil {
		return false
	}
	labels := object.GetLabels()
	if labels[utils.NetworkPolicyComponentLabel] == "tas" {
		return true
	}
	return labels["app.kubernetes.io/part-of"] == "trustyai" &&
		labels["app"] != "" &&
		labels["app.kubernetes.io/instance"] == labels["app"] &&
		labels["app.kubernetes.io/name"] == labels["app"]
}

func tasPodLabelsChanged(oldObject, newObject client.Object) bool {
	return (hasTASPodIdentityHint(oldObject) || hasTASPodIdentityHint(newObject)) &&
		!reflect.DeepEqual(objectLabels(oldObject), objectLabels(newObject))
}

func objectLabels(object client.Object) map[string]string {
	if object == nil {
		return nil
	}
	return object.GetLabels()
}

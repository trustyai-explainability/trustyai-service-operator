package tas

import (
	"context"
	"testing"

	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestBuildTASNetworkPolicy(t *testing.T) {
	instance := testTASNetworkPolicyOwner()
	policy, err := buildTASNetworkPolicy(instance)
	if err != nil {
		t.Fatalf("buildTASNetworkPolicy() error = %v", err)
	}
	if err := utils.ValidateNetworkPolicy(policy); err != nil {
		t.Fatalf("ValidateNetworkPolicy() error = %v", err)
	}

	wantSelector := map[string]string{
		"app":                        instance.Name,
		"app.kubernetes.io/instance": instance.Name,
		"app.kubernetes.io/part-of":  "trustyai",
	}
	if len(policy.Spec.PodSelector.MatchLabels) != len(wantSelector) {
		t.Fatalf("pod selector = %#v, want %#v", policy.Spec.PodSelector.MatchLabels, wantSelector)
	}
	for key, value := range wantSelector {
		if policy.Spec.PodSelector.MatchLabels[key] != value {
			t.Errorf("pod selector[%q] = %q, want %q", key, policy.Spec.PodSelector.MatchLabels[key], value)
		}
	}
	if len(policy.Spec.PolicyTypes) != 1 || policy.Spec.PolicyTypes[0] != networkingv1.PolicyTypeIngress {
		t.Fatalf("policyTypes = %v, want [Ingress]", policy.Spec.PolicyTypes)
	}
	if len(policy.Spec.Egress) != 0 {
		t.Fatalf("egress rules = %#v, want none", policy.Spec.Egress)
	}
	if len(policy.Spec.Ingress) != 4 {
		t.Fatalf("got %d ingress rules, want 4", len(policy.Spec.Ingress))
	}

	assertPeerAndPort(t, policy.Spec.Ingress[0], 8443, func(peer networkingv1.NetworkPolicyPeer) bool {
		return peer.NamespaceSelector != nil && peer.NamespaceSelector.MatchLabels[networkPolicyNamespaceGroupLabel] == networkPolicyIngressGroup && peer.PodSelector == nil
	})
	assertPeerAndPort(t, policy.Spec.Ingress[1], 8443, func(peer networkingv1.NetworkPolicyPeer) bool {
		return isPrometheusPeer(peer)
	})
	assertPeerAndPort(t, policy.Spec.Ingress[2], 8080, func(peer networkingv1.NetworkPolicyPeer) bool {
		return isPrometheusPeer(peer)
	})
	assertPeerAndPort(t, policy.Spec.Ingress[3], 4443, func(peer networkingv1.NetworkPolicyPeer) bool {
		if peer.PodSelector == nil {
			return false
		}
		if peer.PodSelector.MatchLabels[modelMeshServiceLabel] == modelMeshServiceValue {
			return true
		}
		for _, expression := range peer.PodSelector.MatchExpressions {
			if expression.Key == kserveInferenceServiceLabel && expression.Operator == metav1.LabelSelectorOpExists {
				return true
			}
		}
		return false
	})

	if policy.Labels[utils.NetworkPolicyOwnerUIDLabel] != string(instance.UID) {
		t.Errorf("owner UID label = %q, want %q", policy.Labels[utils.NetworkPolicyOwnerUIDLabel], instance.UID)
	}
}

func TestBuildTASNetworkPolicyRejectsMissingIdentity(t *testing.T) {
	if _, err := buildTASNetworkPolicy(nil); err == nil {
		t.Fatal("buildTASNetworkPolicy(nil) succeeded, want error")
	}
	instance := testTASNetworkPolicyOwner()
	instance.UID = ""
	if _, err := buildTASNetworkPolicy(instance); err == nil {
		t.Fatal("buildTASNetworkPolicy() without owner UID succeeded, want error")
	}
}

func TestReconcileTASNetworkPolicySetsOwnershipAndRepairsDrift(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("register core types: %v", err)
	}
	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatalf("register NetworkPolicy type: %v", err)
	}
	if err := trustyaiopendatahubiov1.AddToScheme(scheme); err != nil {
		t.Fatalf("register TrustyAIService type: %v", err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := &TrustyAIServiceReconciler{Client: c, Scheme: scheme}
	instance := testTASNetworkPolicyOwner()
	ctx := context.Background()

	if err := reconciler.reconcileNetworkPolicy(instance, ctx); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}

	key := client.ObjectKey{Name: policyNameForTest(t, instance.Name), Namespace: instance.Namespace}
	actual := &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, key, actual); err != nil {
		t.Fatalf("get NetworkPolicy: %v", err)
	}
	if len(actual.OwnerReferences) != 1 || actual.OwnerReferences[0].UID != instance.UID || actual.OwnerReferences[0].Controller == nil || !*actual.OwnerReferences[0].Controller {
		t.Fatalf("owner references = %#v, want controller reference to TrustyAIService UID %q", actual.OwnerReferences, instance.UID)
	}

	actual.Spec.Ingress = nil
	if err := c.Update(ctx, actual); err != nil {
		t.Fatalf("simulate policy drift: %v", err)
	}
	if err := reconciler.reconcileNetworkPolicy(instance, ctx); err != nil {
		t.Fatalf("reconcile drifted NetworkPolicy: %v", err)
	}
	actual = &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, key, actual); err != nil {
		t.Fatalf("get repaired NetworkPolicy: %v", err)
	}
	if len(actual.Spec.Ingress) != 4 {
		t.Errorf("repaired ingress rules = %d, want 4", len(actual.Spec.Ingress))
	}
}

func testTASNetworkPolicyOwner() *trustyaiopendatahubiov1.TrustyAIService {
	return &trustyaiopendatahubiov1.TrustyAIService{ObjectMeta: metav1.ObjectMeta{
		Name:      "trustyai-example",
		Namespace: "trustyai-namespace",
		UID:       types.UID("trustyai-owner-uid"),
	}}
}

func policyNameForTest(t *testing.T, workloadName string) string {
	t.Helper()
	name, err := utils.NetworkPolicyName(workloadName)
	if err != nil {
		t.Fatalf("NetworkPolicyName() error = %v", err)
	}
	return name
}

func isPrometheusPeer(peer networkingv1.NetworkPolicyPeer) bool {
	return peer.NamespaceSelector != nil &&
		peer.NamespaceSelector.MatchLabels[networkPolicyNamespaceGroupLabel] == networkPolicyMonitoringGroup &&
		peer.PodSelector != nil &&
		peer.PodSelector.MatchLabels[prometheusNameLabel] == prometheusNameValue &&
		peer.PodSelector.MatchLabels[prometheusComponentLabel] == prometheusComponentValue
}

func assertPeerAndPort(t *testing.T, rule networkingv1.NetworkPolicyIngressRule, wantPort int32, wantPeer func(networkingv1.NetworkPolicyPeer) bool) {
	t.Helper()
	if len(rule.From) == 0 {
		t.Fatal("ingress rule has no peers")
	}
	matched := false
	for _, peer := range rule.From {
		if wantPeer(peer) {
			matched = true
			break
		}
	}
	if !matched {
		t.Errorf("ingress peers %#v do not include expected peer", rule.From)
	}
	if len(rule.Ports) != 1 || rule.Ports[0].Port == nil || rule.Ports[0].Port.IntVal != wantPort || rule.Ports[0].Protocol == nil || *rule.Ports[0].Protocol != corev1.ProtocolTCP {
		t.Errorf("ingress ports = %#v, want TCP %d", rule.Ports, wantPort)
	}
}

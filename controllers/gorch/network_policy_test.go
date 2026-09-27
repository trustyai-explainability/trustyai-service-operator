package gorch

import (
	"context"
	"reflect"
	"testing"

	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestBuildGORCHNetworkPolicy(t *testing.T) {
	tests := []struct {
		name      string
		owner     *gorchv1alpha1.GuardrailsOrchestrator
		wantRules []expectedGORCHRule
	}{
		{
			name:      "direct mode with gateway and built-in detectors",
			owner:     testGORCHNetworkPolicyOwner(false, false, true, true),
			wantRules: []expectedGORCHRule{{8032, false}, {8034, false}, {8090, false}, {8080, false}, {8080, true}},
		},
		{
			name:      "auth proxy mode with gateway and built-in detectors",
			owner:     testGORCHNetworkPolicyOwner(true, false, true, true),
			wantRules: []expectedGORCHRule{{8432, false}, {8034, false}, {8490, false}, {8480, false}, {8080, true}},
		},
		{
			name:      "disabled main service only exposes enabled detector",
			owner:     testGORCHNetworkPolicyOwner(false, true, false, true),
			wantRules: []expectedGORCHRule{{8080, false}, {8080, true}},
		},
		{
			name:      "disabled optional features do not leave rules behind",
			owner:     testGORCHNetworkPolicyOwner(false, false, false, false),
			wantRules: []expectedGORCHRule{{8032, false}, {8034, false}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy, err := buildGORCHNetworkPolicy(tt.owner)
			if err != nil {
				t.Fatalf("buildGORCHNetworkPolicy() error = %v", err)
			}
			if err := utils.ValidateNetworkPolicy(policy); err != nil {
				t.Fatalf("ValidateNetworkPolicy() error = %v", err)
			}

			wantSelector := map[string]string{
				"app":                        tt.owner.Name,
				"component":                  tt.owner.Name,
				"deploy-name":                tt.owner.Name,
				"app.kubernetes.io/instance": tt.owner.Name,
				"app.kubernetes.io/name":     tt.owner.Name,
				"app.kubernetes.io/part-of":  "trustyai",
			}
			if !reflect.DeepEqual(policy.Spec.PodSelector.MatchLabels, wantSelector) {
				t.Errorf("pod selector = %#v, want %#v", policy.Spec.PodSelector.MatchLabels, wantSelector)
			}
			if len(policy.Spec.PolicyTypes) != 1 || policy.Spec.PolicyTypes[0] != networkingv1.PolicyTypeIngress {
				t.Errorf("policyTypes = %v, want [Ingress]", policy.Spec.PolicyTypes)
			}
			if len(policy.Spec.Egress) != 0 {
				t.Errorf("egress rules = %#v, want none", policy.Spec.Egress)
			}
			if len(policy.Spec.Ingress) != len(tt.wantRules) {
				t.Fatalf("got %d ingress rules, want %d: %#v", len(policy.Spec.Ingress), len(tt.wantRules), policy.Spec.Ingress)
			}
			for _, expected := range tt.wantRules {
				assertGORCHRule(t, policy.Spec.Ingress, expected)
			}
			if policy.Labels[utils.NetworkPolicyOwnerUIDLabel] != string(tt.owner.UID) {
				t.Errorf("owner UID label = %q, want %q", policy.Labels[utils.NetworkPolicyOwnerUIDLabel], tt.owner.UID)
			}
		})
	}
}

func TestBuildGORCHNetworkPolicyRejectsMissingIdentity(t *testing.T) {
	if _, err := buildGORCHNetworkPolicy(nil); err == nil {
		t.Fatal("buildGORCHNetworkPolicy(nil) succeeded, want error")
	}
	owner := testGORCHNetworkPolicyOwner(false, false, true, false)
	owner.UID = ""
	if _, err := buildGORCHNetworkPolicy(owner); err == nil {
		t.Fatal("buildGORCHNetworkPolicy() without owner UID succeeded, want error")
	}
}

func TestReconcileGORCHNetworkPolicySetsOwnershipAndRepairsDrift(t *testing.T) {
	scheme := runtime.NewScheme()
	for _, addToScheme := range []func(*runtime.Scheme) error{
		corev1.AddToScheme,
		networkingv1.AddToScheme,
		gorchv1alpha1.AddToScheme,
	} {
		if err := addToScheme(scheme); err != nil {
			t.Fatalf("register scheme type: %v", err)
		}
	}

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := &GuardrailsOrchestratorReconciler{Client: c, Scheme: scheme}
	owner := testGORCHNetworkPolicyOwner(false, false, true, true)
	ctx := context.Background()

	if err := reconciler.reconcileNetworkPolicy(ctx, owner); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}
	name, err := utils.NetworkPolicyName("gorch-" + owner.Name)
	if err != nil {
		t.Fatalf("NetworkPolicyName(): %v", err)
	}
	key := client.ObjectKey{Name: name, Namespace: owner.Namespace}
	actual := &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, key, actual); err != nil {
		t.Fatalf("get NetworkPolicy: %v", err)
	}
	if len(actual.OwnerReferences) != 1 || actual.OwnerReferences[0].UID != owner.UID || actual.OwnerReferences[0].Controller == nil || !*actual.OwnerReferences[0].Controller {
		t.Fatalf("owner references = %#v, want controller reference to GuardrailsOrchestrator UID %q", actual.OwnerReferences, owner.UID)
	}

	actual.Spec.Ingress = nil
	if err := c.Update(ctx, actual); err != nil {
		t.Fatalf("simulate policy drift: %v", err)
	}
	if err := reconciler.reconcileNetworkPolicy(ctx, owner); err != nil {
		t.Fatalf("reconcile drifted NetworkPolicy: %v", err)
	}
	actual = &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, key, actual); err != nil {
		t.Fatalf("get repaired NetworkPolicy: %v", err)
	}
	if len(actual.Spec.Ingress) != 5 {
		t.Errorf("repaired ingress rules = %d, want 5", len(actual.Spec.Ingress))
	}

	if err := c.Delete(ctx, actual); err != nil {
		t.Fatalf("delete NetworkPolicy: %v", err)
	}
	if err := reconciler.reconcileNetworkPolicy(ctx, owner); err != nil {
		t.Fatalf("recreate deleted NetworkPolicy: %v", err)
	}
	actual = &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, key, actual); err != nil {
		t.Fatalf("get recreated NetworkPolicy: %v", err)
	}
}

type expectedGORCHRule struct {
	port       int32
	monitoring bool
}

func assertGORCHRule(t *testing.T, rules []networkingv1.NetworkPolicyIngressRule, expected expectedGORCHRule) {
	t.Helper()
	for _, rule := range rules {
		if len(rule.Ports) != 1 || rule.Ports[0].Port == nil || rule.Ports[0].Port.IntVal != expected.port || rule.Ports[0].Protocol == nil || *rule.Ports[0].Protocol != corev1.ProtocolTCP {
			continue
		}
		if len(rule.From) != 1 {
			continue
		}
		peer := rule.From[0]
		if expected.monitoring {
			if isGORCHPrometheusPeer(peer) {
				return
			}
		} else if isGORCHRouterPeer(peer) {
			return
		}
	}
	t.Errorf("no ingress rule for TCP %d from monitoring=%t peer", expected.port, expected.monitoring)
}

func isGORCHRouterPeer(peer networkingv1.NetworkPolicyPeer) bool {
	return peer.NamespaceSelector != nil &&
		reflect.DeepEqual(peer.NamespaceSelector.MatchLabels, map[string]string{
			gorchNetworkPolicyNamespaceGroupLabel: gorchNetworkPolicyIngressGroup,
		}) && peer.PodSelector == nil && peer.IPBlock == nil
}

func isGORCHPrometheusPeer(peer networkingv1.NetworkPolicyPeer) bool {
	return peer.NamespaceSelector != nil &&
		reflect.DeepEqual(peer.NamespaceSelector.MatchLabels, map[string]string{
			gorchNetworkPolicyNamespaceGroupLabel: gorchNetworkPolicyMonitoringGroup,
		}) && peer.PodSelector != nil &&
		reflect.DeepEqual(peer.PodSelector.MatchLabels, map[string]string{
			gorchPrometheusNameLabel:      gorchPrometheusNameValue,
			gorchPrometheusComponentLabel: gorchPrometheusComponentValue,
		}) && peer.IPBlock == nil
}

func testGORCHNetworkPolicyOwner(auth, disableMain, gateway, detectors bool) *gorchv1alpha1.GuardrailsOrchestrator {
	owner := &gorchv1alpha1.GuardrailsOrchestrator{ObjectMeta: metav1.ObjectMeta{
		Name:      "guardrails-example",
		Namespace: "trustyai-namespace",
		UID:       types.UID("gorch-owner-uid"),
	}, Spec: gorchv1alpha1.GuardrailsOrchestratorSpec{
		DisableOrchestrator:     disableMain,
		EnableGuardrailsGateway: gateway,
		EnableBuiltInDetectors:  detectors,
	}}
	if auth {
		owner.Annotations = map[string]string{constants.AuthAnnotationKey: "true"}
	}
	return owner
}

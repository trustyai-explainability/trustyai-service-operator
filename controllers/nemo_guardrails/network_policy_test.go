package nemo_guardrails

import (
	"context"
	"reflect"
	"testing"

	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
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

func TestBuildNemoNetworkPolicy(t *testing.T) {
	tests := []struct {
		name      string
		owner     *nemoguardrailsv1alpha1.NemoGuardrails
		wantPort  int32
		wantRules int
	}{
		{
			name:      "direct mode defaults to an exposed route",
			owner:     testNemoNetworkPolicyOwner(false, nil),
			wantPort:  8000,
			wantRules: 1,
		},
		{
			name:      "auth proxy route targets proxy listener",
			owner:     testNemoNetworkPolicyOwner(true, boolPointer(true)),
			wantPort:  8443,
			wantRules: 1,
		},
		{
			name:      "route disabled denies all ingress",
			owner:     testNemoNetworkPolicyOwner(false, boolPointer(false)),
			wantRules: 0,
		},
		{
			name:      "auth proxy route disabled denies all ingress",
			owner:     testNemoNetworkPolicyOwner(true, boolPointer(false)),
			wantRules: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy, err := buildNemoNetworkPolicy(tt.owner)
			if err != nil {
				t.Fatalf("buildNemoNetworkPolicy() error = %v", err)
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
			if !reflect.DeepEqual(policy.Spec.PolicyTypes, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}) {
				t.Errorf("policyTypes = %v, want [Ingress]", policy.Spec.PolicyTypes)
			}
			if len(policy.Spec.Egress) != 0 {
				t.Errorf("egress rules = %#v, want none", policy.Spec.Egress)
			}
			if len(policy.Spec.Ingress) != tt.wantRules {
				t.Fatalf("ingress rules = %#v, want %d", policy.Spec.Ingress, tt.wantRules)
			}
			if tt.wantRules == 1 {
				assertNemoRouterRule(t, policy.Spec.Ingress, tt.wantPort)
			}
			if len(policy.Spec.PodSelector.MatchLabels) == 0 && len(policy.Spec.PodSelector.MatchExpressions) == 0 {
				t.Error("policy has an empty pod selector")
			}
			if policy.Labels[utils.NetworkPolicyOwnerUIDLabel] != string(tt.owner.UID) {
				t.Errorf("owner UID label = %q, want %q", policy.Labels[utils.NetworkPolicyOwnerUIDLabel], tt.owner.UID)
			}
		})
	}
}

func TestBuildNemoNetworkPolicyRejectsMissingIdentity(t *testing.T) {
	if _, err := buildNemoNetworkPolicy(nil); err == nil {
		t.Fatal("buildNemoNetworkPolicy(nil) succeeded, want error")
	}
	owner := testNemoNetworkPolicyOwner(false, nil)
	owner.UID = ""
	if _, err := buildNemoNetworkPolicy(owner); err == nil {
		t.Fatal("buildNemoNetworkPolicy() without owner UID succeeded, want error")
	}
}

func TestReconcileNemoNetworkPolicySetsOwnershipRepairsDriftAndRecreatesDeletion(t *testing.T) {
	scheme := runtime.NewScheme()
	for _, addToScheme := range []func(*runtime.Scheme) error{
		corev1.AddToScheme,
		networkingv1.AddToScheme,
		nemoguardrailsv1alpha1.AddToScheme,
	} {
		if err := addToScheme(scheme); err != nil {
			t.Fatalf("register scheme type: %v", err)
		}
	}

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := &NemoGuardrailsReconciler{Client: c, Scheme: scheme}
	owner := testNemoNetworkPolicyOwner(true, boolPointer(true))
	ctx := context.Background()

	if err := reconciler.reconcileNetworkPolicy(ctx, owner); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}
	name, err := utils.NetworkPolicyName("nemo-guardrails-" + owner.Name)
	if err != nil {
		t.Fatalf("NetworkPolicyName(): %v", err)
	}
	key := client.ObjectKey{Name: name, Namespace: owner.Namespace}
	actual := &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, key, actual); err != nil {
		t.Fatalf("get NetworkPolicy: %v", err)
	}
	if len(actual.OwnerReferences) != 1 || actual.OwnerReferences[0].UID != owner.UID || actual.OwnerReferences[0].Controller == nil || !*actual.OwnerReferences[0].Controller {
		t.Fatalf("owner references = %#v, want controller reference to NemoGuardrails UID %q", actual.OwnerReferences, owner.UID)
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
	if len(actual.Spec.Ingress) != 1 {
		t.Errorf("repaired ingress rules = %d, want 1", len(actual.Spec.Ingress))
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

func assertNemoRouterRule(t *testing.T, rules []networkingv1.NetworkPolicyIngressRule, port int32) {
	t.Helper()
	if len(rules) != 1 {
		t.Fatalf("got %d ingress rules, want exactly one", len(rules))
	}
	rule := rules[0]
	if len(rule.From) != 1 || rule.From[0].NamespaceSelector == nil || rule.From[0].PodSelector != nil || rule.From[0].IPBlock != nil {
		t.Fatalf("ingress peers = %#v, want only the OpenShift ingress namespace selector", rule.From)
	}
	if !reflect.DeepEqual(rule.From[0].NamespaceSelector.MatchLabels, map[string]string{
		nemoNetworkPolicyNamespaceGroupLabel: nemoNetworkPolicyIngressGroup,
	}) {
		t.Errorf("router namespace selector = %#v", rule.From[0].NamespaceSelector.MatchLabels)
	}
	if len(rule.Ports) != 1 || rule.Ports[0].Port == nil || rule.Ports[0].Port.IntVal != port || rule.Ports[0].Protocol == nil || *rule.Ports[0].Protocol != corev1.ProtocolTCP {
		t.Errorf("ingress ports = %#v, want TCP %d", rule.Ports, port)
	}
	if port == 8000 || port == 8443 {
		// The health endpoint is never an application ingress path.
		for _, networkPort := range rule.Ports {
			if networkPort.Port != nil && networkPort.Port.IntVal == 9444 {
				t.Error("policy unexpectedly allows the kube-rbac-proxy health port")
			}
		}
	}
}

func testNemoNetworkPolicyOwner(auth bool, exposeRoute *bool) *nemoguardrailsv1alpha1.NemoGuardrails {
	owner := &nemoguardrailsv1alpha1.NemoGuardrails{ObjectMeta: metav1.ObjectMeta{
		Name:      "nemo-example",
		Namespace: "trustyai-namespace",
		UID:       types.UID("nemo-owner-uid"),
	}}
	owner.Spec.ExposeRoute = exposeRoute
	if auth {
		owner.Annotations = map[string]string{constants.AuthAnnotationKey: "true"}
	}
	return owner
}

func boolPointer(value bool) *bool {
	return &value
}

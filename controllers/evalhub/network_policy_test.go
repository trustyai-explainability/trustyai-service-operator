package evalhub

import (
	"context"
	"testing"

	evalhubv1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestBuildEvalHubNetworkPolicy(t *testing.T) {
	instance := testEvalHubNetworkPolicyOwner()
	policy, err := buildEvalHubNetworkPolicy(instance)
	if err != nil {
		t.Fatalf("buildEvalHubNetworkPolicy() error = %v", err)
	}
	if err := utils.ValidateNetworkPolicy(policy); err != nil {
		t.Fatalf("ValidateNetworkPolicy() error = %v", err)
	}

	wantSelector := map[string]string{
		"app":       "eval-hub",
		"instance":  instance.Name,
		"component": "api",
	}
	if !equalStringMap(policy.Spec.PodSelector.MatchLabels, wantSelector) {
		t.Errorf("pod selector = %#v, want %#v", policy.Spec.PodSelector.MatchLabels, wantSelector)
	}
	if len(policy.Spec.PolicyTypes) != 1 || policy.Spec.PolicyTypes[0] != networkingv1.PolicyTypeIngress {
		t.Fatalf("policyTypes = %v, want [Ingress]", policy.Spec.PolicyTypes)
	}
	if len(policy.Spec.Egress) != 0 {
		t.Fatalf("egress rules = %#v, want none", policy.Spec.Egress)
	}
	if len(policy.Spec.Ingress) != 3 {
		t.Fatalf("got %d ingress rules, want 3 with MCP disabled", len(policy.Spec.Ingress))
	}

	assertEvalHubNetworkPolicyRule(t, policy.Spec.Ingress[0], servicePort, func(peer networkingv1.NetworkPolicyPeer) bool {
		return peer.NamespaceSelector != nil &&
			equalStringMap(peer.NamespaceSelector.MatchLabels, map[string]string{
				evalHubNetworkPolicyNamespaceGroupLabel: evalHubNetworkPolicyIngressGroup,
			}) && peer.PodSelector == nil
	})
	jobRule := policy.Spec.Ingress[1]
	if len(jobRule.From) != 2 || len(jobRule.Ports) != 1 || jobRule.Ports[0].Port == nil ||
		jobRule.Ports[0].Port.IntVal != servicePort || jobRule.Ports[0].Protocol == nil || *jobRule.Ports[0].Protocol != corev1.ProtocolTCP {
		t.Fatalf("evaluation-job ingress rule = %#v, want both same-namespace and tenant peers on TCP %d", jobRule, servicePort)
	}
	wantJobLabels := evalHubEvaluationJobPodLabels(instance)
	if jobRule.From[0].PodSelector == nil || !equalStringMap(jobRule.From[0].PodSelector.MatchLabels, wantJobLabels) || jobRule.From[0].NamespaceSelector != nil {
		t.Errorf("same-namespace evaluation-job peer = %#v, want pod labels %#v", jobRule.From[0], wantJobLabels)
	}
	tenantPeer := jobRule.From[1]
	if tenantPeer.NamespaceSelector == nil || tenantPeer.PodSelector == nil || !equalStringMap(tenantPeer.PodSelector.MatchLabels, wantJobLabels) || len(tenantPeer.NamespaceSelector.MatchExpressions) != 1 ||
		tenantPeer.NamespaceSelector.MatchExpressions[0].Key != tenantLabel || tenantPeer.NamespaceSelector.MatchExpressions[0].Operator != metav1.LabelSelectorOpExists {
		t.Errorf("tenant evaluation-job peer = %#v, want tenant namespace and instance-specific pod selectors", tenantPeer)
	}
	assertEvalHubNetworkPolicyRule(t, policy.Spec.Ingress[2], metricsPort, func(peer networkingv1.NetworkPolicyPeer) bool {
		return peer.NamespaceSelector != nil &&
			equalStringMap(peer.NamespaceSelector.MatchLabels, map[string]string{
				evalHubNetworkPolicyNamespaceGroupLabel: evalHubNetworkPolicyMonitoringGroup,
			}) && peer.PodSelector != nil &&
			equalStringMap(peer.PodSelector.MatchLabels, map[string]string{
				evalHubPrometheusNameLabel:      evalHubPrometheusNameValue,
				evalHubPrometheusComponentLabel: evalHubPrometheusComponentValue,
			})
	})

	if policy.Labels[utils.NetworkPolicyManagedByLabel] != utils.NetworkPolicyManagedByValue {
		t.Errorf("managed-by label = %q, want %q", policy.Labels[utils.NetworkPolicyManagedByLabel], utils.NetworkPolicyManagedByValue)
	}
	if policy.Labels[utils.NetworkPolicyOwnerUIDLabel] != string(instance.UID) {
		t.Errorf("owner UID label = %q, want %q", policy.Labels[utils.NetworkPolicyOwnerUIDLabel], instance.UID)
	}
}

func TestBuildEvalHubNetworkPolicyAllowsMCPOnlyWhenEnabled(t *testing.T) {
	instance := testEvalHubNetworkPolicyOwner()
	enabled := true
	instance.Spec.MCP = &evalhubv1.EvalHubMCPSpec{Enabled: &enabled}

	policy, err := buildEvalHubNetworkPolicy(instance)
	if err != nil {
		t.Fatalf("buildEvalHubNetworkPolicy() error = %v", err)
	}
	if len(policy.Spec.Ingress) != 4 {
		t.Fatalf("got %d ingress rules with MCP enabled, want 4", len(policy.Spec.Ingress))
	}
	mcpRule := policy.Spec.Ingress[3]
	if len(mcpRule.From) != 1 || mcpRule.From[0].PodSelector == nil ||
		!equalStringMap(mcpRule.From[0].PodSelector.MatchLabels, mcpLabels(instance)) || len(mcpRule.Ports) != 1 ||
		mcpRule.Ports[0].Port == nil || mcpRule.Ports[0].Port.IntVal != servicePort {
		t.Errorf("MCP ingress rule = %#v, want same-instance MCP pods to TCP %d", mcpRule, servicePort)
	}
}

func TestBuildEvalHubNetworkPolicyRejectsMissingIdentity(t *testing.T) {
	if _, err := buildEvalHubNetworkPolicy(nil); err == nil {
		t.Fatal("buildEvalHubNetworkPolicy(nil) succeeded, want error")
	}
	instance := testEvalHubNetworkPolicyOwner()
	instance.UID = ""
	if _, err := buildEvalHubNetworkPolicy(instance); err == nil {
		t.Fatal("buildEvalHubNetworkPolicy() without owner UID succeeded, want error")
	}
}

func TestReconcileEvalHubNetworkPolicySetsOwnershipAndRepairsDrift(t *testing.T) {
	scheme := runtime.NewScheme()
	for _, addToScheme := range []func(*runtime.Scheme) error{
		corev1.AddToScheme,
		networkingv1.AddToScheme,
		evalhubv1.AddToScheme,
	} {
		if err := addToScheme(scheme); err != nil {
			t.Fatalf("register test types: %v", err)
		}
	}
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := &EvalHubReconciler{Client: c, Scheme: scheme}
	instance := testEvalHubNetworkPolicyOwner()
	ctx := context.Background()

	if err := reconciler.reconcileNetworkPolicy(ctx, instance); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}

	name, err := evalHubNetworkPolicyName(instance)
	if err != nil {
		t.Fatalf("NetworkPolicyName() error = %v", err)
	}
	tasPolicyName, err := utils.NetworkPolicyName(instance.Name)
	if err != nil {
		t.Fatalf("TAS NetworkPolicyName() error = %v", err)
	}
	if name == tasPolicyName {
		t.Fatalf("EvalHub policy name %q collides with same-named TAS policy", name)
	}
	key := client.ObjectKey{Name: name, Namespace: instance.Namespace}
	actual := &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, key, actual); err != nil {
		t.Fatalf("get NetworkPolicy: %v", err)
	}
	if len(actual.OwnerReferences) != 1 || actual.OwnerReferences[0].UID != instance.UID ||
		actual.OwnerReferences[0].Controller == nil || !*actual.OwnerReferences[0].Controller {
		t.Fatalf("owner references = %#v, want controller reference to EvalHub UID %q", actual.OwnerReferences, instance.UID)
	}

	actual.Spec.Ingress = nil
	actual.Labels = map[string]string{"drift": "true"}
	if err := c.Update(ctx, actual); err != nil {
		t.Fatalf("simulate policy drift: %v", err)
	}
	if err := reconciler.reconcileNetworkPolicy(ctx, instance); err != nil {
		t.Fatalf("reconcile drifted NetworkPolicy: %v", err)
	}
	actual = &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, key, actual); err != nil {
		t.Fatalf("get repaired NetworkPolicy: %v", err)
	}
	if len(actual.Spec.Ingress) != 3 {
		t.Errorf("repaired ingress rules = %d, want 3", len(actual.Spec.Ingress))
	}
	if _, exists := actual.Labels["drift"]; exists {
		t.Errorf("unmanaged drift label was not repaired: %#v", actual.Labels)
	}

	if err := c.Delete(ctx, actual); err != nil {
		t.Fatalf("delete NetworkPolicy: %v", err)
	}
	if err := reconciler.reconcileNetworkPolicy(ctx, instance); err != nil {
		t.Fatalf("recreate deleted NetworkPolicy: %v", err)
	}
	actual = &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, key, actual); err != nil {
		t.Fatalf("get recreated NetworkPolicy: %v", err)
	}
}

func testEvalHubNetworkPolicyOwner() *evalhubv1.EvalHub {
	return &evalhubv1.EvalHub{ObjectMeta: metav1.ObjectMeta{
		Name:      "evalhub-example",
		Namespace: "evalhub-namespace",
		UID:       types.UID("evalhub-owner-uid"),
	}}
}

func assertEvalHubNetworkPolicyRule(t *testing.T, rule networkingv1.NetworkPolicyIngressRule, wantPort int32, wantPeer func(networkingv1.NetworkPolicyPeer) bool) {
	t.Helper()
	if len(rule.From) != 1 || !wantPeer(rule.From[0]) {
		t.Errorf("ingress peers = %#v, want expected restricted peer", rule.From)
	}
	if len(rule.Ports) != 1 || rule.Ports[0].Port == nil || rule.Ports[0].Port.IntVal != wantPort ||
		rule.Ports[0].Protocol == nil || *rule.Ports[0].Protocol != corev1.ProtocolTCP {
		t.Errorf("ingress ports = %#v, want TCP %d", rule.Ports, wantPort)
	}
}

func equalStringMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range b {
		if a[key] != value {
			return false
		}
	}
	return true
}

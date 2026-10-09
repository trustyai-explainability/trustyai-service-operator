package evalhub

import (
	"context"
	"encoding/json"
	"testing"

	evalhubv1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const evalHubNetworkPolicyIngressIntentAnnotation = "trustyai.opendatahub.io/network-policy-ingress-namespace-only-peers"

func TestBuildEvalHubNetworkPolicy(t *testing.T) {
	instance := testEvalHubNetworkPolicyOwner()
	desired, err := buildEvalHubNetworkPolicy(instance)
	if err != nil {
		t.Fatalf("buildEvalHubNetworkPolicy() error = %v", err)
	}
	identity, err := evalHubNetworkPolicyIdentity(instance)
	if err != nil {
		t.Fatal(err)
	}
	if err := utils.ValidateWorkloadNetworkPolicyWithIngressIntent(desired.Policy, identity, nil, desired.Ingress); err != nil {
		t.Fatalf("ValidateWorkloadNetworkPolicyWithIngressIntent() error = %v", err)
	}

	wantTarget, err := identity.Labels()
	if err != nil {
		t.Fatal(err)
	}
	if !equalNetworkPolicyLabels(desired.Policy.Spec.PodSelector.MatchLabels, wantTarget) {
		t.Errorf("target selector = %#v, want identity labels %#v", desired.Policy.Spec.PodSelector.MatchLabels, wantTarget)
	}
	if len(desired.Policy.Spec.PolicyTypes) != 1 || desired.Policy.Spec.PolicyTypes[0] != networkingv1.PolicyTypeIngress {
		t.Fatalf("policyTypes = %v, want only Ingress", desired.Policy.Spec.PolicyTypes)
	}
	if len(desired.Policy.Spec.Egress) != 0 {
		t.Fatalf("egress rules = %#v, want none", desired.Policy.Spec.Egress)
	}
	if len(desired.Policy.Spec.Ingress) != 3 {
		t.Fatalf("got %d ingress rules with MCP disabled, want router, evaluation-job, and metrics rules", len(desired.Policy.Spec.Ingress))
	}

	router := desired.Policy.Spec.Ingress[0]
	if len(router.From) != 1 || router.From[0].NamespaceSelector == nil || router.From[0].PodSelector != nil ||
		!equalNetworkPolicyLabels(router.From[0].NamespaceSelector.MatchLabels, map[string]string{
			evalHubNetworkPolicyNamespaceGroupLabel: evalHubNetworkPolicyIngressGroup,
		}) {
		t.Errorf("Route peer = %#v, want the reviewed ingress namespace-only selector", router.From)
	}
	assertEvalHubNetworkPolicyPort(t, router, servicePort)

	jobRule := desired.Policy.Spec.Ingress[1]
	wantJobLabels := evalHubEvaluationJobPodLabels(instance)
	if len(jobRule.From) != 2 {
		t.Fatalf("evaluation-job peers = %#v, want same-namespace and tenant peers", jobRule.From)
	}
	if jobRule.From[0].NamespaceSelector != nil || jobRule.From[0].PodSelector == nil ||
		!equalNetworkPolicyLabels(jobRule.From[0].PodSelector.MatchLabels, wantJobLabels) {
		t.Errorf("same-namespace evaluation-job peer = %#v, want pod labels %#v", jobRule.From[0], wantJobLabels)
	}
	tenantPeer := jobRule.From[1]
	if tenantPeer.NamespaceSelector == nil || tenantPeer.PodSelector == nil ||
		!equalNetworkPolicyLabels(tenantPeer.PodSelector.MatchLabels, wantJobLabels) ||
		len(tenantPeer.NamespaceSelector.MatchExpressions) != 1 ||
		tenantPeer.NamespaceSelector.MatchExpressions[0].Key != tenantLabel ||
		tenantPeer.NamespaceSelector.MatchExpressions[0].Operator != metav1.LabelSelectorOpExists {
		t.Errorf("tenant evaluation-job peer = %#v, want tenant namespace and instance-specific pod selectors", tenantPeer)
	}
	assertEvalHubNetworkPolicyPort(t, jobRule, servicePort)

	metrics := desired.Policy.Spec.Ingress[2]
	if len(metrics.From) != 1 || metrics.From[0].NamespaceSelector == nil || metrics.From[0].PodSelector == nil ||
		!equalNetworkPolicyLabels(metrics.From[0].NamespaceSelector.MatchLabels, map[string]string{
			evalHubNetworkPolicyNamespaceGroupLabel: evalHubNetworkPolicyMonitoringGroup,
		}) || !equalNetworkPolicyLabels(metrics.From[0].PodSelector.MatchLabels, map[string]string{
		evalHubPrometheusNameLabel:      evalHubPrometheusNameValue,
		evalHubPrometheusComponentLabel: evalHubPrometheusComponentValue,
	}) {
		t.Errorf("metrics peer = %#v, want Prometheus Pods in monitoring namespaces", metrics.From)
	}
	assertEvalHubNetworkPolicyPort(t, metrics, metricsPort)

	if desired.Ingress == nil || len(desired.Ingress.NamespaceOnlyPeers) != 1 ||
		!equalNetworkPolicyLabels(desired.Ingress.NamespaceOnlyPeers[0].NamespaceSelector.MatchLabels, map[string]string{
			evalHubNetworkPolicyNamespaceGroupLabel: evalHubNetworkPolicyIngressGroup,
		}) || desired.Ingress.NamespaceOnlyPeers[0].Rationale == "" || desired.Ingress.NamespaceOnlyPeers[0].ResidualRisk == "" {
		t.Fatalf("typed router intent = %#v, want exactly one reviewed namespace-only peer", desired.Ingress)
	}
}

func TestBuildEvalHubNetworkPolicyAllowsMCPOnlyWhenEnabled(t *testing.T) {
	instance := testEvalHubNetworkPolicyOwner()
	enabled := true
	instance.Spec.MCP = &evalhubv1.EvalHubMCPSpec{Enabled: &enabled}

	desired, err := buildEvalHubNetworkPolicy(instance)
	if err != nil {
		t.Fatalf("buildEvalHubNetworkPolicy() error = %v", err)
	}
	if len(desired.Policy.Spec.Ingress) != 4 {
		t.Fatalf("got %d ingress rules with MCP enabled, want 4", len(desired.Policy.Spec.Ingress))
	}
	mcpIdentity, err := evalHubMCPNetworkPolicyIdentity(instance)
	if err != nil {
		t.Fatal(err)
	}
	wantMCPLabels, err := mcpIdentity.Labels()
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range mcpLabels(instance) {
		wantMCPLabels[key] = value
	}
	mcpRule := desired.Policy.Spec.Ingress[3]
	if len(mcpRule.From) != 1 || mcpRule.From[0].NamespaceSelector != nil || mcpRule.From[0].PodSelector == nil ||
		!equalNetworkPolicyLabels(mcpRule.From[0].PodSelector.MatchLabels, wantMCPLabels) {
		t.Errorf("MCP peer = %#v, want same-instance MCP Pod identity %#v", mcpRule.From, wantMCPLabels)
	}
	assertEvalHubNetworkPolicyPort(t, mcpRule, servicePort)
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

func TestReconcileEvalHubNetworkPolicyRepairsDriftAndDeletion(t *testing.T) {
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
	owner := testEvalHubNetworkPolicyOwner()
	owner.GetObjectKind().SetGroupVersionKind(evalhubv1.GroupVersion.WithKind("EvalHub"))
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner).Build()
	reconciler := &EvalHubReconciler{Client: c, Scheme: scheme}
	ctx := context.Background()

	if err := reconciler.reconcileNetworkPolicy(ctx, owner); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}
	identity, err := evalHubNetworkPolicyIdentity(owner)
	if err != nil {
		t.Fatal(err)
	}
	name, err := identity.Name("api-ingress")
	if err != nil {
		t.Fatal(err)
	}
	key := client.ObjectKey{Namespace: owner.Namespace, Name: name}
	actual := &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, key, actual); err != nil {
		t.Fatalf("get NetworkPolicy: %v", err)
	}
	if len(actual.OwnerReferences) != 1 || actual.OwnerReferences[0].UID != owner.UID ||
		actual.OwnerReferences[0].Controller == nil || !*actual.OwnerReferences[0].Controller {
		t.Fatalf("owner references = %#v, want controller reference to EvalHub UID %q", actual.OwnerReferences, owner.UID)
	}
	var recordedIntent []utils.NetworkPolicyNamespaceOnlyPeerIntent
	if err := json.Unmarshal([]byte(actual.Annotations[evalHubNetworkPolicyIngressIntentAnnotation]), &recordedIntent); err != nil {
		t.Fatalf("typed ingress intent annotation is missing or invalid: %v", err)
	}
	if len(recordedIntent) != 1 || recordedIntent[0].Rationale == "" || recordedIntent[0].ResidualRisk == "" {
		t.Fatalf("recorded ingress intent = %#v, want the reviewed router exception", recordedIntent)
	}

	actual.Spec.Ingress = nil
	actual.Labels["test.example/unmanaged"] = "preserve"
	actual.Annotations["test.example/unmanaged"] = "preserve"
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
	if len(actual.Spec.Ingress) != 3 {
		t.Errorf("repaired ingress rule count = %d, want 3", len(actual.Spec.Ingress))
	}
	if actual.Labels["test.example/unmanaged"] != "preserve" || actual.Annotations["test.example/unmanaged"] != "preserve" {
		t.Errorf("unrelated metadata was not preserved: labels=%#v annotations=%#v", actual.Labels, actual.Annotations)
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

// evalHubReconcileOrderClient records resource writes relevant to the API
// NetworkPolicy-before-Deployment invariant.
type evalHubReconcileOrderClient struct {
	client.Client
	writes []string
}

func (c *evalHubReconcileOrderClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	switch obj.(type) {
	case *networkingv1.NetworkPolicy:
		c.writes = append(c.writes, "networkpolicy")
	case *appsv1.Deployment:
		c.writes = append(c.writes, "deployment")
	}
	return c.Client.Create(ctx, obj, opts...)
}

func testEvalHubNetworkPolicyOwner() *evalhubv1.EvalHub {
	return &evalhubv1.EvalHub{
		TypeMeta: metav1.TypeMeta{APIVersion: evalhubv1.GroupVersion.String(), Kind: "EvalHub"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "evalhub-example",
			Namespace: "evalhub-namespace",
			UID:       types.UID("evalhub-owner-uid"),
		},
	}
}

func assertEvalHubNetworkPolicyPort(t *testing.T, rule networkingv1.NetworkPolicyIngressRule, wantPort int32) {
	t.Helper()
	if len(rule.Ports) != 1 || rule.Ports[0].Port == nil || rule.Ports[0].Port.Type != intstr.Int ||
		rule.Ports[0].Port.IntVal != wantPort || rule.Ports[0].Protocol == nil || *rule.Ports[0].Protocol != corev1.ProtocolTCP {
		t.Errorf("ingress ports = %#v, want TCP %d", rule.Ports, wantPort)
	}
}

func equalNetworkPolicyLabels(a, b map[string]string) bool {
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

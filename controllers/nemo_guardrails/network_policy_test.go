package nemo_guardrails

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
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

const (
	testNemoCRNamespace       = "nemo-namespace"
	testNemoInfraNamespace    = "odh-ai-gateway-infra"
	testNemoControllerNS      = "opendatahub"
	testNemoPolicyCallerNS    = nemoMaaSCallerNamespace
	testNemoServerPort        = 8000
	testNemoAuthProxyListener = 8443
)

func TestBuildNemoGuardrailsNetworkPolicy(t *testing.T) {
	instance := testNemoNetworkPolicyOwner()
	desired, err := buildNemoGuardrailsNetworkPolicy(instance, testNemoInfraNamespace)
	if err != nil {
		t.Fatalf("buildNemoGuardrailsNetworkPolicy() error = %v", err)
	}
	identity, err := nemoNetworkPolicyIdentity(instance)
	if err != nil {
		t.Fatal(err)
	}
	if err := utils.ValidateWorkloadNetworkPolicyWithIngressIntent(desired.Policy, identity, desired.Egress, desired.Ingress); err != nil {
		t.Fatalf("ValidateWorkloadNetworkPolicyWithIngressIntent() error = %v", err)
	}
	if desired.Policy.Namespace != testNemoInfraNamespace {
		t.Fatalf("policy namespace = %q, want the workload namespace %q", desired.Policy.Namespace, testNemoInfraNamespace)
	}

	wantLabels, err := identity.Labels()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(desired.Policy.Spec.PodSelector.MatchLabels, wantLabels) {
		t.Fatalf("target labels = %#v, want exactly the reserved identity labels %#v", desired.Policy.Spec.PodSelector.MatchLabels, wantLabels)
	}
	if len(desired.Policy.Spec.PolicyTypes) != 2 || desired.Policy.Spec.PolicyTypes[0] != networkingv1.PolicyTypeIngress ||
		desired.Policy.Spec.PolicyTypes[1] != networkingv1.PolicyTypeEgress {
		t.Fatalf("policyTypes = %v, want [Ingress Egress]", desired.Policy.Spec.PolicyTypes)
	}
	if len(desired.Policy.Spec.Egress) != 1 || len(desired.Policy.Spec.Egress[0].To) != 0 || len(desired.Policy.Spec.Egress[0].Ports) != 0 {
		t.Fatalf("egress rules = %#v, want exactly one empty AllowAll rule", desired.Policy.Spec.Egress)
	}
	if desired.Egress == nil || desired.Egress.Mode != utils.NetworkPolicyAllowAll ||
		desired.Egress.Rationale == "" || desired.Egress.ResidualRisk == "" {
		t.Fatalf("egress intent = %#v, want documented AllowAll with rationale and residual risk", desired.Egress)
	}
	if len(desired.Policy.Spec.Ingress) != 4 {
		t.Fatalf("got %d ingress rules, want payload processing (namespace and same namespace), gateway and router", len(desired.Policy.Spec.Ingress))
	}

	payloadProcessing := desired.Policy.Spec.Ingress[0]
	if len(payloadProcessing.From) != 1 || payloadProcessing.From[0].NamespaceSelector == nil || payloadProcessing.From[0].PodSelector == nil {
		t.Fatalf("payload processing peer = %#v, want a namespace AND Pod peer", payloadProcessing.From)
	}
	if payloadProcessing.From[0].NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] != testNemoPolicyCallerNS {
		t.Errorf("payload processing namespace peer = %#v, want %q", payloadProcessing.From[0].NamespaceSelector, testNemoPolicyCallerNS)
	}
	assertNemoCallerPodSelector(t, payloadProcessing.From[0].PodSelector)
	assertNemoNetworkPolicyPorts(t, payloadProcessing, testNemoServerPort)

	sameNamespace := desired.Policy.Spec.Ingress[1]
	if len(sameNamespace.From) != 1 || sameNamespace.From[0].NamespaceSelector != nil {
		t.Fatalf("same-namespace peer = %#v, want a Pod-only peer", sameNamespace.From)
	}
	assertNemoCallerPodSelector(t, sameNamespace.From[0].PodSelector)

	gateway := desired.Policy.Spec.Ingress[2]
	if len(gateway.From) != 1 || gateway.From[0].PodSelector == nil ||
		gateway.From[0].PodSelector.MatchLabels[nemoGatewayManagedLabel] != nemoGatewayManagedValue {
		t.Fatalf("gateway peer = %#v, want the Istio-managed gateway Pod selector", gateway.From)
	}
	assertNemoNetworkPolicyPorts(t, gateway, testNemoServerPort)

	router := desired.Policy.Spec.Ingress[3]
	if len(router.From) != 1 || router.From[0].NamespaceSelector == nil || router.From[0].PodSelector != nil ||
		router.From[0].NamespaceSelector.MatchLabels[nemoNetworkPolicyNamespaceGroupLabel] != nemoNetworkPolicyIngressGroup {
		t.Errorf("router peer = %#v, want the ingress namespace-only selector", router.From)
	}
	assertNemoNetworkPolicyPorts(t, router, testNemoServerPort)
	if desired.Ingress == nil || len(desired.Ingress.NamespaceOnlyPeers) != 1 ||
		desired.Ingress.NamespaceOnlyPeers[0].Rationale == "" || desired.Ingress.NamespaceOnlyPeers[0].ResidualRisk == "" {
		t.Fatalf("router intent = %#v, want explicit namespace-only rationale and residual risk", desired.Ingress)
	}
}

func TestBuildNemoGuardrailsNetworkPolicyAuthProxyListener(t *testing.T) {
	instance := testNemoNetworkPolicyOwner()
	instance.Annotations = map[string]string{constants.AuthAnnotationKey: "true"}
	desired, err := buildNemoGuardrailsNetworkPolicy(instance, testNemoInfraNamespace)
	if err != nil {
		t.Fatalf("buildNemoGuardrailsNetworkPolicy() error = %v", err)
	}
	for index, rule := range desired.Policy.Spec.Ingress {
		assertNemoNetworkPolicyPorts(t, rule, testNemoAuthProxyListener)
		_ = index
	}
	// The admin listener is never granted.
	for _, rule := range desired.Policy.Spec.Ingress {
		for _, port := range rule.Ports {
			if port.Port != nil && port.Port.IntVal == 8444 {
				t.Fatalf("ingress rule %#v grants the authenticated admin listener", rule)
			}
		}
	}
}

func TestBuildNemoGuardrailsNetworkPolicyWithoutRouteExposure(t *testing.T) {
	instance := testNemoNetworkPolicyOwner()
	exposeRoute := false
	instance.Spec.ExposeRoute = &exposeRoute
	desired, err := buildNemoGuardrailsNetworkPolicy(instance, testNemoInfraNamespace)
	if err != nil {
		t.Fatalf("buildNemoGuardrailsNetworkPolicy(exposeRoute=false) error = %v", err)
	}
	identity, err := nemoNetworkPolicyIdentity(instance)
	if err != nil {
		t.Fatal(err)
	}
	if err := utils.ValidateWorkloadNetworkPolicyWithIngressIntent(desired.Policy, identity, desired.Egress, desired.Ingress); err != nil {
		t.Fatalf("ValidateWorkloadNetworkPolicyWithIngressIntent() error = %v", err)
	}
	// Route exposure disabled keeps the Ingress direction (the listener stays
	// isolated) while removing the router allow rule and its intent entirely.
	if len(desired.Policy.Spec.PolicyTypes) != 2 || desired.Policy.Spec.PolicyTypes[0] != networkingv1.PolicyTypeIngress ||
		desired.Policy.Spec.PolicyTypes[1] != networkingv1.PolicyTypeEgress {
		t.Fatalf("policyTypes = %v, want [Ingress Egress] even with Route exposure disabled", desired.Policy.Spec.PolicyTypes)
	}
	if len(desired.Policy.Spec.Ingress) != 3 {
		t.Fatalf("ingress rules = %#v, want payload processing and gateway only", desired.Policy.Spec.Ingress)
	}
	if desired.Ingress != nil {
		t.Fatalf("ingress intent = %#v, want none with Route exposure disabled", desired.Ingress)
	}
}

func TestBuildNemoGuardrailsNetworkPolicyAdditionalIngress(t *testing.T) {
	instance := testNemoNetworkPolicyOwner()
	exposeRoute := false
	instance.Spec.ExposeRoute = &exposeRoute
	instance.Spec.NetworkPolicy = &nemoguardrailsv1alpha1.NemoNetworkPolicy{AdditionalIngress: []nemoguardrailsv1alpha1.NemoIngressPeer{{
		NamespaceSelector: metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "custom-gateway"}},
		PodSelector:       metav1.LabelSelector{MatchLabels: map[string]string{"app": "client"}},
	}}}
	desired, err := buildNemoGuardrailsNetworkPolicy(instance, testNemoInfraNamespace)
	if err != nil {
		t.Fatal(err)
	}
	if len(desired.Policy.Spec.Ingress) != 4 {
		t.Fatalf("ingress rules = %d, want payload processing, gateway and the configured caller", len(desired.Policy.Spec.Ingress))
	}
	custom := desired.Policy.Spec.Ingress[3]
	if len(custom.From) != 1 || custom.From[0].PodSelector == nil || custom.From[0].NamespaceSelector == nil {
		t.Fatal("namespace and Pod selectors must be ANDed in one peer")
	}
	assertNemoNetworkPolicyPorts(t, custom, testNemoServerPort)

	// An exclusion-only or empty selector must fail closed instead of granting
	// the whole cluster.
	instance.Spec.NetworkPolicy.AdditionalIngress[0].PodSelector = metav1.LabelSelector{}
	if _, err := buildNemoGuardrailsNetworkPolicy(instance, testNemoInfraNamespace); err == nil {
		t.Fatal("empty Pod selector must fail closed")
	}
	instance.Spec.NetworkPolicy.AdditionalIngress[0].PodSelector = metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
		Key: "app", Operator: metav1.LabelSelectorOpNotIn, Values: []string{"blocked"},
	}}}
	if _, err := buildNemoGuardrailsNetworkPolicy(instance, testNemoInfraNamespace); err == nil {
		t.Fatal("exclusion-only Pod selector must fail closed")
	}
}

func TestBuildNemoGuardrailsNetworkPolicyRequiresPersistedIdentity(t *testing.T) {
	if _, err := buildNemoGuardrailsNetworkPolicy(nil, testNemoInfraNamespace); err == nil {
		t.Fatal("buildNemoGuardrailsNetworkPolicy(nil) succeeded")
	}
	instance := testNemoNetworkPolicyOwner()
	instance.UID = ""
	if _, err := buildNemoGuardrailsNetworkPolicy(instance, testNemoInfraNamespace); err == nil {
		t.Fatal("buildNemoGuardrailsNetworkPolicy without UID succeeded")
	}
	if _, err := buildNemoGuardrailsNetworkPolicy(testNemoNetworkPolicyOwner(), ""); err == nil {
		t.Fatal("buildNemoGuardrailsNetworkPolicy without a workload namespace succeeded")
	}
}

// TestReconcileNemoNetworkPolicyWorkloadNamespace covers the merged workload
// placement: the custom resource lives in a project namespace while the
// Deployment is placed in the shared AI Gateway namespace, so the policy cannot
// carry a cross-namespace controller reference.
func TestReconcileNemoNetworkPolicyWorkloadNamespace(t *testing.T) {
	ctx := context.Background()
	scheme, owner := nemoNetworkPolicyFixture(t)
	reconciler := &NemoGuardrailsReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).Build(), Scheme: scheme, Namespace: testNemoControllerNS}

	if err := reconciler.reconcileNetworkPolicy(ctx, owner, testNemoInfraNamespace); err != nil {
		t.Fatalf("reconcileNetworkPolicy() error = %v", err)
	}
	desired, err := buildNemoGuardrailsNetworkPolicy(owner, testNemoInfraNamespace)
	if err != nil {
		t.Fatal(err)
	}
	actual := &networkingv1.NetworkPolicy{}
	if err := reconciler.Get(ctx, client.ObjectKeyFromObject(desired.Policy), actual); err != nil {
		t.Fatalf("get NetworkPolicy: %v", err)
	}
	if len(actual.OwnerReferences) != 0 {
		t.Fatalf("owner references = %#v, want none: a namespaced owner reference cannot cross namespaces", actual.OwnerReferences)
	}
	if actual.Labels[utils.OwnerUIDLabel] != string(owner.UID) {
		t.Fatalf("owner UID label = %q, want %q", actual.Labels[utils.OwnerUIDLabel], owner.UID)
	}
	if actual.Annotations[utils.OwnerNameAnnotation] != owner.Name || actual.Annotations[utils.OwnerNamespaceAnnotation] != owner.Namespace {
		t.Fatalf("owner annotations = %#v, want the owning custom resource", actual.Annotations)
	}
	if actual.Labels[nemoNetworkPolicyManagerLabel] != nemoNetworkPolicyManager {
		t.Fatalf("managed-by label = %q, want %q", actual.Labels[nemoNetworkPolicyManagerLabel], nemoNetworkPolicyManager)
	}
	if !hasNemoNetworkPolicyIdentity(actual) {
		t.Fatal("policy is not visible to the explicit watch predicate")
	}
	requests := reconciler.enqueueNemoFromNetworkPolicy(ctx, actual)
	if len(requests) != 1 || requests[0].Name != owner.Name || requests[0].Namespace != owner.Namespace {
		t.Fatalf("watch mapping = %#v, want the owning NemoGuardrails", requests)
	}
}

func TestReconcileNemoNetworkPolicySameNamespaceOwnsByControllerReference(t *testing.T) {
	ctx := context.Background()
	scheme, owner := nemoNetworkPolicyFixture(t)
	reconciler := &NemoGuardrailsReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).Build(), Scheme: scheme, Namespace: "operator-ns"}

	if err := reconciler.reconcileNetworkPolicy(ctx, owner, owner.Namespace); err != nil {
		t.Fatalf("reconcileNetworkPolicy() error = %v", err)
	}
	desired, err := buildNemoGuardrailsNetworkPolicy(owner, owner.Namespace)
	if err != nil {
		t.Fatal(err)
	}
	actual := &networkingv1.NetworkPolicy{}
	if err := reconciler.Get(ctx, client.ObjectKeyFromObject(desired.Policy), actual); err != nil {
		t.Fatalf("get NetworkPolicy: %v", err)
	}
	if !utils.IsOwnedBy(actual, owner) {
		t.Fatalf("policy is not owned by the custom resource: %#v", actual.OwnerReferences)
	}
}

func TestReconcileNemoNetworkPolicyRepairsDriftAndDeletion(t *testing.T) {
	ctx := context.Background()
	scheme, owner := nemoNetworkPolicyFixture(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := &NemoGuardrailsReconciler{Client: c, Scheme: scheme, Namespace: testNemoControllerNS}

	if err := reconciler.reconcileNetworkPolicy(ctx, owner, testNemoInfraNamespace); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}
	desired, err := buildNemoGuardrailsNetworkPolicy(owner, testNemoInfraNamespace)
	if err != nil {
		t.Fatal(err)
	}
	actual := &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(desired.Policy), actual); err != nil {
		t.Fatalf("get NetworkPolicy: %v", err)
	}

	actual.Spec.Ingress = nil
	if err := c.Update(ctx, actual); err != nil {
		t.Fatalf("simulate NetworkPolicy drift: %v", err)
	}
	if err := reconciler.reconcileNetworkPolicy(ctx, owner, testNemoInfraNamespace); err != nil {
		t.Fatalf("repair NetworkPolicy drift: %v", err)
	}
	actual = &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(desired.Policy), actual); err != nil {
		t.Fatalf("get repaired NetworkPolicy: %v", err)
	}
	if len(actual.Spec.Ingress) != 4 {
		t.Fatalf("repaired ingress rules = %d, want 4", len(actual.Spec.Ingress))
	}

	if err := c.Delete(ctx, actual); err != nil {
		t.Fatalf("delete NetworkPolicy: %v", err)
	}
	if err := reconciler.reconcileNetworkPolicy(ctx, owner, testNemoInfraNamespace); err != nil {
		t.Fatalf("recreate deleted NetworkPolicy: %v", err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(desired.Policy), actual); err != nil {
		t.Fatalf("get recreated NetworkPolicy: %v", err)
	}
}

func TestReconcileNemoNetworkPolicyRejectsUnauthorizedNamespace(t *testing.T) {
	ctx := context.Background()
	scheme, owner := nemoNetworkPolicyFixture(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := &NemoGuardrailsReconciler{Client: c, Scheme: scheme, Namespace: testNemoControllerNS}

	if err := reconciler.reconcileNetworkPolicy(ctx, owner, "another-namespace"); err == nil {
		t.Fatal("reconcileNetworkPolicy accepted a namespace outside its authority")
	}
	policies := &networkingv1.NetworkPolicyList{}
	if err := c.List(ctx, policies); err != nil {
		t.Fatal(err)
	}
	if len(policies.Items) != 0 {
		t.Fatalf("created policy outside the authorized namespace: %#v", policies.Items)
	}
}

func TestReconcileNemoNetworkPolicyRefusesForeignPolicy(t *testing.T) {
	ctx := context.Background()
	scheme, owner := nemoNetworkPolicyFixture(t)
	desired, err := buildNemoGuardrailsNetworkPolicy(owner, testNemoInfraNamespace)
	if err != nil {
		t.Fatal(err)
	}
	foreign := desired.Policy.DeepCopy()
	foreign.Labels = map[string]string{nemoNetworkPolicyManagerLabel: "another-controller"}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(foreign).Build()
	reconciler := &NemoGuardrailsReconciler{Client: c, Scheme: scheme, Namespace: testNemoControllerNS}

	if err := reconciler.reconcileNetworkPolicy(ctx, owner, testNemoInfraNamespace); err == nil {
		t.Fatal("reconcileNetworkPolicy adopted a policy managed by another controller")
	}
	actual := &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(foreign), actual); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual.Labels, foreign.Labels) {
		t.Fatalf("foreign policy labels = %#v, want the untouched %#v", actual.Labels, foreign.Labels)
	}
}

func TestReconcileNemoNetworkPolicyRemovesRouterPeerWhenRouteExposureDisabled(t *testing.T) {
	ctx := context.Background()
	scheme, owner := nemoNetworkPolicyFixture(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := &NemoGuardrailsReconciler{Client: c, Scheme: scheme, Namespace: testNemoControllerNS}

	if err := reconciler.reconcileNetworkPolicy(ctx, owner, testNemoInfraNamespace); err != nil {
		t.Fatalf("initial reconcile with Route exposure enabled: %v", err)
	}
	desired, err := buildNemoGuardrailsNetworkPolicy(owner, testNemoInfraNamespace)
	if err != nil {
		t.Fatal(err)
	}
	policyKey := client.ObjectKeyFromObject(desired.Policy)
	exposed := &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, policyKey, exposed); err != nil {
		t.Fatalf("get NetworkPolicy after exposed reconcile: %v", err)
	}

	exposeRoute := false
	owner.Spec.ExposeRoute = &exposeRoute
	if err := reconciler.reconcileNetworkPolicy(ctx, owner, testNemoInfraNamespace); err != nil {
		t.Fatalf("reconcile after disabling Route exposure: %v", err)
	}
	isolated := &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, policyKey, isolated); err != nil {
		t.Fatalf("get NetworkPolicy after isolated reconcile: %v", err)
	}
	if isolated.UID != exposed.UID {
		t.Fatal("policy object was replaced instead of updated in place")
	}
	if len(isolated.Spec.Ingress) != 3 {
		t.Fatalf("ingress rules = %#v, want the router rule removed after disabling Route exposure", isolated.Spec.Ingress)
	}
	if len(isolated.Spec.PolicyTypes) != 2 || isolated.Spec.PolicyTypes[0] != networkingv1.PolicyTypeIngress ||
		isolated.Spec.PolicyTypes[1] != networkingv1.PolicyTypeEgress {
		t.Fatalf("policyTypes = %v, want [Ingress Egress] preserved", isolated.Spec.PolicyTypes)
	}
}

func TestStampNemoNetworkPolicyIdentity(t *testing.T) {
	scheme, owner := nemoNetworkPolicyFixture(t)
	reconciler := &NemoGuardrailsReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).Build(), Scheme: scheme, Namespace: testNemoControllerNS}
	deployment := nemoNetworkPolicyDeploymentFixture(owner, testNemoInfraNamespace)
	if err := utils.SetOwnerReference(owner, deployment, scheme); err != nil {
		t.Fatal(err)
	}
	identity, err := nemoNetworkPolicyIdentity(owner)
	if err != nil {
		t.Fatal(err)
	}
	selectorBefore := deployment.Spec.Selector.DeepCopy()
	if err := stampNemoNetworkPolicyIdentity(deployment, owner, identity); err != nil {
		t.Fatalf("stampNemoNetworkPolicyIdentity() error = %v", err)
	}
	wantLabels, err := identity.Labels()
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range wantLabels {
		if deployment.Spec.Template.Labels[key] != value {
			t.Errorf("Pod template label %q = %q, want %q", key, deployment.Spec.Template.Labels[key], value)
		}
	}
	if !reflect.DeepEqual(deployment.Spec.Selector, selectorBefore) {
		t.Fatalf("Deployment selector = %#v, want the immutable original %#v", deployment.Spec.Selector, selectorBefore)
	}

	foreign := nemoNetworkPolicyDeploymentFixture(owner, testNemoInfraNamespace)
	foreign.Labels[utils.OwnerUIDLabel] = "another-owner"
	if err := stampNemoNetworkPolicyIdentity(foreign, owner, identity); err == nil {
		t.Fatal("stampNemoNetworkPolicyIdentity labelled an unowned Deployment")
	}
	_ = reconciler
}

// TestDeleteOwnedInNamespaceRemovesNetworkPolicy covers operand cleanup for the
// shared namespace placement, where the policy has no owner reference and
// Kubernetes therefore cannot garbage collect it.
func TestDeleteOwnedInNamespaceRemovesNetworkPolicy(t *testing.T) {
	ctx := context.Background()
	scheme, owner := nemoNetworkPolicyFixture(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := &NemoGuardrailsReconciler{Client: c, Scheme: scheme, Namespace: testNemoControllerNS}
	if err := reconciler.reconcileNetworkPolicy(ctx, owner, testNemoInfraNamespace); err != nil {
		t.Fatalf("reconcileNetworkPolicy() error = %v", err)
	}
	desired, err := buildNemoGuardrailsNetworkPolicy(owner, testNemoInfraNamespace)
	if err != nil {
		t.Fatal(err)
	}
	if err := reconciler.deleteOwnedInNamespace(ctx, owner, testNemoInfraNamespace); err != nil {
		t.Fatalf("deleteOwnedInNamespace() error = %v", err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(desired.Policy), &networkingv1.NetworkPolicy{}); err == nil {
		t.Fatal("operand cleanup left the NetworkPolicy behind")
	}
}

// TestReportNetworkPolicyStatus covers the ADR requirement that a policy failure
// is visible on the component resource and clears only after the policy converges.
func TestReportNetworkPolicyStatus(t *testing.T) {
	ctx := context.Background()
	scheme, owner := nemoNetworkPolicyFixture(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner).WithStatusSubresource(owner).Build()
	reconciler := &NemoGuardrailsReconciler{Client: c, Scheme: scheme, Namespace: testNemoControllerNS}

	reconciler.reportNetworkPolicyFailure(ctx, owner, fmt.Errorf("policy write failed"))
	saved := &nemoguardrailsv1alpha1.NemoGuardrails{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(owner), saved); err != nil {
		t.Fatal(err)
	}
	condition := utils.GetStatusCondition(saved.Status.Conditions, nemoNetworkPolicyCondition)
	if condition == nil || condition.Status != corev1.ConditionFalse || condition.Reason != nemoNetworkPolicyFailedReason ||
		condition.Message != "policy write failed" {
		t.Fatalf("NetworkPolicy condition = %#v, want the reported failure", condition)
	}
	if complete := utils.GetStatusCondition(saved.Status.Conditions, utils.ConditionReconcileComplete); complete != nil && complete.Status == corev1.ConditionTrue {
		t.Fatalf("Complete condition = %#v, want not ready", complete)
	}
	if saved.Status.Phase != utils.PhaseError {
		t.Fatalf("phase = %q, want %q", saved.Status.Phase, utils.PhaseError)
	}

	reconciler.reportNetworkPolicyReady(ctx, saved)
	if err := c.Get(ctx, client.ObjectKeyFromObject(owner), saved); err != nil {
		t.Fatal(err)
	}
	if condition := utils.GetStatusCondition(saved.Status.Conditions, nemoNetworkPolicyCondition); condition == nil ||
		condition.Status != corev1.ConditionTrue || condition.Reason != nemoNetworkPolicyReconciledReason {
		t.Fatalf("NetworkPolicy condition = %#v, want the reconciled state", condition)
	}
}

// The intent annotations are the object-level record of a reviewed exception, so
// they are pinned by their published keys rather than through utils internals.
const (
	testNemoEgressModeAnnotation      = "trustyai.opendatahub.io/network-policy-egress-mode"
	testNemoEgressRationaleAnnotation = "trustyai.opendatahub.io/network-policy-egress-rationale"
	testNemoEgressRiskAnnotation      = "trustyai.opendatahub.io/network-policy-egress-residual-risk"
	testNemoIngressIntentAnnotation   = "trustyai.opendatahub.io/network-policy-ingress-namespace-only-peers"
)

func TestReconcileNemoNetworkPolicyRecordsIntentAnnotations(t *testing.T) {
	ctx := context.Background()
	scheme, owner := nemoNetworkPolicyFixture(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := &NemoGuardrailsReconciler{Client: c, Scheme: scheme, Namespace: testNemoControllerNS}

	if err := reconciler.reconcileNetworkPolicy(ctx, owner, testNemoInfraNamespace); err != nil {
		t.Fatalf("reconcileNetworkPolicy() error = %v", err)
	}
	desired, err := buildNemoGuardrailsNetworkPolicy(owner, testNemoInfraNamespace)
	if err != nil {
		t.Fatal(err)
	}
	key := client.ObjectKeyFromObject(desired.Policy)
	actual := &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, key, actual); err != nil {
		t.Fatal(err)
	}
	annotations := actual.GetAnnotations()
	if annotations[testNemoEgressModeAnnotation] != string(utils.NetworkPolicyAllowAll) {
		t.Fatalf("egress mode annotation = %q, want %q", annotations[testNemoEgressModeAnnotation], utils.NetworkPolicyAllowAll)
	}
	if annotations[testNemoEgressRationaleAnnotation] == "" || annotations[testNemoEgressRiskAnnotation] == "" {
		t.Fatalf("egress rationale or residual risk not recorded: %#v", annotations)
	}
	if annotations[testNemoIngressIntentAnnotation] == "" {
		t.Fatalf("namespace-only ingress peer intent not recorded: %#v", annotations)
	}

	// A later hand edit to the recorded rationale is repaired like any other drift.
	actual.Annotations[testNemoEgressRiskAnnotation] = "unreviewed"
	if err := c.Update(ctx, actual); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.reconcileNetworkPolicy(ctx, owner, testNemoInfraNamespace); err != nil {
		t.Fatalf("reconcileNetworkPolicy() after tampering error = %v", err)
	}
	repaired := &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, key, repaired); err != nil {
		t.Fatal(err)
	}
	if repaired.Annotations[testNemoEgressRiskAnnotation] == "unreviewed" || repaired.Annotations[testNemoEgressRiskAnnotation] == "" {
		t.Fatalf("residual risk annotation = %q, want the reviewed value restored", repaired.Annotations[testNemoEgressRiskAnnotation])
	}
}

func TestHasNemoNetworkPolicyIdentity(t *testing.T) {
	policy := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
		utils.NetworkPolicyComponentLabel: nemoNetworkPolicyComponent,
		nemoNetworkPolicyManagerLabel:     nemoNetworkPolicyManager,
	}}}
	if !hasNemoNetworkPolicyIdentity(policy) {
		t.Fatal("managed NemoGuardrails policy was not recognized")
	}
	other := policy.DeepCopy()
	other.Labels[utils.NetworkPolicyComponentLabel] = "tas"
	if hasNemoNetworkPolicyIdentity(other) {
		t.Fatal("another component's policy was mapped to NemoGuardrails")
	}
	unmanaged := policy.DeepCopy()
	delete(unmanaged.Labels, nemoNetworkPolicyManagerLabel)
	if hasNemoNetworkPolicyIdentity(unmanaged) {
		t.Fatal("an unmanaged policy was mapped to NemoGuardrails")
	}
	if requests := (&NemoGuardrailsReconciler{}).enqueueNemoFromNetworkPolicy(context.Background(), unmanaged); len(requests) != 0 {
		t.Fatalf("unmanaged policy produced requests %#v", requests)
	}
}

func assertNemoCallerPodSelector(t *testing.T, selector *metav1.LabelSelector) {
	t.Helper()
	if selector == nil || len(selector.MatchExpressions) != 1 {
		t.Fatalf("caller pod selector = %#v, want one app expression", selector)
	}
	expression := selector.MatchExpressions[0]
	if expression.Key != nemoCallerAppLabel || expression.Operator != metav1.LabelSelectorOpIn {
		t.Fatalf("caller pod selector expression = %#v, want app In (...)", expression)
	}
	for _, required := range []string{nemoPayloadProcessingApp, nemoPayloadPreProcessingApp} {
		found := false
		for _, value := range expression.Values {
			if value == required {
				found = true
			}
		}
		if !found {
			t.Fatalf("caller pod selector values = %v, want %q", expression.Values, required)
		}
	}
}

func assertNemoNetworkPolicyPorts(t *testing.T, rule networkingv1.NetworkPolicyIngressRule, want ...int32) {
	t.Helper()
	if len(rule.Ports) != len(want) {
		t.Fatalf("ports = %#v, want %v", rule.Ports, want)
	}
	actual := map[int32]bool{}
	for _, port := range rule.Ports {
		if port.Port == nil || port.Protocol == nil || *port.Protocol != corev1.ProtocolTCP {
			t.Fatalf("port = %#v, want explicit TCP port", port)
		}
		if port.Port.Type != intstr.Int {
			t.Fatalf("port = %#v, want numeric port", port)
		}
		actual[port.Port.IntVal] = true
	}
	for _, port := range want {
		if !actual[port] {
			t.Errorf("port set %#v does not contain TCP %d", actual, port)
		}
	}
}

func nemoNetworkPolicyFixture(t *testing.T) (*runtime.Scheme, *nemoguardrailsv1alpha1.NemoGuardrails) {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, addToScheme := range []func(*runtime.Scheme) error{
		corev1.AddToScheme,
		appsv1.AddToScheme,
		routev1.AddToScheme,
		networkingv1.AddToScheme,
		nemoguardrailsv1alpha1.AddToScheme,
	} {
		if err := addToScheme(scheme); err != nil {
			t.Fatalf("register test API types: %v", err)
		}
	}
	owner := testNemoNetworkPolicyOwner()
	owner.GetObjectKind().SetGroupVersionKind(nemoguardrailsv1alpha1.GroupVersion.WithKind("NemoGuardrails"))
	return scheme, owner
}

func testNemoNetworkPolicyOwner() *nemoguardrailsv1alpha1.NemoGuardrails {
	return &nemoguardrailsv1alpha1.NemoGuardrails{ObjectMeta: metav1.ObjectMeta{
		Name:      "nemo-example",
		Namespace: testNemoCRNamespace,
		UID:       types.UID("nemo-owner-uid"),
	}}
}

func nemoNetworkPolicyDeploymentFixture(owner *nemoguardrailsv1alpha1.NemoGuardrails, namespace string) *appsv1.Deployment {
	selector := owner.Name
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      selector,
			Namespace: namespace,
			Labels:    map[string]string{"app": selector},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": selector}},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
				"app":                        selector,
				"component":                  selector,
				"app.kubernetes.io/part-of":  "trustyai",
				"app.kubernetes.io/name":     selector,
				"app.kubernetes.io/instance": selector,
			}}},
		},
	}
}

package nemo_guardrails

import (
	"context"
	"reflect"
	"testing"

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

func TestBuildNemoGuardrailsNetworkPolicy(t *testing.T) {
	instance := testNemoNetworkPolicyOwner()
	desired, err := buildNemoGuardrailsNetworkPolicy(instance)
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
	if len(desired.Policy.Spec.Ingress) != 2 {
		t.Fatalf("got %d ingress rules, want gateway and router", len(desired.Policy.Spec.Ingress))
	}

	router := desired.Policy.Spec.Ingress[1]
	if len(router.From) != 1 || router.From[0].NamespaceSelector == nil || router.From[0].PodSelector != nil ||
		router.From[0].NamespaceSelector.MatchLabels[nemoNetworkPolicyNamespaceGroupLabel] != nemoNetworkPolicyIngressGroup {
		t.Errorf("router peer = %#v, want the ingress namespace-only selector", router.From)
	}
	assertNemoNetworkPolicyPorts(t, router, nemoServerPort)
	if desired.Ingress == nil || len(desired.Ingress.NamespaceOnlyPeers) != 1 ||
		desired.Ingress.NamespaceOnlyPeers[0].Rationale == "" || desired.Ingress.NamespaceOnlyPeers[0].ResidualRisk == "" {
		t.Fatalf("router intent = %#v, want explicit namespace-only rationale and residual risk", desired.Ingress)
	}
}

func TestBuildNemoGuardrailsNetworkPolicyAuthProxyListener(t *testing.T) {
	instance := testNemoNetworkPolicyOwner()
	instance.Annotations = map[string]string{constants.AuthAnnotationKey: "true"}
	desired, err := buildNemoGuardrailsNetworkPolicy(instance)
	if err != nil {
		t.Fatalf("buildNemoGuardrailsNetworkPolicy() error = %v", err)
	}
	if len(desired.Policy.Spec.Ingress) != 2 {
		t.Fatalf("got %d ingress rules, want gateway and router", len(desired.Policy.Spec.Ingress))
	}
	assertNemoNetworkPolicyPorts(t, desired.Policy.Spec.Ingress[0], nemoAuthProxyPort)
}

func TestBuildNemoGuardrailsNetworkPolicyRequiresPersistedIdentity(t *testing.T) {
	if _, err := buildNemoGuardrailsNetworkPolicy(nil); err == nil {
		t.Fatal("buildNemoGuardrailsNetworkPolicy(nil) succeeded")
	}
	instance := testNemoNetworkPolicyOwner()
	instance.UID = ""
	if _, err := buildNemoGuardrailsNetworkPolicy(instance); err == nil {
		t.Fatal("buildNemoGuardrailsNetworkPolicy without UID succeeded")
	}
}

func TestReconcileNemoNetworkPolicyRepairsDriftAndDeletion(t *testing.T) {
	ctx := context.Background()
	scheme, owner := nemoNetworkPolicyFixture(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner).Build()
	reconciler := &NemoGuardrailsReconciler{Client: c, Scheme: scheme}

	if err := reconciler.reconcileNetworkPolicy(ctx, owner); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}
	desired, err := buildNemoGuardrailsNetworkPolicy(owner)
	if err != nil {
		t.Fatal(err)
	}
	actual := &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(desired.Policy), actual); err != nil {
		t.Fatalf("get NetworkPolicy: %v", err)
	}
	if len(actual.OwnerReferences) != 1 || actual.OwnerReferences[0].UID != owner.UID ||
		actual.OwnerReferences[0].Controller == nil || !*actual.OwnerReferences[0].Controller {
		t.Fatalf("owner references = %#v, want NemoGuardrails controller reference", actual.OwnerReferences)
	}
	if actual.Labels[utils.NetworkPolicyOwnerUIDLabel] == "" {
		t.Fatal("NetworkPolicy is missing its owner UID label")
	}

	actual.Spec.Ingress = nil
	if err := c.Update(ctx, actual); err != nil {
		t.Fatalf("simulate NetworkPolicy drift: %v", err)
	}
	if err := reconciler.reconcileNetworkPolicy(ctx, owner); err != nil {
		t.Fatalf("repair NetworkPolicy drift: %v", err)
	}
	actual = &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(desired.Policy), actual); err != nil {
		t.Fatalf("get repaired NetworkPolicy: %v", err)
	}
	if len(actual.Spec.Ingress) != 2 {
		t.Fatalf("repaired ingress rules = %d, want 1", len(actual.Spec.Ingress))
	}

	if err := c.Delete(ctx, actual); err != nil {
		t.Fatalf("delete NetworkPolicy: %v", err)
	}
	if err := reconciler.reconcileNetworkPolicy(ctx, owner); err != nil {
		t.Fatalf("recreate deleted NetworkPolicy: %v", err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(desired.Policy), actual); err != nil {
		t.Fatalf("get recreated NetworkPolicy: %v", err)
	}
}

func TestReconcileNemoNetworkPolicyLabelsOnlyVerifiedOwnedPods(t *testing.T) {
	ctx := context.Background()
	scheme, owner := nemoNetworkPolicyFixture(t)
	deployment, replicaSet, pod := nemoNetworkPolicyWorkloadFixture(owner)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner, deployment, replicaSet, pod).Build()
	reconciler := &NemoGuardrailsReconciler{Client: c, Scheme: scheme}

	if err := reconciler.reconcileNetworkPolicy(ctx, owner); err != nil {
		t.Fatalf("reconcile policy and existing Pod identity: %v", err)
	}
	identity, err := nemoNetworkPolicyIdentity(owner)
	if err != nil {
		t.Fatal(err)
	}
	wantIdentityLabels, err := identity.Labels()
	if err != nil {
		t.Fatal(err)
	}
	actualPod := &corev1.Pod{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(pod), actualPod); err != nil {
		t.Fatalf("get migrated Pod: %v", err)
	}
	for key, value := range wantIdentityLabels {
		if actualPod.Labels[key] != value {
			t.Errorf("Pod label %q = %q, want %q", key, actualPod.Labels[key], value)
		}
	}

	// A reserved-label drift event can map back to the NemoGuardrails instance
	// and the controller repairs the label only after the owner chain is
	// reverified.
	delete(actualPod.Labels, utils.NetworkPolicyOwnerUIDLabel)
	if err := c.Update(ctx, actualPod); err != nil {
		t.Fatalf("simulate Pod identity drift: %v", err)
	}
	requests := reconciler.enqueueNemoFromPod(ctx, actualPod)
	if len(requests) != 1 || requests[0].Name != owner.Name || requests[0].Namespace != owner.Namespace {
		t.Fatalf("Pod map requests = %#v, want the owning NemoGuardrails", requests)
	}
	if !nemoPodLabelsChanged(pod, actualPod) {
		t.Fatal("Pod label drift was not recognized")
	}
	if err := reconciler.reconcileNetworkPolicy(ctx, owner); err != nil {
		t.Fatalf("repair Pod identity drift: %v", err)
	}
	actualPod = &corev1.Pod{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(pod), actualPod); err != nil {
		t.Fatalf("get repaired Pod: %v", err)
	}
	if actualPod.Labels[utils.NetworkPolicyOwnerUIDLabel] != wantIdentityLabels[utils.NetworkPolicyOwnerUIDLabel] {
		t.Fatal("owner UID identity drift was not repaired")
	}
}

func TestNemoPolicyConvergesBeforeExistingPodMutation(t *testing.T) {
	ctx := context.Background()
	scheme, owner := nemoNetworkPolicyFixture(t)
	deployment, replicaSet, pod := nemoNetworkPolicyWorkloadFixture(owner)
	baseClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner, deployment, replicaSet, pod).Build()
	c := &recordingNemoClient{Client: baseClient}
	reconciler := &NemoGuardrailsReconciler{Client: c, Scheme: scheme}

	if err := reconciler.reconcileNetworkPolicy(ctx, owner); err != nil {
		t.Fatalf("reconcile NetworkPolicy and current Pods: %v", err)
	}

	indices := map[string]int{}
	for index, write := range c.writes {
		if _, exists := indices[write]; !exists {
			indices[write] = index
		}
	}
	if indices["networkpolicy-create"] >= indices["pod-update"] {
		t.Fatalf("writes = %v, want NetworkPolicy before Pod labels", c.writes)
	}
}

type recordingNemoClient struct {
	client.Client
	writes []string
}

func (c *recordingNemoClient) Create(ctx context.Context, object client.Object, options ...client.CreateOption) error {
	if _, ok := object.(*networkingv1.NetworkPolicy); ok {
		c.writes = append(c.writes, "networkpolicy-create")
	}
	return c.Client.Create(ctx, object, options...)
}

func (c *recordingNemoClient) Update(ctx context.Context, object client.Object, options ...client.UpdateOption) error {
	switch object.(type) {
	case *corev1.Pod:
		c.writes = append(c.writes, "pod-update")
	case *appsv1.Deployment:
		c.writes = append(c.writes, "deployment-update")
	}
	return c.Client.Update(ctx, object, options...)
}

func TestReconcileNemoNetworkPolicyRefusesForeignDeployment(t *testing.T) {
	ctx := context.Background()
	scheme, owner := nemoNetworkPolicyFixture(t)
	deployment, _, _ := nemoNetworkPolicyWorkloadFixture(owner)
	deployment.OwnerReferences[0].UID = types.UID("different-owner")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner, deployment).Build()
	reconciler := &NemoGuardrailsReconciler{Client: c, Scheme: scheme}
	if err := reconciler.reconcileNetworkPolicy(ctx, owner); err == nil {
		t.Fatal("foreign Deployment was accepted")
	}
	policies := &networkingv1.NetworkPolicyList{}
	if err := c.List(ctx, policies, client.InNamespace(owner.Namespace)); err != nil {
		t.Fatal(err)
	}
	if len(policies.Items) != 0 {
		t.Fatalf("created policy despite Deployment ownership conflict: %#v", policies.Items)
	}
}

func TestReconcileNemoNetworkPolicySkipsPodFromForeignReplicaSet(t *testing.T) {
	ctx := context.Background()
	scheme, owner := nemoNetworkPolicyFixture(t)
	deployment, replicaSet, pod := nemoNetworkPolicyWorkloadFixture(owner)
	replicaSet.OwnerReferences[0].UID = types.UID("foreign-deployment-uid")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner, deployment, replicaSet, pod).Build()
	reconciler := &NemoGuardrailsReconciler{Client: c, Scheme: scheme}
	if err := reconciler.reconcileNetworkPolicy(ctx, owner); err != nil {
		t.Fatalf("foreign ReplicaSet candidate blocked reconciliation: %v", err)
	}
	actualPod := &corev1.Pod{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(pod), actualPod); err != nil {
		t.Fatal(err)
	}
	if _, found := actualPod.Labels[utils.NetworkPolicyOwnerUIDLabel]; found {
		t.Fatal("foreign-owned Pod received reserved NemoGuardrails identity labels")
	}
	// The owned workload keeps reconciling: the policy exists even though the
	// foreign candidate was skipped.
	policies := &networkingv1.NetworkPolicyList{}
	if err := c.List(ctx, policies, client.InNamespace(owner.Namespace)); err != nil {
		t.Fatal(err)
	}
	if len(policies.Items) != 1 {
		t.Fatalf("policy was not reconciled alongside the foreign Pod candidate: %#v", policies.Items)
	}
}

func TestBuildNemoGuardrailsNetworkPolicyWithoutRouteExposure(t *testing.T) {
	instance := testNemoNetworkPolicyOwner()
	exposeRoute := false
	instance.Spec.ExposeRoute = &exposeRoute
	desired, err := buildNemoGuardrailsNetworkPolicy(instance)
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
	if len(desired.Policy.Spec.Ingress) != 1 {
		t.Fatalf("ingress rules = %#v, want none with Route exposure disabled", desired.Policy.Spec.Ingress)
	}
	if desired.Ingress != nil {
		t.Fatalf("ingress intent = %#v, want none with Route exposure disabled", desired.Ingress)
	}
	if len(desired.Policy.Spec.Egress) != 1 || len(desired.Policy.Spec.Egress[0].To) != 0 || len(desired.Policy.Spec.Egress[0].Ports) != 0 {
		t.Fatalf("egress rules = %#v, want exactly one empty AllowAll rule", desired.Policy.Spec.Egress)
	}
}

func TestReconcileNemoNetworkPolicyRemovesRouterPeerWhenRouteExposureDisabled(t *testing.T) {
	ctx := context.Background()
	scheme, owner := nemoNetworkPolicyFixture(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner).Build()
	reconciler := &NemoGuardrailsReconciler{Client: c, Scheme: scheme}

	if err := reconciler.reconcileNetworkPolicy(ctx, owner); err != nil {
		t.Fatalf("initial reconcile with Route exposure enabled: %v", err)
	}
	desired, err := buildNemoGuardrailsNetworkPolicy(owner)
	if err != nil {
		t.Fatal(err)
	}
	policyKey := client.ObjectKeyFromObject(desired.Policy)
	exposed := &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, policyKey, exposed); err != nil {
		t.Fatalf("get NetworkPolicy after exposed reconcile: %v", err)
	}
	if len(exposed.Spec.Ingress) != 2 {
		t.Fatalf("ingress rules = %d, want the router rule while Route exposure is enabled", len(exposed.Spec.Ingress))
	}

	// Flipping exposeRoute to false must converge the same owned policy object
	// to the ingress-isolated shape instead of leaving the stale router grant.
	exposeRoute := false
	owner.Spec.ExposeRoute = &exposeRoute
	if err := reconciler.reconcileNetworkPolicy(ctx, owner); err != nil {
		t.Fatalf("reconcile after disabling Route exposure: %v", err)
	}
	isolated := &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, policyKey, isolated); err != nil {
		t.Fatalf("get NetworkPolicy after isolated reconcile: %v", err)
	}
	if isolated.UID != exposed.UID {
		t.Fatal("policy object was replaced instead of updated in place")
	}
	if len(isolated.Spec.Ingress) != 1 {
		t.Fatalf("ingress rules = %#v, want the router rule removed after disabling Route exposure", isolated.Spec.Ingress)
	}
	if len(isolated.Spec.PolicyTypes) != 2 || isolated.Spec.PolicyTypes[0] != networkingv1.PolicyTypeIngress ||
		isolated.Spec.PolicyTypes[1] != networkingv1.PolicyTypeEgress {
		t.Fatalf("policyTypes = %v, want [Ingress Egress] preserved", isolated.Spec.PolicyTypes)
	}
}

func TestNemoPodLabelPredicateIgnoresStatusOnlyChanges(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
		"app":                        "nemo-test",
		"component":                  "nemo-test",
		"app.kubernetes.io/instance": "nemo-test",
		"app.kubernetes.io/name":     "nemo-test",
		"app.kubernetes.io/part-of":  "trustyai",
	}}}
	other := pod.DeepCopy()
	if nemoPodLabelsChanged(pod, other) {
		t.Fatal("identical Pod labels should not trigger a label-drift reconcile")
	}
	other.Labels[utils.NetworkPolicyOwnerUIDLabel] = "owner"
	if !nemoPodLabelsChanged(pod, other) {
		t.Fatal("NemoGuardrails identity-label change should trigger reconciliation")
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
		Namespace: "nemo-namespace",
		UID:       types.UID("nemo-owner-uid"),
	}}
}

func nemoNetworkPolicyWorkloadFixture(owner *nemoguardrailsv1alpha1.NemoGuardrails) (*appsv1.Deployment, *appsv1.ReplicaSet, *corev1.Pod) {
	selector := map[string]string{
		"app":                        owner.Name,
		"component":                  owner.Name,
		"deploy-name":                owner.Name,
		"app.kubernetes.io/instance": owner.Name,
		"app.kubernetes.io/name":     owner.Name,
		"app.kubernetes.io/part-of":  "trustyai",
	}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      owner.Name,
			Namespace: owner.Namespace,
			UID:       types.UID("nemo-deployment-uid"),
			OwnerReferences: []metav1.OwnerReference{{

				APIVersion: nemoguardrailsv1alpha1.GroupVersion.String(),
				Kind:       "NemoGuardrails",
				Name:       owner.Name,
				UID:        owner.UID,
				Controller: nemoBoolPointer(true),
			}},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": owner.Name}},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: selector}},
		},
	}
	replicaSet := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      owner.Name + "-abc123",
			Namespace: owner.Namespace,
			UID:       types.UID("nemo-replicaset-uid"),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       deployment.Name,
				UID:        deployment.UID,
				Controller: nemoBoolPointer(true),
			}},
		},
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name:      owner.Name + "-abc123-xyz",
		Namespace: owner.Namespace,
		Labels:    selector,
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: "apps/v1",
			Kind:       "ReplicaSet",
			Name:       replicaSet.Name,
			UID:        replicaSet.UID,
			Controller: nemoBoolPointer(true),
		}},
	}}
	return deployment, replicaSet, pod
}

func nemoBoolPointer(value bool) *bool { return &value }

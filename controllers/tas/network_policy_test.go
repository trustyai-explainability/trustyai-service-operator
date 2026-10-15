package tas

import (
	"context"
	"reflect"
	"testing"

	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
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

func TestBuildTASNetworkPolicy(t *testing.T) {
	instance := testTASNetworkPolicyOwner()
	desired, err := buildTASNetworkPolicy(instance)
	if err != nil {
		t.Fatalf("buildTASNetworkPolicy() error = %v", err)
	}
	identity, err := tasNetworkPolicyIdentity(instance)
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
	if len(desired.Policy.Spec.Ingress) != 3 {
		t.Fatalf("got %d ingress rules, want router, monitoring, and model-serving rules", len(desired.Policy.Spec.Ingress))
	}

	router := desired.Policy.Spec.Ingress[0]
	if len(router.From) != 1 || router.From[0].NamespaceSelector == nil || router.From[0].PodSelector != nil ||
		router.From[0].NamespaceSelector.MatchLabels[tasNetworkPolicyNamespaceGroupLabel] != tasNetworkPolicyIngressGroup {
		t.Errorf("router peer = %#v, want the ingress namespace-only selector", router.From)
	}
	assertTASNetworkPolicyPorts(t, router, 8443)
	if desired.Ingress == nil || len(desired.Ingress.NamespaceOnlyPeers) != 1 ||
		desired.Ingress.NamespaceOnlyPeers[0].Rationale == "" || desired.Ingress.NamespaceOnlyPeers[0].ResidualRisk == "" {
		t.Fatalf("router intent = %#v, want explicit namespace-only rationale and residual risk", desired.Ingress)
	}

	monitoring := desired.Policy.Spec.Ingress[1]
	if len(monitoring.From) != 1 || monitoring.From[0].NamespaceSelector == nil || monitoring.From[0].PodSelector == nil ||
		monitoring.From[0].NamespaceSelector.MatchLabels[tasNetworkPolicyNamespaceGroupLabel] != tasNetworkPolicyMonitoringGroup ||
		monitoring.From[0].PodSelector.MatchLabels[tasPrometheusNameLabel] != tasPrometheusNameValue ||
		monitoring.From[0].PodSelector.MatchLabels[tasPrometheusComponentLabel] != tasPrometheusComponentValue {
		t.Errorf("monitoring peer = %#v, want Prometheus Pods in monitoring namespaces", monitoring.From)
	}
	assertTASNetworkPolicyPorts(t, monitoring, 8443, 8080)

	modelServing := desired.Policy.Spec.Ingress[2]
	if len(modelServing.From) != 2 {
		t.Fatalf("model-serving peers = %#v, want ModelMesh and KServe", modelServing.From)
	}
	if modelServing.From[0].PodSelector == nil || modelServing.From[0].NamespaceSelector != nil ||
		modelServing.From[0].PodSelector.MatchLabels[tasModelMeshServiceLabel] != tasModelMeshServiceValue {
		t.Errorf("ModelMesh peer = %#v", modelServing.From[0])
	}
	if modelServing.From[1].PodSelector == nil || modelServing.From[1].NamespaceSelector != nil ||
		len(modelServing.From[1].PodSelector.MatchExpressions) != 1 ||
		modelServing.From[1].PodSelector.MatchExpressions[0].Key != tasKServeInferenceServiceLabel ||
		modelServing.From[1].PodSelector.MatchExpressions[0].Operator != metav1.LabelSelectorOpExists {
		t.Errorf("KServe peer = %#v", modelServing.From[1])
	}
	assertTASNetworkPolicyPorts(t, modelServing, 4443)
}

func TestBuildTASNetworkPolicyRequiresPersistedIdentity(t *testing.T) {
	if _, err := buildTASNetworkPolicy(nil); err == nil {
		t.Fatal("buildTASNetworkPolicy(nil) succeeded")
	}
	instance := testTASNetworkPolicyOwner()
	instance.UID = ""
	if _, err := buildTASNetworkPolicy(instance); err == nil {
		t.Fatal("buildTASNetworkPolicy without UID succeeded")
	}
}

func TestReconcileTASNetworkPolicyRepairsDriftAndDeletion(t *testing.T) {
	ctx := context.Background()
	scheme, owner := tasNetworkPolicyFixture(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner).Build()
	reconciler := &TrustyAIServiceReconciler{Client: c, Scheme: scheme}

	if err := reconciler.reconcileNetworkPolicy(ctx, owner); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}
	desired, err := buildTASNetworkPolicy(owner)
	if err != nil {
		t.Fatal(err)
	}
	actual := &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(desired.Policy), actual); err != nil {
		t.Fatalf("get NetworkPolicy: %v", err)
	}
	if len(actual.OwnerReferences) != 1 || actual.OwnerReferences[0].UID != owner.UID ||
		actual.OwnerReferences[0].Controller == nil || !*actual.OwnerReferences[0].Controller {
		t.Fatalf("owner references = %#v, want TrustyAIService controller reference", actual.OwnerReferences)
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
	if len(actual.Spec.Ingress) != 3 {
		t.Fatalf("repaired ingress rules = %d, want 3", len(actual.Spec.Ingress))
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

func TestReconcileTASNetworkPolicyLabelsOnlyVerifiedOwnedPods(t *testing.T) {
	ctx := context.Background()
	scheme, owner := tasNetworkPolicyFixture(t)
	deployment, replicaSet, pod := tasNetworkPolicyWorkloadFixture(owner)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner, deployment, replicaSet, pod).Build()
	reconciler := &TrustyAIServiceReconciler{Client: c, Scheme: scheme}

	if err := reconciler.reconcileNetworkPolicy(ctx, owner); err != nil {
		t.Fatalf("reconcile policy and existing Pod identity: %v", err)
	}
	identity, err := tasNetworkPolicyIdentity(owner)
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

	actualDeployment := &appsv1.Deployment{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(deployment), actualDeployment); err != nil {
		t.Fatalf("get Deployment: %v", err)
	}
	if err := reconciler.ensureTASDeploymentIdentity(ctx, owner, actualDeployment); err != nil {
		t.Fatalf("label Deployment template: %v", err)
	}
	actualDeployment = &appsv1.Deployment{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(deployment), actualDeployment); err != nil {
		t.Fatalf("get labeled Deployment: %v", err)
	}
	for key, value := range wantIdentityLabels {
		if actualDeployment.Spec.Template.Labels[key] != value {
			t.Errorf("Deployment template label %q = %q, want %q", key, actualDeployment.Spec.Template.Labels[key], value)
		}
	}
	if !reflect.DeepEqual(actualDeployment.Spec.Selector.MatchLabels, deployment.Spec.Selector.MatchLabels) {
		t.Fatal("NetworkPolicy identity labeling changed the immutable Deployment selector")
	}

	// A reserved-label drift event can map back to the TAS instance and the
	// controller repairs the label only after the owner chain is reverified.
	delete(actualPod.Labels, utils.NetworkPolicyOwnerUIDLabel)
	if err := c.Update(ctx, actualPod); err != nil {
		t.Fatalf("simulate Pod identity drift: %v", err)
	}
	requests := reconciler.enqueueTASFromPod(ctx, actualPod)
	if len(requests) != 1 || requests[0].Name != owner.Name || requests[0].Namespace != owner.Namespace {
		t.Fatalf("Pod map requests = %#v, want the owning TAS", requests)
	}
	if !tasPodLabelsChanged(pod, actualPod) {
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

func TestTASPolicyConvergesBeforeExistingPodAndDeploymentMutation(t *testing.T) {
	ctx := context.Background()
	scheme, owner := tasNetworkPolicyFixture(t)
	deployment, replicaSet, pod := tasNetworkPolicyWorkloadFixture(owner)
	baseClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner, deployment, replicaSet, pod).Build()
	c := &recordingTASClient{Client: baseClient}
	reconciler := &TrustyAIServiceReconciler{Client: c, Scheme: scheme}

	if err := reconciler.reconcileNetworkPolicy(ctx, owner); err != nil {
		t.Fatalf("reconcile NetworkPolicy and current Pods: %v", err)
	}
	actualDeployment := &appsv1.Deployment{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(deployment), actualDeployment); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.ensureTASDeploymentIdentity(ctx, owner, actualDeployment); err != nil {
		t.Fatalf("update Pod template identity: %v", err)
	}

	indices := map[string]int{}
	for index, write := range c.writes {
		if _, exists := indices[write]; !exists {
			indices[write] = index
		}
	}
	if indices["networkpolicy-create"] >= indices["pod-update"] || indices["pod-update"] >= indices["deployment-update"] {
		t.Fatalf("writes = %v, want NetworkPolicy before Pod labels before Deployment template", c.writes)
	}
}

type recordingTASClient struct {
	client.Client
	writes []string
}

func (c *recordingTASClient) Create(ctx context.Context, object client.Object, options ...client.CreateOption) error {
	if _, ok := object.(*networkingv1.NetworkPolicy); ok {
		c.writes = append(c.writes, "networkpolicy-create")
	}
	return c.Client.Create(ctx, object, options...)
}

func (c *recordingTASClient) Update(ctx context.Context, object client.Object, options ...client.UpdateOption) error {
	switch object.(type) {
	case *corev1.Pod:
		c.writes = append(c.writes, "pod-update")
	case *appsv1.Deployment:
		c.writes = append(c.writes, "deployment-update")
	}
	return c.Client.Update(ctx, object, options...)
}

func TestReconcileTASNetworkPolicyRefusesForeignDeployment(t *testing.T) {
	ctx := context.Background()
	scheme, owner := tasNetworkPolicyFixture(t)
	deployment, _, _ := tasNetworkPolicyWorkloadFixture(owner)
	deployment.OwnerReferences[0].UID = types.UID("different-owner")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner, deployment).Build()
	reconciler := &TrustyAIServiceReconciler{Client: c, Scheme: scheme}
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

func TestReconcileTASNetworkPolicyRefusesPodFromForeignReplicaSet(t *testing.T) {
	ctx := context.Background()
	scheme, owner := tasNetworkPolicyFixture(t)
	deployment, replicaSet, pod := tasNetworkPolicyWorkloadFixture(owner)
	replicaSet.OwnerReferences[0].UID = types.UID("foreign-deployment-uid")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner, deployment, replicaSet, pod).Build()
	reconciler := &TrustyAIServiceReconciler{Client: c, Scheme: scheme}
	if err := reconciler.reconcileNetworkPolicy(ctx, owner); err == nil {
		t.Fatal("Pod controlled by a foreign ReplicaSet was accepted")
	}
	actualPod := &corev1.Pod{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(pod), actualPod); err != nil {
		t.Fatal(err)
	}
	if _, found := actualPod.Labels[utils.NetworkPolicyOwnerUIDLabel]; found {
		t.Fatal("foreign-owned Pod received reserved TAS identity labels")
	}
}

func TestTASPodLabelPredicateIgnoresStatusOnlyChanges(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
		"app":                        "trustyai-test",
		"app.kubernetes.io/instance": "trustyai-test",
		"app.kubernetes.io/name":     "trustyai-test",
		"app.kubernetes.io/part-of":  "trustyai",
	}}}
	other := pod.DeepCopy()
	if tasPodLabelsChanged(pod, other) {
		t.Fatal("identical Pod labels should not trigger a label-drift reconcile")
	}
	other.Labels[utils.NetworkPolicyOwnerUIDLabel] = "owner"
	if !tasPodLabelsChanged(pod, other) {
		t.Fatal("TAS identity-label change should trigger reconciliation")
	}
}

func assertTASNetworkPolicyPorts(t *testing.T, rule networkingv1.NetworkPolicyIngressRule, want ...int32) {
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

func tasNetworkPolicyFixture(t *testing.T) (*runtime.Scheme, *trustyaiopendatahubiov1.TrustyAIService) {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, addToScheme := range []func(*runtime.Scheme) error{
		corev1.AddToScheme,
		appsv1.AddToScheme,
		networkingv1.AddToScheme,
		trustyaiopendatahubiov1.AddToScheme,
	} {
		if err := addToScheme(scheme); err != nil {
			t.Fatalf("register test API types: %v", err)
		}
	}
	owner := testTASNetworkPolicyOwner()
	owner.GetObjectKind().SetGroupVersionKind(trustyaiopendatahubiov1.GroupVersion.WithKind("TrustyAIService"))
	return scheme, owner
}

func testTASNetworkPolicyOwner() *trustyaiopendatahubiov1.TrustyAIService {
	return &trustyaiopendatahubiov1.TrustyAIService{ObjectMeta: metav1.ObjectMeta{
		Name:      "trustyai-example",
		Namespace: "trustyai-namespace",
		UID:       types.UID("tas-owner-uid"),
	}}
}

func tasNetworkPolicyWorkloadFixture(owner *trustyaiopendatahubiov1.TrustyAIService) (*appsv1.Deployment, *appsv1.ReplicaSet, *corev1.Pod) {
	selector := map[string]string{
		"app":                        owner.Name,
		"app.kubernetes.io/instance": owner.Name,
		"app.kubernetes.io/name":     owner.Name,
		"app.kubernetes.io/part-of":  "trustyai",
		"app.kubernetes.io/version":  "test-version",
	}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      owner.Name,
			Namespace: owner.Namespace,
			UID:       types.UID("tas-deployment-uid"),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: trustyaiopendatahubiov1.GroupVersion.String(),
				Kind:       "TrustyAIService",
				Name:       owner.Name,
				UID:        owner.UID,
				Controller: boolPointer(true),
			}},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: selector},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: selector}},
		},
	}
	replicaSet := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      owner.Name + "-abc123",
			Namespace: owner.Namespace,
			UID:       types.UID("tas-replicaset-uid"),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       deployment.Name,
				UID:        deployment.UID,
				Controller: boolPointer(true),
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
			Controller: boolPointer(true),
		}},
	}}
	return deployment, replicaSet, pod
}

func boolPointer(value bool) *bool { return &value }

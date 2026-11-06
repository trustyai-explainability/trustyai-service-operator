package evalhub

import (
	"context"
	"testing"

	evalhubv1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type podWatchFixture struct {
	reconciler *EvalHubReconciler
	owner      *evalhubv1.EvalHub
	deployment *appsv1.Deployment
	replicaSet *appsv1.ReplicaSet
}

func newPodWatchFixture(t *testing.T) *podWatchFixture {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, addToScheme := range []func(*runtime.Scheme) error{
		corev1.AddToScheme,
		appsv1.AddToScheme,
		evalhubv1.AddToScheme,
	} {
		if err := addToScheme(scheme); err != nil {
			t.Fatalf("register test types: %v", err)
		}
	}
	controller := true
	owner := testEvalHubNetworkPolicyOwner()
	owner.GetObjectKind().SetGroupVersionKind(evalhubv1.GroupVersion.WithKind("EvalHub"))
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      owner.Name,
			Namespace: owner.Namespace,
			UID:       types.UID("deployment-uid"),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: evalhubv1.GroupVersion.String(), Kind: "EvalHub", Name: owner.Name, UID: owner.UID, Controller: &controller,
			}},
		},
	}
	replicaSet := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      owner.Name + "-abc",
			Namespace: owner.Namespace,
			UID:       types.UID("replicaset-uid"),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1", Kind: "Deployment", Name: deployment.Name, UID: deployment.UID, Controller: &controller,
			}},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner, deployment, replicaSet).Build()
	return &podWatchFixture{
		reconciler: &EvalHubReconciler{Client: c, APIReader: c, Scheme: scheme, Namespace: "redhat-ods-applications"},
		owner:      owner,
		deployment: deployment,
		replicaSet: replicaSet,
	}
}

func (f *podWatchFixture) pod(name, replicaSetName string, replicaSetUID types.UID) *corev1.Pod {
	controller := true
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: f.owner.Namespace,
			Labels:    map[string]string{"app": "eval-hub", "instance": f.owner.Name},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1", Kind: "ReplicaSet", Name: replicaSetName, UID: replicaSetUID, Controller: &controller,
			}},
		},
	}
}

func TestMapEvalHubPodToRequestFollowsOwnershipChain(t *testing.T) {
	fixture := newPodWatchFixture(t)
	requests := fixture.reconciler.mapEvalHubPodToRequest(context.Background(), fixture.pod("pod-1", fixture.replicaSet.Name, fixture.replicaSet.UID))
	if len(requests) != 1 || requests[0].Name != fixture.owner.Name || requests[0].Namespace != fixture.owner.Namespace {
		t.Fatalf("requests = %#v, want a single request for %s/%s", requests, fixture.owner.Namespace, fixture.owner.Name)
	}
}

func TestMapEvalHubPodToRequestIgnoresUnprovenOwnership(t *testing.T) {
	fixture := newPodWatchFixture(t)
	cases := map[string]*corev1.Pod{
		"replica set UID mismatch": fixture.pod("pod-1", fixture.replicaSet.Name, types.UID("someone-else")),
		"replica set missing":      fixture.pod("pod-2", "gone-abc", types.UID("gone-uid")),
	}
	for name, pod := range cases {
		if requests := fixture.reconciler.mapEvalHubPodToRequest(context.Background(), pod); len(requests) != 0 {
			t.Fatalf("%s: requests = %#v, want none", name, requests)
		}
	}
	bare := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "bare", Namespace: fixture.owner.Namespace}}
	if requests := fixture.reconciler.mapEvalHubPodToRequest(context.Background(), bare); len(requests) != 0 {
		t.Fatalf("Pod without a controller reference produced requests %#v", requests)
	}
}

func TestEvalHubPodIdentityPredicates(t *testing.T) {
	hinted := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
		utils.NetworkPolicyComponentLabel: "evalhub",
	}}}
	reservedOnly := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
		utils.NetworkPolicyOwnerUIDLabel: "uid",
	}}}
	workloadLabels := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
		"app": "eval-hub", "instance": "evalhub-example",
	}}}
	unrelated := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "something-else"}}}

	for name, pod := range map[string]*corev1.Pod{"component label": hinted, "reserved label": reservedOnly, "workload labels": workloadLabels} {
		if !hasEvalHubPodIdentityHint(pod) {
			t.Fatalf("%s: hasEvalHubPodIdentityHint() = false, want true", name)
		}
	}
	if hasEvalHubPodIdentityHint(unrelated) {
		t.Fatal("hasEvalHubPodIdentityHint() accepted an unrelated Pod")
	}
	if hasEvalHubPodIdentityHint(nil) {
		t.Fatal("hasEvalHubPodIdentityHint() accepted nil")
	}

	// Removal is the case that matters, so an old Pod with the hint and a new
	// Pod without it must still enqueue.
	stripped := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "eval-hub", "instance": "evalhub-example"}}}
	if !evalHubPodLabelsChanged(hinted, stripped) {
		t.Fatal("evalHubPodLabelsChanged() missed identity labels being removed")
	}
	if evalHubPodLabelsChanged(hinted, hinted.DeepCopy()) {
		t.Fatal("evalHubPodLabelsChanged() fired without a label change")
	}
	if evalHubPodLabelsChanged(unrelated, unrelated.DeepCopy()) {
		t.Fatal("evalHubPodLabelsChanged() fired for an unrelated Pod")
	}
}

var _ client.Object = &corev1.Pod{}

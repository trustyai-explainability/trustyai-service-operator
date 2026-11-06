package utils

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type podRepairFixture struct {
	scheme       *runtime.Scheme
	owner        *corev1.ConfigMap
	deployment   *appsv1.Deployment
	replicaSet   *appsv1.ReplicaSet
	identity     NetworkPolicyIdentity
	reconciler   client.Client
	identityKeys map[string]string
}

func newPodRepairFixture(t *testing.T, configure func(*podRepairFixture)) *podRepairFixture {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	owner := &corev1.ConfigMap{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{Name: "instance", Namespace: "operand", UID: types.UID("owner-uid")},
	}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "instance",
			Namespace: "operand",
			UID:       types.UID("deployment-uid"),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Name:       owner.Name,
				UID:        owner.UID,
				Controller: pointerToTrue(),
			}},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "eval-hub"}},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "eval-hub"}}},
		},
	}
	replicaSet := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "instance-abc",
			Namespace: "operand",
			UID:       types.UID("replicaset-uid"),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       deployment.Name,
				UID:        deployment.UID,
				Controller: pointerToTrue(),
			}},
		},
	}
	identity := NetworkPolicyIdentity{
		OwnerKind: schema.GroupKind{Group: "", Kind: "ConfigMap"},
		OwnerUID:  owner.UID,
		Component: "evalhub",
		Role:      "api",
	}
	keys, err := identity.Labels()
	if err != nil {
		t.Fatal(err)
	}
	fixture := &podRepairFixture{
		scheme:       scheme,
		owner:        owner,
		deployment:   deployment,
		replicaSet:   replicaSet,
		identity:     identity,
		identityKeys: keys,
	}
	if configure != nil {
		configure(fixture)
	}
	fixture.reconciler = fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(owner, deployment, replicaSet).
		Build()
	return fixture
}

func pointerToTrue() *bool {
	value := true
	return &value
}

func (f *podRepairFixture) pod(t *testing.T, name string, labels map[string]string, controller *metav1.OwnerReference) *corev1.Pod {
	t.Helper()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "operand",
			Labels:    labels,
		},
	}
	if controller != nil {
		pod.OwnerReferences = []metav1.OwnerReference{*controller}
	}
	if err := f.reconciler.Create(context.Background(), pod); err != nil {
		t.Fatalf("create Pod %s: %v", name, err)
	}
	return pod
}

func (f *podRepairFixture) repair(t *testing.T) []WorkloadPodLabelsRestored {
	t.Helper()
	restored, err := RepairOwnedWorkloadNetworkPolicyPodLabels(
		context.Background(), f.reconciler, f.reconciler, f.deployment, f.scheme, f.owner, f.identity)
	if err != nil {
		t.Fatalf("RepairOwnedWorkloadNetworkPolicyPodLabels() error = %v", err)
	}
	return restored
}

func (f *podRepairFixture) assertIdentityLabels(t *testing.T, name string) {
	t.Helper()
	pod := &corev1.Pod{}
	if err := f.reconciler.Get(context.Background(), client.ObjectKey{Namespace: "operand", Name: name}, pod); err != nil {
		t.Fatalf("get Pod %s: %v", name, err)
	}
	for key, value := range f.identityKeys {
		if pod.Labels[key] != value {
			t.Fatalf("Pod %s label %q = %q, want %q", name, key, pod.Labels[key], value)
		}
	}
}

func TestRepairPodIdentityRestoresStrippedLabels(t *testing.T) {
	fixture := newPodRepairFixture(t, nil)
	fixture.pod(t, "instance-abc-1", map[string]string{"app": "eval-hub"}, &metav1.OwnerReference{
		APIVersion: "apps/v1", Kind: "ReplicaSet", Name: fixture.replicaSet.Name, UID: fixture.replicaSet.UID, Controller: pointerToTrue(),
	})

	restored := fixture.repair(t)
	if len(restored) != 1 || restored[0].Name != "instance-abc-1" {
		t.Fatalf("restored = %#v, want the single selected Pod", restored)
	}
	fixture.assertIdentityLabels(t, "instance-abc-1")

	// A second pass is a no-op: the repair reports only work it actually did.
	if again := fixture.repair(t); len(again) != 0 {
		t.Fatalf("second repair restored %#v, want nothing", again)
	}
}

func TestRepairPodIdentityLeavesForeignPodsAlone(t *testing.T) {
	fixture := newPodRepairFixture(t, nil)
	// A principal with Pod-create permission can reproduce the Deployment
	// selector. Such a Pod must never inherit this workload's grants.
	fixture.pod(t, "forged", map[string]string{"app": "eval-hub"}, nil)
	foreign := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other-abc",
			Namespace: "operand",
			UID:       types.UID("other-replicaset-uid"),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1", Kind: "Deployment", Name: "other", UID: types.UID("other-deployment-uid"), Controller: pointerToTrue(),
			}},
		},
	}
	if err := fixture.reconciler.Create(context.Background(), foreign); err != nil {
		t.Fatal(err)
	}
	fixture.pod(t, "other-abc-1", map[string]string{"app": "eval-hub"}, &metav1.OwnerReference{
		APIVersion: "apps/v1", Kind: "ReplicaSet", Name: foreign.Name, UID: foreign.UID, Controller: pointerToTrue(),
	})
	// A Pod whose ReplicaSet has been deleted cannot be attributed either.
	fixture.pod(t, "stale", map[string]string{"app": "eval-hub"}, &metav1.OwnerReference{
		APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "gone-abc", UID: types.UID("gone-uid"), Controller: pointerToTrue(),
	})

	if restored := fixture.repair(t); len(restored) != 0 {
		t.Fatalf("restored = %#v, want no Pod outside the ownership chain to be relabelled", restored)
	}
	for _, name := range []string{"forged", "other-abc-1", "stale"} {
		pod := &corev1.Pod{}
		if err := fixture.reconciler.Get(context.Background(), client.ObjectKey{Namespace: "operand", Name: name}, pod); err != nil {
			t.Fatal(err)
		}
		if pod.Labels[NetworkPolicyComponentLabel] != "" {
			t.Fatalf("Pod %s received reserved labels: %#v", name, pod.Labels)
		}
	}
}

func TestRepairPodIdentityRefusesUnownedDeployment(t *testing.T) {
	fixture := newPodRepairFixture(t, func(f *podRepairFixture) {
		f.deployment.OwnerReferences = nil
	})
	_, err := RepairOwnedWorkloadNetworkPolicyPodLabels(
		context.Background(), fixture.reconciler, fixture.reconciler, fixture.deployment, fixture.scheme, fixture.owner, fixture.identity)
	if err == nil {
		t.Fatal("repair accepted a Deployment the owner does not control")
	}
}

func TestRepairPodIdentityRefusesForeignIdentity(t *testing.T) {
	fixture := newPodRepairFixture(t, nil)
	other := fixture.identity
	other.OwnerUID = types.UID("someone-else")
	if _, err := RepairOwnedWorkloadNetworkPolicyPodLabels(
		context.Background(), fixture.reconciler, fixture.reconciler, fixture.deployment, fixture.scheme, fixture.owner, other); err == nil {
		t.Fatal("repair accepted an identity that does not belong to the owner")
	}
}

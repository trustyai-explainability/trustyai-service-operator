package utils

import (
	"context"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// This test uses a local API server/etcd, never the current kubeconfig cluster.
// It verifies serialization/defaulting and UID preconditions, not GC or CNI.
func TestNetworkPolicyAPI(t *testing.T) {
	assets := os.Getenv("KUBEBUILDER_ASSETS")
	if assets == "" {
		t.Skip("KUBEBUILDER_ASSETS unset: local API serialization test not run")
	}
	environment := &envtest.Environment{BinaryAssetsDirectory: assets}
	cfg, err := environment.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := environment.Stop(); err != nil {
			t.Error(err)
		}
	})
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	api, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "policy-test"}}
	if err := api.Create(ctx, namespace); err != nil {
		t.Fatal(err)
	}
	owner := &corev1.ConfigMap{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"}, ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: namespace.Name}}
	if err := api.Create(ctx, owner); err != nil {
		t.Fatal(err)
	}
	identity := NetworkPolicyIdentity{OwnerKind: schema.GroupKind{Kind: "ConfigMap"}, OwnerUID: owner.UID, Component: "evalhub", Role: "api"}
	target, _ := identity.Labels()
	name, _ := identity.Name("egress")
	policy := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace.Name}, Spec: networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{MatchLabels: target}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}}}
	authority := NetworkPolicyAuthority{Namespaces: map[string]bool{namespace.Name: true}}
	counter := &policyCountingClient{Client: api}
	for _, mode := range []NetworkPolicyEgressMode{NetworkPolicyDenyAll, NetworkPolicyRestricted, NetworkPolicyAllowAll, NetworkPolicyDenyAll} {
		intent := &NetworkPolicyEgressIntent{Mode: mode}
		policy.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{}
		if mode == NetworkPolicyRestricted {
			policy.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{restrictedRule()}
		}
		if mode == NetworkPolicyAllowAll {
			intent = allowIntent()
			policy.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{{}}
		}
		request := WorkloadNetworkPolicy{Policy: policy, Egress: intent}
		if err := ReconcileWorkloadNetworkPolicy(ctx, counter, scheme, owner, identity, request, authority); err != nil {
			t.Fatal(err)
		}
		readback := &networkingv1.NetworkPolicy{}
		if err := api.Get(ctx, client.ObjectKeyFromObject(policy), readback); err != nil {
			t.Fatal(err)
		}
		if err := ValidateWorkloadNetworkPolicy(readback, identity, intent); err != nil {
			t.Fatal(err)
		}
		writes := counter.updates
		if err := ReconcileWorkloadNetworkPolicy(ctx, counter, scheme, owner, identity, request, authority); err != nil {
			t.Fatal(err)
		}
		if counter.updates != writes {
			t.Fatalf("%s repeated write after API normalization", mode)
		}
	}
	// Ingress-only empty annotation maps must also be stable after API omission.
	ingress := policy.DeepCopy()
	ingress.Name += "-ingress"
	ingress.Spec.Egress = nil
	ingress.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
	if err := ReconcileWorkloadNetworkPolicy(ctx, counter, scheme, owner, identity, WorkloadNetworkPolicy{Policy: ingress}, authority); err != nil {
		t.Fatal(err)
	}
	writes := counter.updates
	if err := ReconcileWorkloadNetworkPolicy(ctx, counter, scheme, owner, identity, WorkloadNetworkPolicy{Policy: ingress}, authority); err != nil {
		t.Fatal(err)
	}
	if counter.updates != writes {
		t.Fatal("empty annotations caused repeated ingress update")
	}
	readback := &networkingv1.NetworkPolicy{}
	key := client.ObjectKeyFromObject(policy)
	if err := api.Get(ctx, key, readback); err != nil {
		t.Fatal(err)
	}
	if err := DeleteOwnedWorkloadNetworkPolicy(ctx, api, scheme, owner, key, "foreign-uid", authority); err == nil {
		t.Fatal("replacement UID accepted")
	}
	// Replace the object between cleanup's GET and DELETE. The actual API must
	// reject the old UID precondition and leave the replacement untouched.
	racing := &policyDeleteRaceClient{Client: api, replace: func() error {
		if err := api.Delete(ctx, readback); err != nil {
			return err
		}
		replacement := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: readback.Name, Namespace: readback.Namespace, OwnerReferences: readback.OwnerReferences}, Spec: readback.Spec}
		return api.Create(ctx, replacement)
	}}
	if err := DeleteOwnedWorkloadNetworkPolicy(ctx, racing, scheme, owner, key, readback.UID, authority); !apierrors.IsConflict(err) {
		t.Fatalf("expected UID conflict, got %v", err)
	}
	replacement := &networkingv1.NetworkPolicy{}
	if err := api.Get(ctx, key, replacement); err != nil {
		t.Fatal(err)
	}
	if replacement.UID == readback.UID {
		t.Fatal("race did not replace policy")
	}
	if err := DeleteOwnedWorkloadNetworkPolicy(ctx, api, scheme, owner, key, replacement.UID, authority); err != nil {
		t.Fatal(err)
	}
}

type policyDeleteRaceClient struct {
	client.Client
	replace func() error
}

func (c *policyDeleteRaceClient) Delete(ctx context.Context, object client.Object, opts ...client.DeleteOption) error {
	if err := c.replace(); err != nil {
		return err
	}
	return c.Client.Delete(ctx, object, opts...)
}

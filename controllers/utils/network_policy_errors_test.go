package utils

import (
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestNetworkPolicyCompleteSetStatus(t *testing.T) {
	for _, failure := range []bool{false, true} {
		ctx := context.Background()
		scheme, owner, c, identity, egress, authority := reconciliationFixture(t)
		ingress := egress.DeepCopy()
		ingress.Name += "-ingress"
		ingress.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
		policies := []WorkloadNetworkPolicy{{Policy: ingress}, {Policy: egress, Egress: &NetworkPolicyEgressIntent{Mode: NetworkPolicyDenyAll}}}
		if failure {
			foreign := egress.DeepCopy()
			foreign.UID = "foreign-policy"
			if err := c.Client.Create(ctx, foreign); err != nil {
				t.Fatal(err)
			}
		}
		calls := 0
		statusErr := errors.New("status write failed")
		err := ReconcileWorkloadNetworkPolicySet(ctx, c, scheme, owner, identity, policies, authority, func(ready bool, cause error) error {
			calls++
			if ready == failure || (cause != nil) != failure {
				t.Fatalf("partial-set readiness reported: ready=%v cause=%v", ready, cause)
			}
			if failure {
				return statusErr
			}
			return nil
		})
		if calls != 1 || (err != nil) != failure || (failure && !errors.Is(err, statusErr)) {
			t.Fatal("wrong set result", calls, err)
		}
		if c.creates != 1 && failure {
			t.Fatal("first policy did not reconcile before second failed")
		}
		// Invalid/missing egress is rejected before any API writes, with failure status.
		before := c.creates + c.updates
		if err := ReconcileWorkloadNetworkPolicySet(ctx, c, scheme, owner, identity, policies[:1], authority, func(ready bool, cause error) error {
			if ready || cause == nil {
				t.Fatal("invalid set reported ready")
			}
			return nil
		}); err == nil {
			t.Fatal("invalid set accepted")
		}
		if c.creates+c.updates != before {
			t.Fatal("invalid set wrote a policy")
		}
	}
}

func TestNetworkPolicySetRejectsMixedModes(t *testing.T) {
	identity, deny := policyFixture()
	allow := deny.DeepCopy()
	allow.Name += "-allow"
	allow.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{{}}
	ingress := deny.DeepCopy()
	ingress.Name += "-ingress"
	ingress.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
	policies := []WorkloadNetworkPolicy{{Policy: ingress}, {Policy: deny, Egress: &NetworkPolicyEgressIntent{Mode: NetworkPolicyDenyAll}}, {Policy: allow, Egress: allowIntent()}}
	if err := ValidateWorkloadNetworkPolicySet(policies, identity); err == nil {
		t.Fatal("mixed egress modes accepted")
	}
}

func TestNetworkPolicyAPIErrors(t *testing.T) {
	for _, test := range []string{"read forbidden", "create forbidden", "create race", "update conflict", "deleting owner", "cross namespace"} {
		t.Run(test, func(t *testing.T) {
			ctx := context.Background()
			scheme, owner, c, identity, p, authority := reconciliationFixture(t)
			request := WorkloadNetworkPolicy{Policy: p, Egress: &NetworkPolicyEgressIntent{Mode: NetworkPolicyDenyAll}}
			var expected error
			resource := schema.GroupResource{Group: "networking.k8s.io", Resource: "networkpolicies"}
			switch test {
			case "read forbidden":
				expected = apierrors.NewForbidden(resource, p.Name, errors.New("denied"))
				c.failGet = expected
			case "create forbidden":
				expected = apierrors.NewForbidden(resource, p.Name, errors.New("denied"))
				c.failCreate = expected
			case "create race":
				expected = apierrors.NewAlreadyExists(resource, p.Name)
				c.failCreate = expected
			case "update conflict":
				if err := ReconcileWorkloadNetworkPolicy(ctx, c, scheme, owner, identity, request, authority); err != nil {
					t.Fatal(err)
				}
				p.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{{}}
				request.Egress = allowIntent()
				expected = apierrors.NewConflict(resource, p.Name, errors.New("stale version"))
				c.failUpdate = expected
			case "deleting owner":
				owner.Finalizers = []string{"test.example/hold"}
				if err := c.Client.Update(ctx, owner); err != nil {
					t.Fatal(err)
				}
				if err := c.Client.Delete(ctx, owner); err != nil {
					t.Fatal(err)
				}
			case "cross namespace":
				p.Namespace = "other"
				authority.Namespaces["other"] = true
			}
			err := ReconcileWorkloadNetworkPolicy(ctx, c, scheme, owner, identity, request, authority)
			if err == nil || (expected != nil && !errors.Is(err, expected)) {
				t.Fatal("missing/wrong error", err)
			}
		})
	}
}

func TestNetworkPolicyPreservesNonControllerReferences(t *testing.T) {
	ctx := context.Background()
	scheme, owner, c, identity, p, authority := reconciliationFixture(t)
	request := WorkloadNetworkPolicy{Policy: p, Egress: &NetworkPolicyEgressIntent{Mode: NetworkPolicyDenyAll}}
	if err := ReconcileWorkloadNetworkPolicy(ctx, c, scheme, owner, identity, request, authority); err != nil {
		t.Fatal(err)
	}
	got := &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(p), got); err != nil {
		t.Fatal(err)
	}
	other := metav1.OwnerReference{APIVersion: "v1", Kind: "Secret", Name: "other", UID: "other-uid"}
	got.OwnerReferences = append(got.OwnerReferences, other)
	got.Spec.PodSelector.MatchLabels["extra"] = "drift"
	if err := c.Client.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileWorkloadNetworkPolicy(ctx, c, scheme, owner, identity, request, authority); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(p), got); err != nil {
		t.Fatal(err)
	}
	if len(got.OwnerReferences) != 2 || got.OwnerReferences[1] != other {
		t.Fatal("non-controller reference lost")
	}
}

func TestNetworkPolicyTemplateSelectorConflicts(t *testing.T) {
	_, owner, _, identity, _, _ := reconciliationFixture(t)
	// Reuse the positive test's setup but exercise both MatchLabels/expressions.
	for _, selector := range []*metav1.LabelSelector{
		nil,
		{MatchLabels: map[string]string{NetworkPolicyRoleLabel: "legacy"}},
		{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: NetworkPolicyRoleLabel, Operator: metav1.LabelSelectorOpNotIn, Values: []string{"api"}}}},
	} {
		deployment := ownedDeploymentFixture(owner)
		deployment.Spec.Selector = selector
		original := deployment.DeepCopy()
		if err := LabelOwnedNetworkPolicyDeployment(deployment, owner, identity); err == nil {
			t.Fatal("selector conflict accepted")
		}
		if deployment.Spec.Template.Labels[NetworkPolicyRoleLabel] != original.Spec.Template.Labels[NetworkPolicyRoleLabel] {
			t.Fatal("template modified on failure")
		}
	}
}

// Separate from production builders; only metadata helper behavior is under test.
func ownedDeploymentFixture(owner *corev1.ConfigMap) *appsv1.Deployment {
	return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: owner.Namespace, OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "ConfigMap", Name: owner.Name, UID: owner.UID, Controller: new(true)}}}, Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "legacy"}}}}}
}

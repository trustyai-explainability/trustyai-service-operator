package utils

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"
)

func policyFixture() (NetworkPolicyIdentity, *networkingv1.NetworkPolicy) {
	identity := NetworkPolicyIdentity{OwnerKind: schema.GroupKind{Kind: "ConfigMap"}, OwnerUID: "owner-uid", Component: "evalhub", Role: "api"}
	target, _ := identity.Labels()
	name, _ := identity.Name("egress")
	return identity, &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "operand"}, Spec: networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{MatchLabels: target}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}}}
}
func restrictedRule() networkingv1.NetworkPolicyEgressRule {
	port := intstr.FromInt32(443)
	return networkingv1.NetworkPolicyEgressRule{To: []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "backend"}}, NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "backend"}}}}, Ports: []networkingv1.NetworkPolicyPort{{Port: &port}}}
}
func allowIntent() *NetworkPolicyEgressIntent {
	return &NetworkPolicyEgressIntent{Mode: NetworkPolicyAllowAll, Rationale: "Configured dynamic model destinations", ResidualRisk: "Unrestricted outbound; no exfiltration prevention"}
}

func TestNetworkPolicyIdentity(t *testing.T) {
	identity, policy := policyFixture()
	name, _ := identity.Name("egress")
	if len(name) > 63 || len(validation.IsDNS1123Label(name)) > 0 {
		t.Fatal(name)
	}
	variants := []NetworkPolicyIdentity{identity, identity, identity, identity}
	variants[0].OwnerKind.Kind = "Secret"
	variants[1].OwnerKind.Group = "example.org"
	variants[2].OwnerUID = "replacement-owner"
	variants[3].Role = "mcp"
	for _, variant := range variants {
		other, err := variant.Name("egress")
		if err != nil || other == name {
			t.Fatalf("collision: %s %v", other, err)
		}
	}
	for _, purpose := range []string{"A.B", "a-b", strings.Repeat("x", 1000), "!!!"} {
		generated, err := identity.Name(purpose)
		if err != nil || generated == name || len(generated) > 63 {
			t.Fatal(generated, err)
		}
	}
	a, _ := identity.Name("A.B")
	b, _ := identity.Name("a-b")
	if a == b {
		t.Fatal("normalization collision")
	}
	long := identity
	long.OwnerUID = types.UID(strings.Repeat("a", 100))
	longLabels, err := long.Labels()
	if err != nil || len(longLabels[NetworkPolicyOwnerUIDLabel]) > 63 {
		t.Fatal(longLabels, err)
	}
	selector, err := metav1.LabelSelectorAsSelector(&policy.Spec.PodSelector)
	if err != nil {
		t.Fatal(err)
	}
	own, _ := identity.Labels()
	if !selector.Matches(labels.Set(own)) {
		t.Fatal("own workload excluded")
	}
	for _, key := range []string{NetworkPolicyOwnerUIDLabel, NetworkPolicyRoleLabel, NetworkPolicyComponentLabel} {
		foreign, _ := identity.Labels()
		foreign[key] = "foreign"
		if selector.Matches(labels.Set(foreign)) {
			t.Fatal("foreign workload selected")
		}
	}
	identity.OwnerUID = ""
	if _, err := identity.Name("egress"); err == nil {
		t.Fatal("missing UID accepted")
	}
}

func TestNetworkPolicyEgressModes(t *testing.T) {
	tests := []struct {
		name   string
		intent *NetworkPolicyEgressIntent
		rules  []networkingv1.NetworkPolicyEgressRule
		valid  bool
	}{
		{"deny nil", &NetworkPolicyEgressIntent{Mode: NetworkPolicyDenyAll}, nil, true},
		{"deny empty", &NetworkPolicyEgressIntent{Mode: NetworkPolicyDenyAll}, []networkingv1.NetworkPolicyEgressRule{}, true},
		{"deny allow", &NetworkPolicyEgressIntent{Mode: NetworkPolicyDenyAll}, []networkingv1.NetworkPolicyEgressRule{{}}, false},
		{"restricted", &NetworkPolicyEgressIntent{Mode: NetworkPolicyRestricted}, []networkingv1.NetworkPolicyEgressRule{restrictedRule()}, true},
		{"restricted empty", &NetworkPolicyEgressIntent{Mode: NetworkPolicyRestricted}, nil, false},
		{"restricted permissive", &NetworkPolicyEgressIntent{Mode: NetworkPolicyRestricted}, []networkingv1.NetworkPolicyEgressRule{{}}, false},
		{"allow", allowIntent(), []networkingv1.NetworkPolicyEgressRule{{}}, true},
		{"allow missing rule", allowIntent(), nil, false},
		{"allow mixed", allowIntent(), []networkingv1.NetworkPolicyEgressRule{{}, restrictedRule()}, false},
		{"allow restricted", allowIntent(), []networkingv1.NetworkPolicyEgressRule{restrictedRule()}, false},
		{"no intent", nil, []networkingv1.NetworkPolicyEgressRule{{}}, false},
		{"empty mode", &NetworkPolicyEgressIntent{}, nil, false},
		{"unknown mode", &NetworkPolicyEgressIntent{Mode: " deny "}, nil, false},
		{"missing rationale", &NetworkPolicyEgressIntent{Mode: NetworkPolicyAllowAll, Rationale: " ", ResidualRisk: "risk"}, []networkingv1.NetworkPolicyEgressRule{{}}, false},
		{"missing risk", &NetworkPolicyEgressIntent{Mode: NetworkPolicyAllowAll, Rationale: "reason"}, []networkingv1.NetworkPolicyEgressRule{{}}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			identity, policy := policyFixture()
			policy.Spec.Egress = test.rules
			policy.Annotations = map[string]string{networkPolicyEgressModeAnnotation: "AllowAll", networkPolicyRationaleAnnotation: "spoofed"}
			err := ValidateWorkloadNetworkPolicy(policy, identity, test.intent)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v error=%v", test.valid, err)
			}
		})
	}
}

func TestNetworkPolicyValidationNonRegression(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*networkingv1.NetworkPolicy)
	}{
		{"missing directions", func(p *networkingv1.NetworkPolicy) { p.Spec.PolicyTypes = nil }},
		{"duplicate direction", func(p *networkingv1.NetworkPolicy) {
			p.Spec.PolicyTypes = append(p.Spec.PolicyTypes, networkingv1.PolicyTypeEgress)
		}},
		{"unknown direction", func(p *networkingv1.NetworkPolicy) { p.Spec.PolicyTypes = []networkingv1.PolicyType{"Unknown"} }},
		{"empty target", func(p *networkingv1.NetworkPolicy) { p.Spec.PodSelector = metav1.LabelSelector{} }},
		{"wrong target UID", func(p *networkingv1.NetworkPolicy) {
			p.Spec.PodSelector.MatchLabels[NetworkPolicyOwnerUIDLabel] = "foreign"
		}},
		{"malformed target", func(p *networkingv1.NetworkPolicy) { p.Spec.PodSelector.MatchLabels["bad key"] = "value" }},
		{"missing destinations", func(p *networkingv1.NetworkPolicy) { p.Spec.Egress[0].To = nil }},
		{"empty peer", func(p *networkingv1.NetworkPolicy) { p.Spec.Egress[0].To = []networkingv1.NetworkPolicyPeer{{}} }},
		{"all namespace", func(p *networkingv1.NetworkPolicy) {
			p.Spec.Egress[0].To[0].NamespaceSelector = &metav1.LabelSelector{}
		}},
		{"negative peer", func(p *networkingv1.NetworkPolicy) {
			p.Spec.Egress[0].To[0].PodSelector = &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "app", Operator: metav1.LabelSelectorOpNotIn, Values: []string{"bad"}}}}
		}},
		{"missing ports", func(p *networkingv1.NetworkPolicy) { p.Spec.Egress[0].Ports = nil }},
		{"all ports", func(p *networkingv1.NetworkPolicy) { p.Spec.Egress[0].Ports[0].Port = nil }},
		{"invalid port", func(p *networkingv1.NetworkPolicy) { v := intstr.FromInt32(0); p.Spec.Egress[0].Ports[0].Port = &v }},
		{"bad name", func(p *networkingv1.NetworkPolicy) {
			v := intstr.FromString("UPPER")
			p.Spec.Egress[0].Ports[0].Port = &v
		}},
		{"name endPort", func(p *networkingv1.NetworkPolicy) {
			v := intstr.FromString("https")
			end := int32(500)
			p.Spec.Egress[0].Ports[0].Port = &v
			p.Spec.Egress[0].Ports[0].EndPort = &end
		}},
		{"bad range", func(p *networkingv1.NetworkPolicy) { end := int32(1); p.Spec.Egress[0].Ports[0].EndPort = &end }},
		{"bad protocol", func(p *networkingv1.NetworkPolicy) {
			v := corev1.Protocol("ICMP")
			p.Spec.Egress[0].Ports[0].Protocol = &v
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			identity, p := policyFixture()
			p.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{restrictedRule()}
			test.mutate(p)
			if err := ValidateWorkloadNetworkPolicy(p, identity, &NetworkPolicyEgressIntent{Mode: NetworkPolicyRestricted}); err == nil {
				t.Fatal("invalid policy accepted")
			}
		})
	}
	identity, p := policyFixture()
	p.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
	p.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{{}}
	if err := ValidateWorkloadNetworkPolicy(p, identity, nil); err == nil {
		t.Fatal("unrestricted ingress accepted")
	}
	rule := restrictedRule()
	p.Spec.Ingress[0] = networkingv1.NetworkPolicyIngressRule{From: rule.To, Ports: rule.Ports}
	if err := ValidateWorkloadNetworkPolicy(p, identity, nil); err != nil {
		t.Fatal(err)
	}
	p.Spec.Ingress[0].Ports = nil
	if err := ValidateWorkloadNetworkPolicy(p, identity, nil); err == nil {
		t.Fatal("missing ingress ports accepted")
	}
}

func TestNetworkPolicyIPBlock(t *testing.T) {
	for _, test := range []struct {
		cidr   string
		except []string
		valid  bool
	}{
		{"10.0.0.1/24", []string{"10.0.0.129/25"}, true}, {"2001:db8::/32", []string{"2001:db8:1::/48"}, true},
		{"0.0.0.0/0", nil, false}, {"::/0", nil, false}, {"bad", nil, false},
		{"10.0.0.0/24", []string{"10.0.0.0/16"}, false}, {"10.0.0.0/24", []string{"10.0.1.0/25"}, false},
		{"10.0.0.0/24", []string{"::/128"}, false}, {"10.0.0.0/24", []string{"bad"}, false},
	} {
		peer := networkingv1.NetworkPolicyPeer{IPBlock: &networkingv1.IPBlock{CIDR: test.cidr, Except: test.except}}
		if err := validatePolicyPeer(peer); (err == nil) != test.valid {
			t.Fatalf("%+v: %v", test, err)
		}
	}
}

func TestNetworkPolicyRoundTripAndSet(t *testing.T) {
	for _, mode := range []NetworkPolicyEgressMode{NetworkPolicyDenyAll, NetworkPolicyAllowAll} {
		identity, p := policyFixture()
		intent := &NetworkPolicyEgressIntent{Mode: mode}
		if mode == NetworkPolicyAllowAll {
			intent = allowIntent()
			p.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{{}}
		}
		for _, codec := range []struct {
			encode func(interface{}) ([]byte, error)
			decode func([]byte, interface{}) error
		}{
			{json.Marshal, json.Unmarshal}, {yaml.Marshal, func(b []byte, v interface{}) error { return yaml.Unmarshal(b, v) }},
		} {
			data, err := codec.encode(p)
			if err != nil {
				t.Fatal(err)
			}
			var readback networkingv1.NetworkPolicy
			if err := codec.decode(data, &readback); err != nil {
				t.Fatal(err)
			}
			if err := ValidateWorkloadNetworkPolicy(&readback, identity, intent); err != nil {
				t.Fatal(err)
			}
		}
		ingress := p.DeepCopy()
		ingress.Name += "-ingress"
		ingress.Spec.Egress = nil
		ingress.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
		if err := ValidateWorkloadNetworkPolicySet([]WorkloadNetworkPolicy{{Policy: ingress}}, identity); err == nil {
			t.Fatal("missing egress accepted")
		}
		items := []WorkloadNetworkPolicy{{Policy: ingress}, {Policy: p, Egress: intent}}
		if err := ValidateWorkloadNetworkPolicySet(items, identity); err != nil {
			t.Fatal(err)
		}
		p.Spec.PodSelector.MatchLabels["extra"] = "subset"
		if err := ValidateWorkloadNetworkPolicySet(items, identity); err == nil {
			t.Fatal("different target accepted")
		}
	}
}

type policyCountingClient struct {
	client.Client
	updates                         int
	creates                         int
	failGet, failUpdate, failCreate error
}

func (c *policyCountingClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*networkingv1.NetworkPolicy); ok && c.failGet != nil {
		return c.failGet
	}
	return c.Client.Get(ctx, key, obj, opts...)
}
func (c *policyCountingClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	c.updates++
	if c.failUpdate != nil {
		return c.failUpdate
	}
	return c.Client.Update(ctx, obj, opts...)
}
func (c *policyCountingClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	c.creates++
	if c.failCreate != nil {
		return c.failCreate
	}
	return c.Client.Create(ctx, obj, opts...)
}
func reconciliationFixture(t *testing.T) (*runtime.Scheme, *corev1.ConfigMap, *policyCountingClient, NetworkPolicyIdentity, *networkingv1.NetworkPolicy, NetworkPolicyAuthority) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	identity, p := policyFixture()
	owner := &corev1.ConfigMap{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"}, ObjectMeta: metav1.ObjectMeta{Name: "instance", Namespace: "operand", UID: identity.OwnerUID}}
	c := &policyCountingClient{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner).Build()}
	return scheme, owner, c, identity, p, NetworkPolicyAuthority{Namespaces: map[string]bool{"operand": true}}
}

func TestNetworkPolicyReconciliation(t *testing.T) {
	ctx := context.Background()
	scheme, owner, c, identity, p, authority := reconciliationFixture(t)
	request := WorkloadNetworkPolicy{Policy: p, Egress: &NetworkPolicyEgressIntent{Mode: NetworkPolicyDenyAll}}
	reconcile := func() error {
		return ReconcileWorkloadNetworkPolicy(ctx, c, scheme, owner, identity, request, authority)
	}
	if err := reconcile(); err != nil {
		t.Fatal(err)
	}
	got := &networkingv1.NetworkPolicy{}
	key := client.ObjectKeyFromObject(p)
	if err := c.Get(ctx, key, got); err != nil {
		t.Fatal(err)
	}
	if !matchesController(got, owner) {
		t.Fatal("missing controller owner")
	}
	// Simulate stored empty-array omission and API defaulting; no update required.
	if err := reconcile(); err != nil {
		t.Fatal(err)
	}
	if c.updates != 0 {
		t.Fatal("non-idempotent deny reconciliation")
	}
	got.Annotations = map[string]string{"customer": "keep", "trustyai.opendatahub.io/other-feature": "keep"}
	got.Labels["customer"] = "keep"
	got.Spec.PodSelector.MatchLabels[NetworkPolicyRoleLabel] = "drift"
	if err := c.Client.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	if err := reconcile(); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, key, got); err != nil {
		t.Fatal(err)
	}
	if got.Labels["customer"] != "keep" || got.Annotations["trustyai.opendatahub.io/other-feature"] != "keep" || got.Spec.PodSelector.MatchLabels[NetworkPolicyRoleLabel] != "api" {
		t.Fatal("metadata/drift repair failed")
	}
	p.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{{}}
	request.Egress = allowIntent()
	if err := reconcile(); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, key, got); err != nil {
		t.Fatal(err)
	}
	if got.Annotations[networkPolicyBehaviorAnnotation] == "" {
		t.Fatal("unrestricted behavior not recorded")
	}
	p.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{restrictedRule()}
	request.Egress = &NetworkPolicyEgressIntent{Mode: NetworkPolicyRestricted}
	if err := reconcile(); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, key, got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Annotations[networkPolicyBehaviorAnnotation]; ok {
		t.Fatal("stale AllowAll metadata")
	}
	count := c.updates // Fake API does not default protocol; normalize read-back TCP explicitly.
	got.Spec.Egress[0].Ports[0].Protocol = new(corev1.ProtocolTCP)
	if err := c.Client.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	if err := reconcile(); err != nil {
		t.Fatal(err)
	}
	if c.updates != count {
		t.Fatal("TCP default caused update")
	}
	if err := c.Client.Delete(ctx, got); err != nil {
		t.Fatal(err)
	}
	if err := reconcile(); err != nil {
		t.Fatal(err)
	}
	if c.creates != 2 {
		t.Fatal("deleted policy not restored")
	}
	sentinel := errors.New("API denied")
	c.failGet = sentinel
	if err := reconcile(); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	c.failGet = nil
	c.failUpdate = sentinel
	p.Spec.Egress = nil
	request.Egress = &NetworkPolicyEgressIntent{Mode: NetworkPolicyDenyAll}
	if err := reconcile(); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
}

func TestNetworkPolicyOwnershipAndAuthority(t *testing.T) {
	for _, test := range []string{"foreign controller", "ownerless", "wrong namespace", "owner label conflict", "foreign manager", "stale live owner", "approved adoption", "wrong adoption UID"} {
		t.Run(test, func(t *testing.T) {
			ctx := context.Background()
			scheme, owner, c, identity, p, authority := reconciliationFixture(t)
			existing := p.DeepCopy()
			existing.UID = "policy-uid"
			existing.Spec.PodSelector.MatchLabels["untouched"] = "yes"
			switch test {
			case "foreign controller":
				existing.OwnerReferences = []metav1.OwnerReference{{APIVersion: "v1", Kind: "ConfigMap", Name: "foreign", UID: "foreign", Controller: new(true)}}
			case "wrong namespace":
				authority.Namespaces = nil
			case "owner label conflict":
				existing.Labels = map[string]string{NetworkPolicyOwnerUIDLabel: "foreign"}
			case "foreign manager":
				existing.Labels = map[string]string{networkPolicyManagedByLabel: "foreign"}
			case "stale live owner":
				owner.UID = "stale"
			case "approved adoption":
				authority.VerifiedAdoptionUID = existing.UID
			case "wrong adoption UID":
				authority.VerifiedAdoptionUID = "wrong"
			}
			if err := c.Client.Create(ctx, existing); err != nil {
				t.Fatal(err)
			}
			before, _ := json.Marshal(existing)
			err := ReconcileWorkloadNetworkPolicy(ctx, c, scheme, owner, identity, WorkloadNetworkPolicy{Policy: p, Egress: &NetworkPolicyEgressIntent{Mode: NetworkPolicyDenyAll}}, authority)
			if test == "approved adoption" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil {
				t.Fatal("unsafe reconciliation accepted")
			}
			after := &networkingv1.NetworkPolicy{}
			if err := c.Get(ctx, client.ObjectKeyFromObject(p), after); err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(after)
			if string(before) != string(data) {
				t.Fatal("rejected policy mutated")
			}
		})
	}
}

func TestNetworkPolicyCleanup(t *testing.T) {
	ctx := context.Background()
	scheme, owner, c, identity, p, authority := reconciliationFixture(t)
	p.UID = "policy-uid"
	if err := ReconcileWorkloadNetworkPolicy(ctx, c, scheme, owner, identity, WorkloadNetworkPolicy{Policy: p, Egress: &NetworkPolicyEgressIntent{Mode: NetworkPolicyDenyAll}}, authority); err != nil {
		t.Fatal(err)
	}
	key := client.ObjectKeyFromObject(p)
	// The fake client does not assign server UIDs; emulate API metadata here.
	stored := &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, key, stored); err != nil {
		t.Fatal(err)
	}
	stored.UID = p.UID
	if err := c.Client.Update(ctx, stored); err != nil {
		t.Fatal(err)
	}
	if err := DeleteOwnedWorkloadNetworkPolicy(ctx, c, scheme, owner, key, "replacement", authority); err == nil {
		t.Fatal("replacement UID deleted")
	}
	if err := DeleteOwnedWorkloadNetworkPolicy(ctx, c, scheme, owner, key, p.UID, authority); err != nil {
		t.Fatal(err)
	}
	if err := DeleteOwnedWorkloadNetworkPolicy(ctx, c, scheme, owner, key, p.UID, authority); err != nil {
		t.Fatal(err)
	}
	got := &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, key, got); !apierrors.IsNotFound(err) {
		t.Fatal(err)
	}
}

func TestNetworkPolicyOwnedTemplate(t *testing.T) {
	_, owner, _, identity, _, _ := reconciliationFixture(t)
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: owner.Namespace, OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "ConfigMap", Name: owner.Name, UID: owner.UID, Controller: new(true)}}}, Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "legacy"}}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "legacy", NetworkPolicyRoleLabel: "user-override"}}}}}
	selector, _ := json.Marshal(d.Spec.Selector)
	if err := LabelOwnedNetworkPolicyDeployment(d, owner, identity); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(d.Spec.Selector)
	if string(selector) != string(after) || d.Spec.Template.Labels[NetworkPolicyRoleLabel] != "api" {
		t.Fatal("selector mutated or reserved label not stamped")
	}
	d.OwnerReferences = nil
	before, _ := json.Marshal(d)
	if err := LabelOwnedNetworkPolicyDeployment(d, owner, identity); err == nil {
		t.Fatal("foreign deployment labeled")
	}
	after, _ = json.Marshal(d)
	if string(before) != string(after) {
		t.Fatal("foreign deployment mutated")
	}
}

func TestNetworkPolicyStatusErrors(t *testing.T) {
	reconcileErr, statusErr := errors.New("reconcile"), errors.New("status")
	err := ReportNetworkPolicyResult(reconcileErr, func(ready bool, cause error) error {
		if ready || !errors.Is(cause, reconcileErr) {
			t.Fatal("failure reported ready")
		}
		return statusErr
	})
	if !errors.Is(err, reconcileErr) || !errors.Is(err, statusErr) {
		t.Fatal("error swallowed")
	}
	if err := ReportNetworkPolicyResult(nil, func(ready bool, cause error) error {
		if !ready || cause != nil {
			t.Fatal("successful set reported failed")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

package utils

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNetworkPolicyName(t *testing.T) {
	name, err := NetworkPolicyName("some.workload/name")
	if err != nil {
		t.Fatalf("NetworkPolicyName() error = %v", err)
	}
	if len(name) > 63 {
		t.Fatalf("NetworkPolicyName() returned %q with length %d", name, len(name))
	}
	if !strings.HasPrefix(name, "some-workload-name-np-") {
		t.Errorf("NetworkPolicyName() = %q, want normalized workload prefix", name)
	}

	other, err := NetworkPolicyName("some.workload/name")
	if err != nil {
		t.Fatalf("NetworkPolicyName() second call error = %v", err)
	}
	if name != other {
		t.Errorf("NetworkPolicyName() is not deterministic: %q != %q", name, other)
	}

	collidingSlug, err := NetworkPolicyName("some-workload.name")
	if err != nil {
		t.Fatalf("NetworkPolicyName() colliding slug error = %v", err)
	}
	if name == collidingSlug {
		t.Errorf("different workload names produced the same policy name %q", name)
	}

	if _, err := NetworkPolicyName("  "); err == nil {
		t.Error("NetworkPolicyName() accepted an empty workload name")
	}
}

func TestNetworkPolicyOwnerLabels(t *testing.T) {
	owner := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name:      "example",
		Namespace: "trustyai",
		UID:       types.UID("owner-uid"),
	}}

	labels, err := NetworkPolicyOwnerLabels(owner)
	if err != nil {
		t.Fatalf("NetworkPolicyOwnerLabels() error = %v", err)
	}
	if labels[NetworkPolicyManagedByLabel] != NetworkPolicyManagedByValue {
		t.Errorf("managed-by label = %q, want %q", labels[NetworkPolicyManagedByLabel], NetworkPolicyManagedByValue)
	}
	if labels[NetworkPolicyOwnerUIDLabel] != string(owner.UID) {
		t.Errorf("owner UID label = %q, want %q", labels[NetworkPolicyOwnerUIDLabel], owner.UID)
	}

	owner.UID = ""
	if _, err := NetworkPolicyOwnerLabels(owner); err == nil {
		t.Error("NetworkPolicyOwnerLabels() accepted an owner without a UID")
	}
}

func TestSetNetworkPolicyOwnerReference(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("register core API types: %v", err)
	}
	owner := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name:      "example",
		Namespace: "trustyai",
		UID:       types.UID("owner-uid"),
	}}
	policy := testNetworkPolicy()

	if err := SetNetworkPolicyOwnerReference(policy, owner, scheme); err != nil {
		t.Fatalf("SetNetworkPolicyOwnerReference() error = %v", err)
	}
	if len(policy.OwnerReferences) != 1 || policy.OwnerReferences[0].UID != owner.UID || policy.OwnerReferences[0].Controller == nil || !*policy.OwnerReferences[0].Controller {
		t.Errorf("unexpected owner references: %#v", policy.OwnerReferences)
	}

	policy.Namespace = "another-namespace"
	if err := SetNetworkPolicyOwnerReference(policy, owner, scheme); err == nil {
		t.Error("SetNetworkPolicyOwnerReference() accepted a cross-namespace owner")
	}
}

func TestValidateNetworkPolicy(t *testing.T) {
	protocol := corev1.ProtocolTCP
	port := intstr.FromInt32(8443)
	tests := []struct {
		name    string
		mutate  func(*networkingv1.NetworkPolicy)
		wantErr string
	}{
		{
			name: "empty target selector",
			mutate: func(policy *networkingv1.NetworkPolicy) {
				policy.Spec.PodSelector = metav1.LabelSelector{}
			},
			wantErr: "non-empty pod selector",
		},
		{
			name: "rule without a peer",
			mutate: func(policy *networkingv1.NetworkPolicy) {
				policy.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{{
					Ports: []networkingv1.NetworkPolicyPort{{Protocol: &protocol, Port: &port}},
				}}
			},
			wantErr: "has no peers",
		},
		{
			name: "rule without a port",
			mutate: func(policy *networkingv1.NetworkPolicy) {
				policy.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{{
					From: []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "caller"}}}},
				}}
			},
			wantErr: "has no ports",
		},
		{
			name: "unrestricted IPBlock",
			mutate: func(policy *networkingv1.NetworkPolicy) {
				policy.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{{
					From:  []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: "0.0.0.0/0"}}},
					Ports: []networkingv1.NetworkPolicyPort{{Protocol: &protocol, Port: &port}},
				}}
			},
			wantErr: "is unrestricted",
		},
		{
			name: "empty namespace selector",
			mutate: func(policy *networkingv1.NetworkPolicy) {
				policy.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{{
					From:  []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{}}},
					Ports: []networkingv1.NetworkPolicyPort{{Protocol: &protocol, Port: &port}},
				}}
			},
			wantErr: "matches every namespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := testNetworkPolicy()
			tt.mutate(policy)
			err := ValidateNetworkPolicy(policy)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateNetworkPolicy() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}

	if err := ValidateNetworkPolicy(testNetworkPolicy()); err != nil {
		t.Errorf("ValidateNetworkPolicy() rejected a valid ingress-deny policy: %v", err)
	}
}

func TestValidateNetworkPolicyPeerIPBlockExceptions(t *testing.T) {
	tests := []struct {
		name       string
		cidr       string
		exceptions []string
		wantErr    string
	}{
		{
			name:       "masked exception is contained",
			cidr:       "192.168.1.7/24",
			exceptions: []string{"192.168.1.130/25"},
		},
		{
			name:       "malformed exception",
			cidr:       "192.168.1.0/24",
			exceptions: []string{"not-a-prefix"},
			wantErr:    "invalid IPBlock exception CIDR",
		},
		{
			name:       "exception outside parent",
			cidr:       "192.168.1.0/24",
			exceptions: []string{"192.168.2.0/24"},
			wantErr:    "not contained within parent CIDR",
		},
		{
			name:       "exception overlaps parent but is broader",
			cidr:       "10.1.2.0/24",
			exceptions: []string{"10.1.2.0/23"},
			wantErr:    "not contained within parent CIDR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateNetworkPolicyPeer(networkingv1.NetworkPolicyPeer{
				IPBlock: &networkingv1.IPBlock{CIDR: tt.cidr, Except: tt.exceptions},
			})
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateNetworkPolicyPeer() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateNetworkPolicyPeer() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateNetworkPolicyNamedPorts(t *testing.T) {
	protocol := corev1.ProtocolTCP
	for _, tt := range []struct {
		name    string
		port    string
		wantErr bool
	}{
		{name: "valid Kubernetes port name", port: "http-metrics"},
		{name: "name longer than Kubernetes limit", port: "portname123456789", wantErr: true},
		{name: "name without a letter", port: "12345", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			port := intstr.FromString(tt.port)
			err := validateNetworkPolicyPorts([]networkingv1.NetworkPolicyPort{{Protocol: &protocol, Port: &port}}, "ingress rule 0")
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "ingress rule 0 port 0 has an invalid named port:") {
					t.Fatalf("validateNetworkPolicyPorts() error = %v, want invalid named port error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateNetworkPolicyPorts() error = %v", err)
			}
		})
	}
}

func TestReconcileNetworkPolicy(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatalf("register networking API types: %v", err)
	}

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	desired := testNetworkPolicy()
	if err := ReconcileNetworkPolicy(context.Background(), c, desired); err != nil {
		t.Fatalf("create NetworkPolicy: %v", err)
	}

	key := client.ObjectKeyFromObject(desired)
	actual := &networkingv1.NetworkPolicy{}
	if err := c.Get(context.Background(), key, actual); err != nil {
		t.Fatalf("get created NetworkPolicy: %v", err)
	}
	if actual.Spec.PolicyTypes[0] != networkingv1.PolicyTypeIngress {
		t.Errorf("created policyTypes = %v, want [Ingress]", actual.Spec.PolicyTypes)
	}

	protocol := corev1.ProtocolTCP
	port := intstr.FromInt32(8443)
	desired.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{{
		From:  []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "caller"}}}},
		Ports: []networkingv1.NetworkPolicyPort{{Protocol: &protocol, Port: &port}},
	}}
	desired.Labels = map[string]string{NetworkPolicyManagedByLabel: NetworkPolicyManagedByValue}
	desired.Annotations = map[string]string{"example.com/purpose": "reconciled"}
	if err := ReconcileNetworkPolicy(context.Background(), c, desired); err != nil {
		t.Fatalf("update drifted NetworkPolicy: %v", err)
	}

	actual = &networkingv1.NetworkPolicy{}
	if err := c.Get(context.Background(), key, actual); err != nil {
		t.Fatalf("get updated NetworkPolicy: %v", err)
	}
	if len(actual.Spec.Ingress) != 1 || actual.Spec.Ingress[0].Ports[0].Port.IntVal != 8443 {
		t.Errorf("updated ingress rules = %#v, want TCP 8443 rule", actual.Spec.Ingress)
	}
	if actual.Labels[NetworkPolicyManagedByLabel] != NetworkPolicyManagedByValue {
		t.Errorf("updated labels = %#v", actual.Labels)
	}
	if actual.Annotations["example.com/purpose"] != "reconciled" {
		t.Errorf("updated annotations = %#v", actual.Annotations)
	}

	if err := ReconcileNetworkPolicy(context.Background(), c, desired); err != nil {
		t.Fatalf("reconcile already-current NetworkPolicy: %v", err)
	}
}

func testNetworkPolicy() *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "example-np", Namespace: "trustyai"},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app": "example"}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
		},
	}
}

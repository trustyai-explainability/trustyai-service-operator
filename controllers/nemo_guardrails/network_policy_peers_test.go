package nemo_guardrails

import (
	nemov1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func TestNemoGatewayAndAdditionalPeers(t *testing.T) {
	owner := testNemoNetworkPolicyOwner()
	disabled := false
	owner.Spec.ExposeRoute = &disabled
	owner.Spec.NetworkPolicy = &nemov1.NemoNetworkPolicy{AdditionalIngress: []nemov1.NemoIngressPeer{{
		NamespaceSelector: metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "custom-gateway"}},
		PodSelector:       metav1.LabelSelector{MatchLabels: map[string]string{"app": "client"}},
	}}}
	desired, err := buildNemoGuardrailsNetworkPolicy(owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(desired.Policy.Spec.Ingress) != 2 {
		t.Fatal("expected gateway and custom peer without Route")
	}
	gateway := desired.Policy.Spec.Ingress[0].From[0]
	if gateway.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] != "openshift-ingress" || gateway.PodSelector.MatchLabels["gateway.istio.io/managed"] != "istio.io-gateway-controller" {
		t.Fatalf("unexpected gateway peer: %#v", gateway)
	}
	custom := desired.Policy.Spec.Ingress[1]
	if len(custom.From) != 1 || custom.From[0].PodSelector == nil || custom.From[0].NamespaceSelector == nil {
		t.Fatal("selectors must be ANDed in one peer")
	}
	assertNemoNetworkPolicyPorts(t, custom, nemoServerPort)
	owner.Spec.NetworkPolicy.AdditionalIngress[0].PodSelector = metav1.LabelSelector{}
	if _, err := buildNemoGuardrailsNetworkPolicy(owner); err == nil {
		t.Fatal("empty Pod selector must fail closed")
	}
}

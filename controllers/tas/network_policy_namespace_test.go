package tas

import "testing"

func TestTASNetworkPolicyRequiresApplicationsNamespace(t *testing.T) {
	if _, err := buildTASNetworkPolicy(testTASNetworkPolicyOwner(), "", false); err == nil {
		t.Fatal("policy accepted an empty applications namespace")
	}
}

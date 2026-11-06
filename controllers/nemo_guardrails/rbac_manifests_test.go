package nemo_guardrails

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

const (
	nemoManagerRoleName   = "nemo-guardrails-manager-role"
	nemoManagerRolePrefix = "trustyai-service-operator-"
	nemoPolicyNameSetFile = "nemo_network_policy.rego"
)

// TestNemoNetworkPolicyRBACIsMinimalAndSynchronized pins the NetworkPolicy
// permissions this component declares and keeps the standalone manifest and its
// module mirror equal. The verbs are the full set on purpose: the operand policy
// lives in the workload namespace, so cross-namespace cleanup needs delete.
func TestNemoNetworkPolicyRBACIsMinimalAndSynchronized(t *testing.T) {
	wantVerbs := []string{"create", "delete", "get", "list", "patch", "update", "watch"}
	sort.Strings(wantVerbs)
	for _, path := range nemoManagerRolePaths(t) {
		role := readNemoManagerRole(t, path)
		var groups, resources, verbs []string
		for _, rule := range role.Rules {
			if !containsString(rule.APIGroups, "networking.k8s.io") {
				continue
			}
			groups = append(groups, rule.APIGroups...)
			resources = append(resources, rule.Resources...)
			verbs = append(verbs, rule.Verbs...)
		}
		sort.Strings(groups)
		sort.Strings(resources)
		sort.Strings(verbs)
		if strings.Join(groups, ",") != "networking.k8s.io" {
			t.Fatalf("%s NetworkPolicy rule apiGroups = %v, want only networking.k8s.io", path, groups)
		}
		if strings.Join(resources, ",") != "networkpolicies" {
			t.Fatalf("%s NetworkPolicy rule resources = %v, want only networkpolicies", path, resources)
		}
		if strings.Join(verbs, ",") != strings.Join(wantVerbs, ",") {
			t.Fatalf("%s NetworkPolicy rule verbs = %v, want %v", path, verbs, wantVerbs)
		}
	}
}

// TestNemoManagerRoleNameIsCoveredByPolicyNameSet keeps the policy name set in
// step with the ClusterRole it guards. Overlays apply namePrefix, so the rendered
// name differs from the component manifest name and both have to be listed.
func TestNemoManagerRoleNameIsCoveredByPolicyNameSet(t *testing.T) {
	moduleRoot := nemoModuleRoot(t)
	for _, path := range nemoManagerRolePaths(t) {
		role := readNemoManagerRole(t, path)
		if role.Name != nemoManagerRoleName {
			t.Fatalf("%s ClusterRole name = %q, want %q", path, role.Name, nemoManagerRoleName)
		}
	}
	policyPath := filepath.Join(moduleRoot, "policy", nemoPolicyNameSetFile)
	raw, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatalf("read %s: %v", policyPath, err)
	}
	policy := string(raw)
	for _, name := range []string{nemoManagerRoleName, nemoManagerRolePrefix + nemoManagerRoleName} {
		if !strings.Contains(policy, `"`+name+`"`) {
			t.Fatalf("%s does not list the ClusterRole name %q: the online guard cannot fire for it", policyPath, name)
		}
	}
}

func nemoModuleRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
}

func nemoManagerRolePaths(t *testing.T) []string {
	t.Helper()
	moduleRoot := nemoModuleRoot(t)
	return []string{
		filepath.Join(moduleRoot, "config", "components", "nemo-guardrails", "rbac", "manager-rbac.yaml"),
		filepath.Join(moduleRoot, "trustyai-operator-module", "config", "manifests-template", "components", "nemo-guardrails", "rbac", "manager-rbac.yaml"),
	}
}

func readNemoManagerRole(t *testing.T, path string) rbacv1.ClusterRole {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var role rbacv1.ClusterRole
	if err := yaml.Unmarshal(raw, &role); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	if role.Kind != "ClusterRole" {
		t.Fatalf("%s kind = %q, want ClusterRole", path, role.Kind)
	}
	return role
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

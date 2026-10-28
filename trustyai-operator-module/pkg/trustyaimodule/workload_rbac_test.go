package trustyaimodule

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// The module must hold each permission before delegating it to the TAS manager.
// Keep this test scoped to the NetworkPolicy and ownership-verification grants.
func TestModuleRoleDelegatesTASNetworkPolicyPermissions(t *testing.T) {
	readRole := func(path string) rbacv1.ClusterRole {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var role rbacv1.ClusterRole
		if err := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096).Decode(&role); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		if role.Kind != "ClusterRole" || role.Name == "" || len(role.Rules) == 0 {
			t.Fatalf("%s is not a populated ClusterRole", path)
		}
		return role
	}
	module := readRole(filepath.Join("..", "..", "config", "rbac", "role.yaml"))
	tas := readRole(filepath.Join("..", "..", "config", "manifests-template", "components", "tas", "rbac", "manager-rbac.yaml"))
	contains := func(values []string, want string) bool {
		for _, value := range values {
			if value == want {
				return true
			}
		}
		return false
	}
	for _, permission := range []struct {
		group, resource string
		verbs           []string
	}{
		{"apps", "replicasets", []string{"get"}},
		{"networking.k8s.io", "networkpolicies", []string{"get", "list", "watch", "create", "update"}},
	} {
		for _, role := range []rbacv1.ClusterRole{tas, module} {
			for _, verb := range permission.verbs {
				covered := false
				for _, rule := range role.Rules {
					if len(rule.ResourceNames) == 0 && contains(rule.APIGroups, permission.group) &&
						contains(rule.Resources, permission.resource) && contains(rule.Verbs, verb) {
						covered = true
					}
				}
				if !covered {
					t.Errorf("%s lacks %s %s/%s", role.Name, verb, permission.group, permission.resource)
				}
			}
		}
	}
}

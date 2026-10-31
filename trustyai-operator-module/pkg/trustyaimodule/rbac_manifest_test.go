package trustyaimodule

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestManagerRoleIncludesNamespaceMutationPermissions(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine the test source path")
	}

	roleFile, err := os.Open(filepath.Join(filepath.Dir(sourceFile), "..", "..", "config", "rbac", "role.yaml"))
	if err != nil {
		t.Fatalf("open generated manager role: %v", err)
	}
	defer func() { _ = roleFile.Close() }()

	var role rbacv1.ClusterRole
	if err := yaml.NewYAMLOrJSONDecoder(roleFile, 4096).Decode(&role); err != nil {
		t.Fatalf("decode generated manager role: %v", err)
	}

	verbs := make(map[string]struct{})
	for _, rule := range role.Rules {
		if !containsString(rule.APIGroups, "") || !containsString(rule.Resources, "namespaces") {
			continue
		}
		for _, verb := range rule.Verbs {
			verbs[verb] = struct{}{}
		}
	}

	if len(verbs) == 0 {
		t.Fatal("generated manager role has no namespace rule")
	}

	for _, requiredVerb := range []string{"get", "list", "watch", "update", "patch"} {
		if _, ok := verbs[requiredVerb]; !ok {
			t.Errorf("manager role namespace rules are missing %q permission: %#v", requiredVerb, verbs)
		}
	}
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

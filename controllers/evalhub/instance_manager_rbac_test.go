package evalhub

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

func TestEvalHubInstanceManagerRBAC(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	want := []rbacv1.PolicyRule{{
		APIGroups: []string{"trustyai.opendatahub.io"},
		Resources: []string{"evalhubs"},
		Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
	}}
	for _, prefix := range []string{"config/components", "trustyai-operator-module/config/manifests-template/components"} {
		dir := filepath.Join(root, prefix, "evalhub")
		raw, err := os.ReadFile(filepath.Join(dir, "rbac/evalhub_instance_manager_role.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		var role rbacv1.ClusterRole
		if err := yaml.Unmarshal(raw, &role); err != nil {
			t.Fatal(err)
		}
		if role.Kind != "ClusterRole" || role.Name != "evalhub-instance-manager" || !reflect.DeepEqual(role.Rules, want) {
			t.Fatalf("unexpected instance manager in %s: %+v", prefix, role)
		}
		if !reflect.DeepEqual(role.Labels, map[string]string{
			"rbac.authorization.k8s.io/aggregate-to-edit":  "true",
			"rbac.authorization.k8s.io/aggregate-to-admin": "true",
		}) {
			t.Fatalf("unexpected project role aggregation: %v", role.Labels)
		}
		kustomization, err := os.ReadFile(filepath.Join(dir, "kustomization.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(kustomization), "- rbac/evalhub_instance_manager_role.yaml") {
			t.Fatalf("role not packaged in %s", prefix)
		}
	}
}

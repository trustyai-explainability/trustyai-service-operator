package evalhub

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

func TestEvalHubJobConfigClusterRoleVerbsAreMinimalAndSufficient(t *testing.T) {
	// EvalHub sets ConfigMap ownerReferences after creating the Job:
	// it does a Get+Update on the ConfigMap (see eval-hub/internal/runtimes/k8s/k8s_helper.go:SetConfigMapOwner).
	// Therefore the job-config ClusterRole must include: create,get,update,delete.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	// This test file lives at trustyai-service-operator/controllers/evalhub/.
	// The manifest lives under trustyai-service-operator/config/rbac/evalhub/.
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	manifestPath := filepath.Join(moduleRoot, "config", "rbac", "evalhub", "evalhub_job_config_role.yaml")

	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read %s: %v", manifestPath, err)
	}

	var cr rbacv1.ClusterRole
	if err := yaml.Unmarshal(raw, &cr); err != nil {
		t.Fatalf("unmarshal %s: %v", manifestPath, err)
	}

	var gotVerbs []string
	for _, rule := range cr.Rules {
		for _, res := range rule.Resources {
			if res == "configmaps" {
				gotVerbs = append([]string(nil), rule.Verbs...)
				break
			}
		}
		if len(gotVerbs) > 0 {
			break
		}
	}
	if len(gotVerbs) == 0 {
		t.Fatalf("expected %s to contain a policy rule for resource 'configmaps'", manifestPath)
	}

	wantVerbs := []string{"create", "delete", "get", "update"}
	sort.Strings(gotVerbs)
	sort.Strings(wantVerbs)

	if len(gotVerbs) != len(wantVerbs) {
		t.Fatalf("unexpected verbs for configmaps: got=%v want=%v", gotVerbs, wantVerbs)
	}
	for i := range wantVerbs {
		if gotVerbs[i] != wantVerbs[i] {
			t.Fatalf("unexpected verbs for configmaps: got=%v want=%v", gotVerbs, wantVerbs)
		}
	}
}

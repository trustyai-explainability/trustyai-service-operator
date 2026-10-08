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

func TestEvalHubNetworkPolicyRBACIsMinimalAndSynchronized(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	manifestPaths := []string{
		filepath.Join(moduleRoot, "config", "components", "evalhub", "rbac", "manager-rbac.yaml"),
		filepath.Join(moduleRoot, "trustyai-operator-module", "config", "manifests-template", "components", "evalhub", "rbac", "manager-rbac.yaml"),
	}
	wantVerbs := []string{"create", "get", "list", "update", "watch"}
	sort.Strings(wantVerbs)
	for _, path := range manifestPaths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var role rbacv1.ClusterRole
		if err := yaml.Unmarshal(raw, &role); err != nil {
			t.Fatalf("unmarshal %s: %v", path, err)
		}
		var gotVerbs []string
		for _, rule := range role.Rules {
			if len(rule.APIGroups) != 1 || rule.APIGroups[0] != "networking.k8s.io" {
				continue
			}
			for _, resource := range rule.Resources {
				if resource == "networkpolicies" {
					gotVerbs = append(gotVerbs, rule.Verbs...)
				}
			}
		}
		sort.Strings(gotVerbs)
		if len(gotVerbs) != len(wantVerbs) {
			t.Fatalf("%s networkpolicy verbs = %v, want %v", path, gotVerbs, wantVerbs)
		}
		for i := range wantVerbs {
			if gotVerbs[i] != wantVerbs[i] {
				t.Fatalf("%s networkpolicy verbs = %v, want %v", path, gotVerbs, wantVerbs)
			}
		}
	}
}

func TestEvalHubHardwareProfilesReaderClusterRoleVerbsAreMinimalAndSufficient(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	manifestPath := filepath.Join(moduleRoot, "config", "components", "evalhub", "rbac", "evalhub_hardware_profiles_reader_role.yaml")

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
			if res == "hardwareprofiles" {
				gotVerbs = append([]string(nil), rule.Verbs...)
				break
			}
		}
		if len(gotVerbs) > 0 {
			break
		}
	}
	if len(gotVerbs) == 0 {
		t.Fatalf("expected %s to contain a policy rule for resource 'hardwareprofiles'", manifestPath)
	}

	wantVerbs := []string{"get", "list"}
	sort.Strings(gotVerbs)
	sort.Strings(wantVerbs)

	if len(gotVerbs) != len(wantVerbs) {
		t.Fatalf("unexpected verbs for hardwareprofiles: got=%v want=%v", gotVerbs, wantVerbs)
	}
	for i := range wantVerbs {
		if gotVerbs[i] != wantVerbs[i] {
			t.Fatalf("unexpected verbs for hardwareprofiles: got=%v want=%v", gotVerbs, wantVerbs)
		}
	}
}

func TestEvalHubJobConfigClusterRoleVerbsAreMinimalAndSufficient(t *testing.T) {
	// EvalHub sets ConfigMap ownerReferences after creating the Job:
	// it does a Get+Update on the ConfigMap (see eval-hub/internal/runtimes/k8s/k8s_helper.go:SetConfigMapOwner).
	// Therefore the job-config ClusterRole must include: create,get,list,update,delete.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	// This test file lives at trustyai-service-operator/controllers/evalhub/.
	// The manifest lives under trustyai-service-operator/config/components/evalhub/rbac/.
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	manifestPath := filepath.Join(moduleRoot, "config", "components", "evalhub", "rbac", "evalhub_job_config_role.yaml")

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

	wantVerbs := []string{"create", "delete", "get", "list", "update"}
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

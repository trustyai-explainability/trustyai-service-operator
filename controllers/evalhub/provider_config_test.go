package evalhub

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

// atifProviderConfig is the subset of eval-hub's provider schema that
// hack/sync-evalhub-providers.sh has to carry into the operator ConfigMap:
// the HLR-1 input joining precedence declarations (RHAI-22311) and the
// adapter's kustomize image placeholder.
type atifProviderConfig struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Agent struct {
		TargetType string   `json:"target_type"`
		Hints      []string `json:"hints"`
	} `json:"agent"`
	Runtime struct {
		K8s struct {
			Image string `json:"image"`
		} `json:"k8s"`
	} `json:"runtime"`
	Benchmarks []struct {
		Metrics []string `json:"metrics"`
	} `json:"benchmarks"`
	Parameters []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"parameters"`
}

// TestATIFProviderConfigDeclaresManifestPrecedence pins the ATIF provider
// ConfigMap to the precedence rule declared upstream in
// eval-hub/config/providers/atif.yaml: a producer's top-level manifest.jsonl
// is authoritative, recursive discovery is the no-manifest fallback. The
// regeneration is a script, so an upstream sync that silently drops the rule
// would otherwise only surface on a cluster.
//
// Both copies are checked because the module image renders the second one:
// config/ is synced into trustyai-operator-module/config/manifests-template,
// and a regeneration that touches one tree leaves the install paths divergent.
func TestATIFProviderConfigDeclaresManifestPrecedence(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	configMapPaths := []string{
		filepath.Join(moduleRoot, "config", "configmaps", "evalhub", "provider-atif.yaml"),
		filepath.Join(moduleRoot, "trustyai-operator-module", "config", "manifests-template", "configmaps", "evalhub", "provider-atif.yaml"),
	}

	raw := make([][]byte, len(configMapPaths))
	for i, path := range configMapPaths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		raw[i] = content
	}
	if string(raw[0]) != string(raw[1]) {
		t.Errorf("%s and %s differ: the module manifests template must mirror config/", configMapPaths[0], configMapPaths[1])
	}

	for i, path := range configMapPaths {
		var configMap corev1.ConfigMap
		if err := yaml.Unmarshal(raw[i], &configMap); err != nil {
			t.Fatalf("unmarshal %s: %v", path, err)
		}
		inner, ok := configMap.Data["atif.yaml"]
		if !ok {
			t.Fatalf("%s: no atif.yaml key in ConfigMap data", path)
		}

		var config atifProviderConfig
		if err := yaml.Unmarshal([]byte(inner), &config); err != nil {
			t.Fatalf("unmarshal %s atif.yaml: %v", path, err)
		}

		if config.ID != "atif" || config.Type != "community" {
			t.Errorf("%s: provider id/type = %q/%q, want atif/community", path, config.ID, config.Type)
		}
		if config.Agent.TargetType != "agent" {
			t.Errorf("%s: agent.target_type = %q, want agent", path, config.Agent.TargetType)
		}
		if !anyContains(config.Agent.Hints, "manifest.jsonl") {
			t.Errorf("%s: agent hints do not mention the manifest precedence rule: %v", path, config.Agent.Hints)
		}

		// The kustomize variable is substituted at render time; a concrete image
		// here means the sync script overwrote the operator's parameterisation.
		if want := "$(evalhub-provider-atif-image)"; config.Runtime.K8s.Image != want {
			t.Errorf("%s: runtime.k8s.image = %q, want %q", path, config.Runtime.K8s.Image, want)
		}

		metrics := map[string]bool{}
		for _, benchmark := range config.Benchmarks {
			for _, metric := range benchmark.Metrics {
				metrics[metric] = true
			}
		}
		for _, want := range []string{"atif_overall_score", "atif_failure_categorization_rate", "atif_trajectory_count", "atif_manifest_entry_count"} {
			if !metrics[want] {
				t.Errorf("%s: benchmark metrics do not declare %q", path, want)
			}
		}

		parameters := map[string]string{}
		for _, parameter := range config.Parameters {
			parameters[parameter.Name] = parameter.Description
		}
		for name, wantSubstring := range map[string]string{
			"trajectory_path":       "manifest.jsonl",
			"invalid_input_policy":  "Manifest rows follow the same policy",
			"partial_result_policy": "SHA-256",
		} {
			if !strings.Contains(parameters[name], wantSubstring) {
				t.Errorf("%s: %s description does not mention %q: %q", path, name, wantSubstring, parameters[name])
			}
		}
	}
}

func anyContains(values []string, substring string) bool {
	for _, value := range values {
		if strings.Contains(value, substring) {
			return true
		}
	}
	return false
}

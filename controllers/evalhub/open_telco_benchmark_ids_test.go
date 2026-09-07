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

var openTelcoBenchmarkIDs = []string{
	"telemath",
	"teleqna",
	"telelogs",
	"3gpp-tsg",
}

func moduleRoot(t *testing.T) string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
}

func readConfigMapData(t *testing.T, relPath string) map[string]string {
	path := filepath.Join(moduleRoot(t), relPath)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var cm corev1.ConfigMap
	if err := yaml.Unmarshal(raw, &cm); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	return cm.Data
}

func TestOpenTelcoCollectionBenchmarkIDsAreK8sLabelSafe(t *testing.T) {
	data := readConfigMapData(t, filepath.Join("config", "configmaps", "evalhub", "collection-open-telco-v1.yaml"))
	raw, ok := data["open-telco-v1.yaml"]
	if !ok {
		t.Fatal("collection ConfigMap missing open-telco-v1.yaml key")
	}

	var collection struct {
		Benchmarks []struct {
			ID string `yaml:"id"`
		} `yaml:"benchmarks"`
	}
	if err := yaml.Unmarshal([]byte(raw), &collection); err != nil {
		t.Fatalf("unmarshal open-telco-v1 collection: %v", err)
	}

	got := make([]string, 0, len(collection.Benchmarks))
	for _, b := range collection.Benchmarks {
		got = append(got, b.ID)
		if strings.Contains(b.ID, "/") {
			t.Fatalf("benchmark id %q contains '/' and is unsafe for Kubernetes label values", b.ID)
		}
	}

	if len(got) != len(openTelcoBenchmarkIDs) {
		t.Fatalf("unexpected benchmark count: got=%v want=%v", got, openTelcoBenchmarkIDs)
	}
	for i, want := range openTelcoBenchmarkIDs {
		if got[i] != want {
			t.Fatalf("benchmark[%d]: got %q want %q", i, got[i], want)
		}
	}
}

func TestOpenTelcoProviderBenchmarkIDsMatchCollection(t *testing.T) {
	data := readConfigMapData(t, filepath.Join("config", "configmaps", "evalhub", "provider-inspect.yaml"))
	raw, ok := data["inspect.yaml"]
	if !ok {
		t.Fatal("provider ConfigMap missing inspect.yaml key")
	}

	var provider struct {
		Benchmarks []struct {
			ID string `yaml:"id"`
		} `yaml:"benchmarks"`
	}
	if err := yaml.Unmarshal([]byte(raw), &provider); err != nil {
		t.Fatalf("unmarshal provider-inspect provider.yaml: %v", err)
	}

	providerIDs := make(map[string]bool, len(provider.Benchmarks))
	for _, b := range provider.Benchmarks {
		providerIDs[b.ID] = true
	}

	for _, id := range openTelcoBenchmarkIDs {
		if !providerIDs[id] {
			t.Fatalf("provider-inspect missing benchmark id %q", id)
		}
	}
}

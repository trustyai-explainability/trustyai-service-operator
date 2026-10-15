package evalhub

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	evalhubv1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"
)

type packagedCollectionConfig struct {
	ID            string `json:"id"`
	CurationOrder int32  `json:"curation_order"`
	Benchmarks    []struct {
		ID         string                 `json:"id"`
		ProviderID string                 `json:"provider_id"`
		Weight     float64                `json:"weight"`
		Parameters map[string]interface{} `json:"parameters"`
	} `json:"benchmarks"`
}

type packagedProviderConfig struct {
	ID      string `json:"id"`
	Runtime struct {
		K8s struct {
			Image string `json:"image"`
		} `json:"k8s"`
	} `json:"runtime"`
	Benchmarks []struct {
		ID          string `json:"id"`
		Description string `json:"description"`
	} `json:"benchmarks"`
	Parameters []struct {
		Name        string      `json:"name"`
		Type        interface{} `json:"type"`
		Default     interface{} `json:"default"`
		Description string      `json:"description"`
	} `json:"parameters"`
}

func readPackagedConfigMap(t *testing.T, relativePath string) *corev1.ConfigMap {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), relativePath))
	if err != nil {
		t.Fatalf("read ConfigMap manifest %s: %v", relativePath, err)
	}
	var configMap corev1.ConfigMap
	if err := yaml.Unmarshal(raw, &configMap); err != nil {
		t.Fatalf("unmarshal ConfigMap manifest %s: %v", relativePath, err)
	}
	return &configMap
}

func unmarshalCollectionConfig(t *testing.T, configMap *corev1.ConfigMap, dataKey string) packagedCollectionConfig {
	t.Helper()
	content, ok := configMap.Data[dataKey]
	if !ok {
		t.Fatalf("ConfigMap %s missing data key %q", configMap.Name, dataKey)
	}
	var collection packagedCollectionConfig
	if err := yaml.Unmarshal([]byte(content), &collection); err != nil {
		t.Fatalf("unmarshal collection config %s: %v", dataKey, err)
	}
	return collection
}

func unmarshalProviderConfig(t *testing.T, configMap *corev1.ConfigMap, dataKey string) packagedProviderConfig {
	t.Helper()
	content, ok := configMap.Data[dataKey]
	if !ok {
		t.Fatalf("ConfigMap %s missing data key %q", configMap.Name, dataKey)
	}
	var provider packagedProviderConfig
	if err := yaml.Unmarshal([]byte(content), &provider); err != nil {
		t.Fatalf("unmarshal provider config %s: %v", dataKey, err)
	}
	return provider
}

func TestPackagedJudgeCollectionConfigs(t *testing.T) {
	tests := []struct {
		name               string
		collectionID       string
		benchmarkID        string
		providerID         string
		benchmarkWeights   map[string]float64
		forbiddenParamKeys []string
	}{
		{
			name:         "knowledge reasoning HLE",
			collectionID: "knowledge-reasoning-v1",
			benchmarkID:  "inspect/hle",
			providerID:   "inspect",
			benchmarkWeights: map[string]float64{
				"gpqa:diamond": 1.0 / 3,
				"inspect/hle":  1.0 / 3,
				"math_500":     1.0 / 3,
			},
		},
		{
			name:         "instruction output FollowBench",
			collectionID: "instruction-output-v1",
			benchmarkID:  "followbench",
			providerID:   "followbench",
			benchmarkWeights: map[string]float64{
				"ifbench":               1.0 / 3,
				"jsonschema_bench_easy": 1.0 / 3,
				"followbench":           1.0 / 3,
			},
		},
		{
			name:         "trustworthiness CyberSecEval 2",
			collectionID: "trustworthiness-v1",
			benchmarkID:  "inspect/cyberseceval-2-pi",
			providerID:   "inspect",
			benchmarkWeights: map[string]float64{
				"inspect/cyberseceval-2-pi": 2,
				"truthfulqa_mc1":            1,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifestPath := filepath.Join("config", "configmaps", "evalhub", "collection-"+test.collectionID+".yaml")
			modulePath := filepath.Join("trustyai-operator-module", "config", "manifests-template", "configmaps", "evalhub", "collection-"+test.collectionID+".yaml")
			manifestBytes, err := os.ReadFile(filepath.Join(moduleRoot(t), manifestPath))
			if err != nil {
				t.Fatalf("read source ConfigMap manifest: %v", err)
			}
			moduleBytes, err := os.ReadFile(filepath.Join(moduleRoot(t), modulePath))
			if err != nil {
				t.Fatalf("read module ConfigMap manifest: %v", err)
			}
			if string(manifestBytes) != string(moduleBytes) {
				t.Fatalf("module ConfigMap %s is not synchronized with config source", test.collectionID)
			}

			configMap := readPackagedConfigMap(t, manifestPath)
			if configMap.Name != "evalhub-collection-"+test.collectionID {
				t.Fatalf("ConfigMap name = %q", configMap.Name)
			}
			if got := configMap.Labels[collectionLabel]; got != "system" {
				t.Fatalf("collection type label = %q, want system", got)
			}
			if got := configMap.Labels[collectionNameLabel]; got != test.collectionID {
				t.Fatalf("collection name label = %q, want %q", got, test.collectionID)
			}
			if len(configMap.Data) != 1 {
				t.Fatalf("ConfigMap data keys = %v, want the single embedded collection YAML", configMap.Data)
			}

			collection := unmarshalCollectionConfig(t, configMap, test.collectionID+".yaml")
			if collection.ID != test.collectionID {
				t.Fatalf("embedded collection id = %q, want %q", collection.ID, test.collectionID)
			}
			if collection.CurationOrder != 10 {
				t.Fatalf("curation_order = %d, want existing curated order 10", collection.CurationOrder)
			}
			if len(collection.Benchmarks) != len(test.benchmarkWeights) {
				t.Fatalf("benchmark count = %d, want %d", len(collection.Benchmarks), len(test.benchmarkWeights))
			}

			var judgeBenchmark *struct {
				ID         string                 `json:"id"`
				ProviderID string                 `json:"provider_id"`
				Weight     float64                `json:"weight"`
				Parameters map[string]interface{} `json:"parameters"`
			}
			found := make(map[string]bool, len(collection.Benchmarks))
			for i := range collection.Benchmarks {
				benchmark := &collection.Benchmarks[i]
				wantWeight, expected := test.benchmarkWeights[benchmark.ID]
				if !expected {
					t.Fatalf("unexpected benchmark %q", benchmark.ID)
				}
				if math.Abs(benchmark.Weight-wantWeight) > 1e-9 {
					t.Errorf("benchmark %q weight = %v, want %v", benchmark.ID, benchmark.Weight, wantWeight)
				}
				found[benchmark.ID] = true
				if benchmark.ID == test.benchmarkID {
					judgeBenchmark = benchmark
				}
			}
			for benchmarkID := range test.benchmarkWeights {
				if !found[benchmarkID] {
					t.Errorf("collection is missing benchmark %q", benchmarkID)
				}
			}
			if judgeBenchmark == nil {
				t.Fatalf("collection is missing judge benchmark %q", test.benchmarkID)
			}
			if judgeBenchmark.ProviderID != test.providerID {
				t.Errorf("judge benchmark provider_id = %q, want %q", judgeBenchmark.ProviderID, test.providerID)
			}
			judgeModel, ok := judgeBenchmark.Parameters["judge_model"]
			if !ok || judgeModel != "" {
				t.Errorf("judge_model = %#v, want an explicit empty string", judgeModel)
			}
			assertNoJudgeConnectionParameters(t, judgeBenchmark.Parameters)
		})
	}
}

func assertNoJudgeConnectionParameters(t *testing.T, value interface{}) {
	t.Helper()
	forbidden := map[string]bool{
		"judge_route":   true,
		"judge_url":     true,
		"judge_api_key": true,
		"judge_api-key": true,
	}
	switch current := value.(type) {
	case map[string]interface{}:
		for key, nested := range current {
			if forbidden[key] {
				t.Errorf("judge connection field %q must not be in collection parameters", key)
			}
			assertNoJudgeConnectionParameters(t, nested)
		}
	case []interface{}:
		for _, nested := range current {
			assertNoJudgeConnectionParameters(t, nested)
		}
	}
}

func TestPackagedJudgeProviderConfigs(t *testing.T) {
	inspectMap := readPackagedConfigMap(t, filepath.Join("config", "configmaps", "evalhub", "provider-inspect.yaml"))
	followbenchMap := readPackagedConfigMap(t, filepath.Join("config", "configmaps", "evalhub", "provider-followbench.yaml"))
	for _, test := range []struct {
		configMap *corev1.ConfigMap
		provider  string
		key       string
	}{
		{configMap: inspectMap, provider: "inspect", key: "inspect.yaml"},
		{configMap: followbenchMap, provider: "followbench", key: "followbench.yaml"},
	} {
		modulePath := filepath.Join("trustyai-operator-module", "config", "manifests-template", "configmaps", "evalhub", "provider-"+test.provider+".yaml")
		manifestPath := filepath.Join("config", "configmaps", "evalhub", "provider-"+test.provider+".yaml")
		manifestBytes, err := os.ReadFile(filepath.Join(moduleRoot(t), manifestPath))
		if err != nil {
			t.Fatal(err)
		}
		moduleBytes, err := os.ReadFile(filepath.Join(moduleRoot(t), modulePath))
		if err != nil {
			t.Fatal(err)
		}
		if string(manifestBytes) != string(moduleBytes) {
			t.Errorf("module ConfigMap %s is not synchronized with config source", test.provider)
		}
		if test.configMap.Name != "evalhub-provider-"+test.provider {
			t.Errorf("provider ConfigMap name = %q", test.configMap.Name)
		}
		if got := test.configMap.Labels[providerLabel]; got != "system" {
			t.Errorf("provider type label = %q, want system", got)
		}
		if got := test.configMap.Labels[providerNameLabel]; got != test.provider {
			t.Errorf("provider name label = %q, want %q", got, test.provider)
		}
		if len(test.configMap.Data) != 1 {
			t.Fatalf("provider ConfigMap data keys = %v, want one embedded YAML", test.configMap.Data)
		}
	}

	inspect := unmarshalProviderConfig(t, inspectMap, "inspect.yaml")
	if inspect.ID != "inspect" {
		t.Fatalf("Inspect provider id = %q", inspect.ID)
	}
	if !strings.Contains(inspect.Runtime.K8s.Image, "$(evalhub-provider-inspect-image)") {
		t.Errorf("Inspect image = %q, want preserved kustomize image placeholder", inspect.Runtime.K8s.Image)
	}
	inspectBenchmarks := make(map[string]string, len(inspect.Benchmarks))
	for _, benchmark := range inspect.Benchmarks {
		inspectBenchmarks[benchmark.ID] = benchmark.Description
	}
	for _, benchmarkID := range []string{"inspect/hle", "inspect/cyberseceval-2-pi"} {
		description, ok := inspectBenchmarks[benchmarkID]
		if !ok {
			t.Errorf("Inspect provider missing benchmark %q", benchmarkID)
			continue
		}
		for _, term := range []string{"judge_model", "judge_api-key", "judge_url", "model.auth.secret_ref"} {
			if !strings.Contains(description, term) {
				t.Errorf("Inspect benchmark %q description does not mention %q", benchmarkID, term)
			}
		}
	}
	inspectJudgeParam := findProviderParameter(inspect, "judge_model")
	if inspectJudgeParam == nil || !strings.Contains(inspectJudgeParam.Description, "Petri/Bloom") || inspectJudgeParam.Default != "claude-opus-4-7" {
		t.Errorf("existing Inspect Petri/Bloom judge_model metadata was not preserved: %#v", inspectJudgeParam)
	}

	followbench := unmarshalProviderConfig(t, followbenchMap, "followbench.yaml")
	if followbench.ID != "followbench" {
		t.Fatalf("FollowBench provider id = %q", followbench.ID)
	}
	if !strings.Contains(followbench.Runtime.K8s.Image, "$(evalhub-provider-followbench-image)") {
		t.Errorf("FollowBench image = %q, want preserved kustomize image placeholder", followbench.Runtime.K8s.Image)
	}
	followbenchJudge := findProviderParameter(followbench, "judge_model")
	if followbenchJudge == nil || followbenchJudge.Type != "string" || !strings.Contains(followbenchJudge.Description, "bundled collection") {
		t.Errorf("FollowBench judge_model metadata is incorrect: %#v", followbenchJudge)
	}
	followbenchURL := findProviderParameter(followbench, "judge_url")
	if followbenchURL == nil || !strings.Contains(followbenchURL.Description, "Legacy endpoint override") || !strings.Contains(followbenchURL.Description, "model.auth.secret_ref") {
		t.Errorf("FollowBench legacy standalone judge_url metadata was not preserved: %#v", followbenchURL)
	}
}

func findProviderParameter(provider packagedProviderConfig, name string) *struct {
	Name        string      `json:"name"`
	Type        interface{} `json:"type"`
	Default     interface{} `json:"default"`
	Description string      `json:"description"`
} {
	for i := range provider.Parameters {
		if provider.Parameters[i].Name == name {
			return &provider.Parameters[i]
		}
	}
	return nil
}

func TestReconcilePackagedJudgeCollectionPreservesParametersAndCurationOverride(t *testing.T) {
	operatorNamespace := "operator-ns"
	instanceNamespace := "instance-ns"
	collectionMap := readPackagedConfigMap(t, filepath.Join("config", "configmaps", "evalhub", "collection-knowledge-reasoning-v1.yaml"))
	collectionMap.Namespace = operatorNamespace
	providerMap := readPackagedConfigMap(t, filepath.Join("config", "configmaps", "evalhub", "provider-inspect.yaml"))
	providerMap.Namespace = operatorNamespace
	order := int32(5)
	instance := &evalhubv1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{Name: "evalhub", Namespace: instanceNamespace},
		Spec: evalhubv1.EvalHubSpec{
			Collections: []string{"knowledge-reasoning-v1"},
			Providers:   []string{"inspect"},
			CollectionOverrides: []evalhubv1.SystemCollectionOverride{{
				Collection:    "knowledge-reasoning-v1",
				CurationOrder: &order,
			}},
		},
	}

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := evalhubv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(collectionMap, providerMap).Build()
	reconciler := &EvalHubReconciler{Client: client, Scheme: scheme, Namespace: operatorNamespace}
	if _, err := reconciler.reconcileCollectionConfigMaps(context.Background(), instance); err != nil {
		t.Fatalf("reconcileCollectionConfigMaps(): %v", err)
	}
	if _, err := reconciler.reconcileProviderConfigMaps(context.Background(), instance); err != nil {
		t.Fatalf("reconcileProviderConfigMaps(): %v", err)
	}

	var copiedProvider corev1.ConfigMap
	if err := client.Get(context.Background(), types.NamespacedName{Name: "evalhub-provider-inspect", Namespace: instanceNamespace}, &copiedProvider); err != nil {
		t.Fatalf("get copied provider ConfigMap: %v", err)
	}
	if copiedProvider.Data["inspect.yaml"] != providerMap.Data["inspect.yaml"] {
		t.Fatal("reconciliation did not preserve the packaged Inspect provider config")
	}

	var copiedCollection corev1.ConfigMap
	if err := client.Get(context.Background(), types.NamespacedName{Name: "evalhub-collection-knowledge-reasoning-v1", Namespace: instanceNamespace}, &copiedCollection); err != nil {
		t.Fatalf("get copied collection ConfigMap: %v", err)
	}
	collection := unmarshalCollectionConfig(t, &copiedCollection, "knowledge-reasoning-v1.yaml")
	if collection.CurationOrder != order {
		t.Errorf("copied collection curation_order = %d, want override %d", collection.CurationOrder, order)
	}
	for _, benchmark := range collection.Benchmarks {
		if benchmark.ID == "inspect/hle" {
			if got := benchmark.Parameters["judge_model"]; got != "" {
				t.Errorf("copied HLE judge_model = %#v, want preserved empty string", got)
			}
			return
		}
	}
	t.Fatal("copied collection is missing inspect/hle")
}

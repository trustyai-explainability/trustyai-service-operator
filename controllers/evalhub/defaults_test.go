package evalhub

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

// repoRoot returns the repository root based on this file's location.
func repoRoot() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..")
}

// configMapMeta is a minimal struct for extracting labels from ConfigMap YAML files.
type configMapMeta struct {
	Metadata struct {
		Labels map[string]string `json:"labels"`
	} `json:"metadata"`
}

// crdSchema is a minimal struct for navigating the generated EvalHub CRD YAML.
// We use map[string]interface{} for property values because different fields
// have defaults of different types (scalars, arrays, objects).
type crdSchema struct {
	Spec struct {
		Versions []struct {
			Schema struct {
				OpenAPIV3Schema struct {
					Properties map[string]struct {
						Properties map[string]map[string]interface{} `json:"properties"`
					} `json:"properties"`
				} `json:"openAPIV3Schema"`
			} `json:"schema"`
		} `json:"versions"`
	} `json:"spec"`
}

// TestCRDDefaultsMatchConfigMaps verifies that the provider and collection
// defaults declared in the EvalHub CRD stay in sync with the ConfigMap files
// shipped in config/configmaps/evalhub/.
//
// If this test fails it means someone added, removed, or renamed a provider or
// collection ConfigMap without updating the kubebuilder default marker in
// api/evalhub/v1alpha1/evalhub_types.go (and regenerating the CRD with
// `make manifests`).
func TestCRDDefaultsMatchConfigMaps(t *testing.T) {
	root := repoRoot()
	configDir := filepath.Join(root, "config", "configmaps", "evalhub")
	crdPath := filepath.Join(root, "config", "components", "evalhub", "crd", "trustyai.opendatahub.io_evalhubs.yaml")

	// --- Discover provider and collection names from ConfigMap files ---
	providerNames := extractNamesFromConfigMaps(t, configDir, "provider-*.yaml", providerNameLabel)
	collectionNames := extractNamesFromConfigMaps(t, configDir, "collection-*.yaml", collectionNameLabel)

	require.NotEmpty(t, providerNames, "no provider ConfigMaps found in %s", configDir)
	require.NotEmpty(t, collectionNames, "no collection ConfigMaps found in %s", configDir)

	// --- Extract defaults from the generated CRD ---
	crdProviders, crdCollections := extractCRDDefaults(t, crdPath)

	// --- Compare (order-insensitive) ---
	sort.Strings(providerNames)
	sort.Strings(collectionNames)
	sort.Strings(crdProviders)
	sort.Strings(crdCollections)

	assert.Equal(t, providerNames, crdProviders,
		"CRD default providers do not match provider ConfigMaps in %s.\n"+
			"Update the kubebuilder default marker on Providers in api/evalhub/v1alpha1/evalhub_types.go and run `make manifests`.", configDir)
	assert.Equal(t, collectionNames, crdCollections,
		"CRD default collections do not match collection ConfigMaps in %s.\n"+
			"Update the kubebuilder default marker on Collections in api/evalhub/v1alpha1/evalhub_types.go and run `make manifests`.", configDir)
}

// extractNamesFromConfigMaps globs for YAML files matching pattern in dir and
// returns the values of the given label from each file.
func extractNamesFromConfigMaps(t *testing.T, dir, pattern, labelKey string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	require.NoError(t, err)

	var names []string
	for _, path := range matches {
		data, err := os.ReadFile(path)
		require.NoError(t, err, "reading %s", path)

		var cm configMapMeta
		require.NoError(t, yaml.Unmarshal(data, &cm), "parsing %s", path)

		name, ok := cm.Metadata.Labels[labelKey]
		require.True(t, ok, "ConfigMap %s is missing label %s", filepath.Base(path), labelKey)
		names = append(names, name)
	}
	return names
}

// extractCRDDefaults reads the generated CRD YAML and returns the default
// values for providers and collections.
func extractCRDDefaults(t *testing.T, crdPath string) (providers, collections []string) {
	t.Helper()
	data, err := os.ReadFile(crdPath)
	require.NoError(t, err, "reading CRD at %s — did you run `make manifests`?", crdPath)

	var crd crdSchema
	require.NoError(t, yaml.Unmarshal(data, &crd), "parsing CRD YAML")
	require.NotEmpty(t, crd.Spec.Versions, "CRD has no versions")

	spec := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"]

	providers = toStringSlice(t, spec.Properties["providers"]["default"], "providers")
	collections = toStringSlice(t, spec.Properties["collections"]["default"], "collections")

	require.NotEmpty(t, providers, "CRD has no default providers — check the kubebuilder marker")
	require.NotEmpty(t, collections, "CRD has no default collections — check the kubebuilder marker")
	return
}

// toStringSlice converts an interface{} (expected to be []interface{} of strings)
// into a []string, failing the test if the shape is unexpected.
func toStringSlice(t *testing.T, v interface{}, field string) []string {
	t.Helper()
	require.NotNil(t, v, "CRD field %q has no default value", field)
	items, ok := v.([]interface{})
	require.True(t, ok, "CRD default for %q is not an array: %T", field, v)
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		require.True(t, ok, "CRD default for %q contains non-string element: %T", field, item)
		out = append(out, s)
	}
	return out
}

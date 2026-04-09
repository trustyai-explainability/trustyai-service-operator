package evalhub

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
// We use map[string]any for property values because different fields
// have defaults of different types (scalars, arrays, objects).
type crdSchema struct {
	Spec struct {
		Versions []struct {
			Schema struct {
				OpenAPIV3Schema struct {
					Properties map[string]struct {
						Properties map[string]map[string]any `json:"properties"`
					} `json:"properties"`
				} `json:"openAPIV3Schema"`
			} `json:"schema"`
		} `json:"versions"`
	} `json:"spec"`
}

// extractNamesFromConfigMaps globs for YAML files matching pattern in dir and
// returns the values of the given label from each file.
func extractNamesFromConfigMaps(dir, pattern, labelKey string) []string {
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	Expect(err).NotTo(HaveOccurred())

	var names []string
	for _, path := range matches {
		data, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred(), "reading %s", path)

		var cm configMapMeta
		Expect(yaml.Unmarshal(data, &cm)).To(Succeed(), "parsing %s", path)

		name, ok := cm.Metadata.Labels[labelKey]
		Expect(ok).To(BeTrue(), "ConfigMap %s is missing label %s", filepath.Base(path), labelKey)
		names = append(names, name)
	}
	return names
}

// extractCRDDefaults reads the generated CRD YAML and returns the default
// values for providers and collections.
func extractCRDDefaults(crdPath string) (providers, collections []string) {
	data, err := os.ReadFile(crdPath)
	Expect(err).NotTo(HaveOccurred(), "reading CRD at %s — did you run `make manifests`?", crdPath)

	var crd crdSchema
	Expect(yaml.Unmarshal(data, &crd)).To(Succeed(), "parsing CRD YAML")
	Expect(crd.Spec.Versions).NotTo(BeEmpty(), "CRD has no versions")

	spec := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"]
	providers = toStringSlice(spec.Properties["providers"]["default"], "providers")
	collections = toStringSlice(spec.Properties["collections"]["default"], "collections")

	Expect(providers).NotTo(BeEmpty(), "CRD has no default providers — check the kubebuilder marker")
	Expect(collections).NotTo(BeEmpty(), "CRD has no default collections — check the kubebuilder marker")
	return
}

// toStringSlice converts an any (expected to be []any of strings)
// into a []string, failing the test if the shape is unexpected.
func toStringSlice(v any, field string) []string {
	Expect(v).NotTo(BeNil(), "CRD field %q has no default value", field)
	items, ok := v.([]any)
	Expect(ok).To(BeTrue(), "CRD default for %q is not an array: %T", field, v)
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		Expect(ok).To(BeTrue(), "CRD default for %q contains non-string element: %T", field, item)
		out = append(out, s)
	}
	return out
}

var _ = Describe("EvalHub CRD Defaults", func() {
	// This test verifies that the provider and collection defaults declared
	// in the EvalHub CRD stay in sync with the ConfigMap files shipped in
	// config/configmaps/evalhub/.
	//
	// If this test fails it means someone added, removed, or renamed a
	// provider or collection ConfigMap without updating the kubebuilder
	// default marker in api/evalhub/v1alpha1/evalhub_types.go (and
	// regenerating the CRD with `make manifests`).

	var (
		configDir string
		crdPath   string
	)

	BeforeEach(func() {
		root := repoRoot()
		configDir = filepath.Join(root, "config", "configmaps", "evalhub")
		crdPath = filepath.Join(root, "config", "components", "evalhub", "crd", "trustyai.opendatahub.io_evalhubs.yaml")
	})

	It("should have CRD default providers matching all provider ConfigMaps", func() {
		providerNames := extractNamesFromConfigMaps(configDir, "provider-*.yaml", providerNameLabel)
		Expect(providerNames).NotTo(BeEmpty(), "no provider ConfigMaps found in %s", configDir)

		crdProviders, _ := extractCRDDefaults(crdPath)

		sort.Strings(providerNames)
		sort.Strings(crdProviders)

		Expect(crdProviders).To(Equal(providerNames),
			"CRD default providers do not match provider ConfigMaps in %s.\n"+
				"Update the kubebuilder default marker on Providers in api/evalhub/v1alpha1/evalhub_types.go and run `make manifests`.", configDir)
	})

	It("should have CRD default collections matching all collection ConfigMaps", func() {
		collectionNames := extractNamesFromConfigMaps(configDir, "collection-*.yaml", collectionNameLabel)
		Expect(collectionNames).NotTo(BeEmpty(), "no collection ConfigMaps found in %s", configDir)

		_, crdCollections := extractCRDDefaults(crdPath)

		sort.Strings(collectionNames)
		sort.Strings(crdCollections)

		Expect(crdCollections).To(Equal(collectionNames),
			"CRD default collections do not match collection ConfigMaps in %s.\n"+
				"Update the kubebuilder default marker on Collections in api/evalhub/v1alpha1/evalhub_types.go and run `make manifests`.", configDir)
	})
})

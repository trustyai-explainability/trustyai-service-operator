package tas

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"
)

// servingCRDFixturePath is the CRD vendored from the serving release this
// operator is built against. The tests below hold the declarations in
// serving_fields.go against it, so a change on the serving side arrives as a
// failing test in this repository rather than as a write to a field that is no
// longer there.
const servingCRDFixturePath = "../../tests/crds/serving.kserve.io_inferenceservices.yaml"

func vendoredServingCRD() *apiextensionsv1.CustomResourceDefinition {
	raw, err := os.ReadFile(servingCRDFixturePath)
	Expect(err).NotTo(HaveOccurred(), "the vendored serving CRD has to be readable; it is what the field paths are checked against")

	crd := &apiextensionsv1.CustomResourceDefinition{}
	Expect(yaml.Unmarshal(raw, crd)).To(Succeed())
	return crd
}

var _ = Describe("The serving API surface", func() {
	Context("declared field paths", func() {
		It("name the CRD the contract is derived from", func() {
			crd := vendoredServingCRD()
			Expect(crd.Spec.Names.Plural+"."+crd.Spec.Group).To(Equal(inferenceServiceCRDName),
				"the vendored CRD is a different resource from the one the operator reads")
		})

		It("are described by at least one served version of the vendored CRD", func() {
			crd := vendoredServingCRD()

			served := []string{}
			for _, version := range crd.Spec.Versions {
				if version.Served {
					served = append(served, version.Name)
				}
			}
			Expect(served).NotTo(BeEmpty(), "a CRD that serves nothing cannot have its schema checked")

			for _, declared := range servingSchemaFieldPaths {
				describing := []string{}
				for _, version := range served {
					if schemaDescribesField(crdSchemaForVersion(crd, version), strings.Split(declared, ".")) {
						describing = append(describing, version)
					}
				}
				// Without a version that describes every declared path, no
				// cluster running this serving release could ever be adopted:
				// payload capture would be reported unsupported everywhere.
				Expect(describing).NotTo(BeEmpty(),
					"no served version of %s describes %s (served: %s)", inferenceServiceCRDName, declared, strings.Join(served, ", "))
			}
		})

		It("are all required before a version is adopted", func() {
			// A schema that describes the first declared path and nothing else
			// must not be adopted, or the declared set would be advisory.
			Expect(len(servingSchemaFieldPaths)).To(BeNumerically(">=", 1))
			full := &apiextensionsv1.JSONSchemaProps{Type: "object", Properties: map[string]apiextensionsv1.JSONSchemaProps{
				"spec": {Type: "object", Properties: map[string]apiextensionsv1.JSONSchemaProps{
					"predictor": {Type: "object", Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"logger": {Type: "object"},
					}},
				}},
			}}
			Expect(versionDescribesServingFields(full)).To(BeTrue())
			Expect(versionDescribesServingFields(nil)).To(BeFalse())
		})
	})

	Context("declared annotation keys", func() {
		It("are unique and well formed", func() {
			seen := map[string]bool{}
			for _, key := range servingAnnotationKeys {
				Expect(seen).NotTo(HaveKey(key), "%s is declared twice", key)
				seen[key] = true

				parts := strings.Split(key, "/")
				Expect(parts).To(HaveLen(2), "%s is not a qualified name of the form prefix/name", key)
				Expect(parts[0]).NotTo(BeEmpty(), "%s has an empty prefix", key)
				Expect(parts[1]).NotTo(BeEmpty(), "%s has an empty name", key)
				Expect(key).NotTo(MatchRegexp(`\s`), "%s contains whitespace", key)
			}
		})

		It("include every logger annotation payload capture deletes", func() {
			// The logger keys are the ones a rollback deletes. They are part of
			// the declared surface, so a key that is deleted must also be
			// declared, and the family keeps its documented prefix.
			for _, key := range servingLoggerAnnotationKeys {
				Expect(servingAnnotationKeys).To(ContainElement(key))
				Expect(key).To(HavePrefix("internal.serving.kserve.io/logger"))
			}
			Expect(servingLoggerAnnotationKeys).To(ConsistOf(
				servingLoggerAnnotation, servingLoggerModeAnnotation, servingLoggerSinkURLAnnotation))
		})
	})

	Context("the package source", func() {
		It("contains no serving literal outside the declarations", func() {
			// The declarations only stay authoritative if nothing else embeds the
			// same knowledge. This walks the package and fails on a literal that
			// names the serving API somewhere other than serving_fields.go, so a
			// second, unvalidated copy cannot appear.
			literals := []string{"serving.kserve.io", "sidecar.istio.io", "spec.predictor"}

			entries, err := os.ReadDir(".")
			Expect(err).NotTo(HaveOccurred())

			offenders := []string{}
			for _, entry := range entries {
				name := entry.Name()
				if entry.IsDir() || !strings.HasSuffix(name, ".go") ||
					strings.HasSuffix(name, "_test.go") || name == "serving_fields.go" {
					continue
				}

				file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(".", name), nil, 0)
				Expect(err).NotTo(HaveOccurred())
				ast.Inspect(file, func(node ast.Node) bool {
					literal, ok := node.(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						return true
					}
					value, err := strconv.Unquote(literal.Value)
					if err != nil {
						return true
					}
					for _, known := range literals {
						if strings.Contains(value, known) {
							offenders = append(offenders, name+": "+value)
						}
					}
					return true
				})
			}
			Expect(offenders).To(BeEmpty(),
				"serving literals belong in serving_fields.go, where the tests can hold them against the CRD schema")
		})
	})
})

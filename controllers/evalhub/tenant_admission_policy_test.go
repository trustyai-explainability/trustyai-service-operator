package evalhub

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	admissioncel "k8s.io/apiserver/pkg/admission/plugin/cel"
	"k8s.io/apiserver/pkg/cel/environment"
	"sigs.k8s.io/yaml"
)

type tenancyValidation string

func (v tenancyValidation) GetExpression() string    { return string(v) }
func (v tenancyValidation) ReturnTypes() []*cel.Type { return []*cel.Type{cel.BoolType} }

func TestEvalHubTenantAdmissionPolicy(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	var original string
	for _, prefix := range []string{"config/components", "trustyai-operator-module/config/manifests-template/components"} {
		raw, err := os.ReadFile(filepath.Join(root, prefix, "evalhub/tenant-admission-policy.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if original != "" && original != string(raw) {
			t.Fatal("admission manifests differ")
		}
		original = string(raw)
		docs := strings.Split(string(raw), "\n---\n")
		if len(docs) != 2 {
			t.Fatal("expected policy and binding")
		}
		var policy admissionv1.ValidatingAdmissionPolicy
		var binding admissionv1.ValidatingAdmissionPolicyBinding
		if err := yaml.Unmarshal([]byte(docs[0]), &policy); err != nil {
			t.Fatal(err)
		}
		if err := yaml.Unmarshal([]byte(docs[1]), &binding); err != nil {
			t.Fatal(err)
		}
		if policy.Spec.FailurePolicy == nil || *policy.Spec.FailurePolicy != admissionv1.Fail {
			t.Fatal("policy must fail closed")
		}
		if binding.Spec.PolicyName != policy.Name || len(binding.Spec.ValidationActions) != 1 || binding.Spec.ValidationActions[0] != admissionv1.Deny {
			t.Fatal("binding must enforce policy")
		}
		if len(policy.Spec.Validations) != 1 {
			t.Fatal("expected tenancy validation")
		}
		expression := policy.Spec.Validations[0].Expression
		compiler := admissioncel.NewCompiler(environment.MustBaseEnvSet(environment.DefaultCompatibilityVersion(), true))
		result := compiler.CompileCELExpression(tenancyValidation(expression), admissioncel.OptionalVariableDeclarations{HasAuthorizer: true}, environment.NewExpressions)
		if result.Error != nil {
			t.Fatalf("invalid admission CEL: %v", result.Error)
		}
		// Exercise the tenancy predicate with the authorization result substituted.
		// Actual RBAC/admission integration still requires a live API server.
		auth := "authorizer.group('trustyai.opendatahub.io').resource('evalhubmultitenancy').check('use').allowed()"
		for _, allowed := range []bool{false, true} {
			replacement := "false"
			if allowed {
				replacement = "true"
			}
			expr := strings.ReplaceAll(expression, auth, replacement)
			env, err := cel.NewEnv(cel.Variable("object", cel.DynType))
			if err != nil {
				t.Fatal(err)
			}
			ast, issues := env.Compile(expr)
			if issues.Err() != nil {
				t.Fatal(issues.Err())
			}
			program, err := env.Program(ast)
			if err != nil {
				t.Fatal(err)
			}
			for _, mode := range []string{"single", "multi", "", "missing"} {
				spec := map[string]interface{}{}
				if mode != "missing" {
					spec["tenancy"] = mode
				}
				value, _, err := program.Eval(map[string]interface{}{"object": map[string]interface{}{"spec": spec}})
				if err != nil {
					t.Fatal(err)
				}
				if value.Value() != (mode == "single" || allowed) {
					t.Fatalf("mode=%s privileged=%v result=%v", mode, allowed, value)
				}
			}
		}
	}
}

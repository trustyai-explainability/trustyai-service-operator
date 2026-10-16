package trustyaimodule

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

// TestModuleRoleDelegatesEveryComponentClusterRole asserts that the module
// manager role (config/rbac/role.yaml, generated from the kubebuilder markers
// in workload_rbac.go) is a superset of every ClusterRole the module applies
// on behalf of the trustyai-service-operator workload
// (config/manifests-template/components/*/rbac/*.yaml).
//
// Kubernetes RBAC escalation prevention rejects creating or updating a role
// that grants permissions the applying principal does not already hold, so a
// missing grant breaks modular installation with an API denial naming the
// module controller's ServiceAccount. This test fails at that exact class of
// gap instead of waiting for the modular smoke test. ClusterRoleBindings are
// out of scope: binding a role requires the bind verb, which the module role
// holds on the exact clusterrole resourceNames it needs.
func TestModuleRoleDelegatesEveryComponentClusterRole(t *testing.T) {
	moduleRole := &rbacv1.ClusterRole{}
	moduleRoleBytes, err := os.ReadFile(filepath.Join("..", "..", "config", "rbac", "role.yaml"))
	if err != nil {
		t.Fatalf("read module manager role: %v", err)
	}
	if err := yaml.Unmarshal(moduleRoleBytes, moduleRole); err != nil {
		t.Fatalf("parse module manager role: %v", err)
	}
	if len(moduleRole.Rules) == 0 {
		t.Fatal("module manager role has no rules")
	}

	componentRoles, err := loadComponentClusterRoles()
	if err != nil {
		t.Fatalf("load component ClusterRoles: %v", err)
	}
	if len(componentRoles) == 0 {
		t.Fatal("no component ClusterRoles found under config/manifests-template/components")
	}

	var missing []string
	moduleIndex := indexRuleTriples(moduleRole.Rules)
	for name, role := range componentRoles {
		for _, rule := range role.Rules {
			for _, gap := range uncoveredTriples(moduleIndex, rule) {
				missing = append(missing, name+" -> "+gap)
			}
		}
	}
	sort.Strings(missing)
	for _, gap := range missing {
		t.Errorf("component permission is not delegated by the module manager role (add a kubebuilder:rbac marker to workload_rbac.go and run make manifests-tom): %s", gap)
	}
}

func loadComponentClusterRoles() (map[string]*rbacv1.ClusterRole, error) {
	roles := map[string]*rbacv1.ClusterRole{}
	componentsDir := filepath.Join("..", "..", "config", "manifests-template", "components")
	components, err := os.ReadDir(componentsDir)
	if err != nil {
		return nil, err
	}
	for _, component := range components {
		if !component.IsDir() {
			continue
		}
		rbacDir := filepath.Join(componentsDir, component.Name(), "rbac")
		files, err := os.ReadDir(rbacDir)
		if err != nil {
			continue
		}
		for _, file := range files {
			if file.IsDir() || !strings.HasSuffix(file.Name(), ".yaml") {
				continue
			}
			path := filepath.Join(rbacDir, file.Name())
			contents, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			for _, document := range strings.Split(string(contents), "\n---") {
				role := &rbacv1.ClusterRole{}
				if err := yaml.Unmarshal([]byte(document), role); err != nil {
					continue
				}
				if role.Kind != "ClusterRole" || len(role.Rules) == 0 {
					continue
				}
				roles[component.Name()+"/"+file.Name()] = role
			}
		}
	}
	return roles, nil
}

// ruleTriple is one (apiGroup, resource, verb) permission, optionally scoped
// to resource names. RBAC permissions are the union of all rules, so coverage
// is decided per triple rather than per rule.
type ruleTriple struct {
	group    string
	resource string
	verb     string
	names    []string
}

// indexRuleTriples expands module rules into the set of triples they grant.
// A rule with no resource names grants its triple unscoped; a rule scoped to
// resource names grants the triple only on those names.
func indexRuleTriples(rules []rbacv1.PolicyRule) []ruleTriple {
	var triples []ruleTriple
	for _, rule := range rules {
		for _, group := range rule.APIGroups {
			for _, resource := range rule.Resources {
				for _, verb := range rule.Verbs {
					triples = append(triples, ruleTriple{group: group, resource: resource, verb: verb, names: rule.ResourceNames})
				}
			}
		}
	}
	return triples
}

// uncoveredTriples returns the component rule's permission triples that no
// module triple grants. A component triple scoped to resource names is only
// covered by an unscoped module triple or by one scoped to at least those
// names, so a narrowly scoped module rule can never satisfy a broader
// component rule.
func uncoveredTriples(moduleTriples []ruleTriple, componentRule rbacv1.PolicyRule) []string {
	var gaps []string
	for _, group := range componentRule.APIGroups {
		for _, resource := range componentRule.Resources {
			for _, verb := range componentRule.Verbs {
				covered := false
				for _, moduleTriple := range moduleTriples {
					if moduleTriple.group != group && moduleTriple.group != "*" {
						continue
					}
					if moduleTriple.resource != resource && moduleTriple.resource != "*" {
						continue
					}
					if moduleTriple.verb != verb && moduleTriple.verb != "*" {
						continue
					}
					if len(componentRule.ResourceNames) > 0 && len(moduleTriple.names) > 0 &&
						!setCovers(moduleTriple.names, componentRule.ResourceNames) {
						continue
					}
					covered = true
					break
				}
				if !covered {
					gaps = append(gaps, "apiGroup="+group+" resource="+resource+" verb="+verb)
				}
			}
		}
	}
	return gaps
}

// setCovers reports whether the covering set includes every element of the
// covered set, treating "*" as covering everything.
func setCovers(covering, covered []string) bool {
	if len(covered) == 0 {
		return true
	}
	for _, want := range covered {
		found := false
		for _, have := range covering {
			if have == "*" || have == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

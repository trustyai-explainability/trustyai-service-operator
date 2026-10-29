package trustyaimodule

import (
	"testing"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestMergeOperatorDeploymentScopesToServiceOperator(t *testing.T) {
	g := NewWithT(t)

	existing := deploymentWithCPU(OperatorDeploymentName, "1001m")
	desired := deploymentWithCPU(OperatorDeploymentName, "900m")

	g.Expect(mergeOperatorDeployment(existing, desired)).To(Succeed())
	g.Expect(cpuLimit(desired)).To(Equal("1001m"))
}

func TestMergeOperatorDeploymentIgnoresOtherDeployments(t *testing.T) {
	g := NewWithT(t)

	existing := deploymentWithCPU("some-other-controller", "1001m")
	desired := deploymentWithCPU("some-other-controller", "900m")

	g.Expect(mergeOperatorDeployment(existing, desired)).To(Succeed())
	g.Expect(cpuLimit(desired)).To(Equal("900m"))
}

func deploymentWithCPU(name, cpu string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name": name,
		},
		"spec": map[string]any{
			"replicas": int64(1),
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{
							"name": ManagerContainerName,
							"resources": map[string]any{
								"limits": map[string]any{
									"cpu": cpu,
								},
							},
						},
					},
				},
			},
		},
	}}
}

func cpuLimit(obj *unstructured.Unstructured) string {
	containers, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
	if len(containers) == 0 {
		return ""
	}
	container, _ := containers[0].(map[string]any)
	resources, _ := container["resources"].(map[string]any)
	limits, _ := resources["limits"].(map[string]any)
	cpu, _ := limits["cpu"].(string)
	return cpu
}

package trustyaimodule

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	platformv1alpha1 "github.com/trustyai-explainability/trustyai-operator-module/pkg/apis/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func operatorDeployment(containers ...map[string]interface{}) unstructured.Unstructured {
	items := make([]interface{}, 0, len(containers))
	for _, c := range containers {
		items = append(items, c)
	}
	return unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]interface{}{
			"name": OperatorDeploymentName,
		},
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": items,
				},
			},
		},
	}}
}

var _ = Describe("enabledServiceNames", func() {
	It("returns only the enabled services", func() {
		Expect(enabledServiceNames(platformv1alpha1.EnabledServices{TAS: true, GORCH: true})).To(ConsistOf("TAS", "GORCH"))
	})

	It("defaults to all services when none are explicitly enabled", func() {
		Expect(enabledServiceNames(platformv1alpha1.EnabledServices{})).To(ConsistOf(
			"TAS", "LMES", "EVALHUB", "GORCH", "NEMO_GUARDRAILS",
		))
	})
})

var _ = Describe("injectEnabledServices", func() {
	It("sets --enable-services on the manager container of the operator Deployment", func() {
		objs := []unstructured.Unstructured{
			operatorDeployment(map[string]interface{}{"name": ManagerContainerName}),
		}

		Expect(injectEnabledServices(objs, platformv1alpha1.EnabledServices{TAS: true, LMES: true})).To(Succeed())

		containers, found, err := unstructured.NestedSlice(objs[0].Object, "spec", "template", "spec", "containers")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(containers).To(HaveLen(1))

		container, ok := containers[0].(map[string]interface{})
		Expect(ok).To(BeTrue())
		Expect(container["args"]).To(Equal([]interface{}{"--enable-services=TAS,LMES"}))
	})

	It("ignores non-Deployment and non-matching-name objects", func() {
		other := unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Service",
			"metadata":   map[string]interface{}{"name": OperatorDeploymentName},
		}}
		objs := []unstructured.Unstructured{other}

		Expect(injectEnabledServices(objs, platformv1alpha1.EnabledServices{TAS: true})).To(Succeed())
		Expect(objs[0]).To(Equal(other))
	})

	It("leaves sidecar containers untouched", func() {
		objs := []unstructured.Unstructured{
			operatorDeployment(
				map[string]interface{}{"name": "sidecar", "args": []interface{}{"--existing"}},
				map[string]interface{}{"name": ManagerContainerName},
			),
		}

		Expect(injectEnabledServices(objs, platformv1alpha1.EnabledServices{TAS: true})).To(Succeed())

		containers, _, _ := unstructured.NestedSlice(objs[0].Object, "spec", "template", "spec", "containers")
		sidecar := containers[0].(map[string]interface{})
		manager := containers[1].(map[string]interface{})
		Expect(sidecar["args"]).To(Equal([]interface{}{"--existing"}))
		Expect(manager["args"]).To(Equal([]interface{}{"--enable-services=TAS"}))
	})
})

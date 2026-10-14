package trustyaimodule

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("paramsEnvMap", func() {
	It("uses the ODH TrustyAI service Python image for the workload image", func() {
		Expect(paramsEnvMap["trustyaiServiceImage"]).To(Equal("RELATED_IMAGE_ODH_TRUSTYAI_SERVICE_PY_IMAGE"))
	})

	It("uses the ODH TrustyAI service operator image for the module operator", func() {
		Expect(paramsEnvMap["trustyaiOperatorImage"]).To(Equal("RELATED_IMAGE_ODH_TRUSTYAI_SERVICE_OPERATOR_IMAGE"))
	})

	It("uses the ODH EvalHub lighteval adapter image for the lighteval provider", func() {
		Expect(paramsEnvMap["evalhub-provider-lighteval-image"]).To(Equal("RELATED_IMAGE_ODH_EVAL_HUB_LIGHTEVAL_IMAGE"))
	})

	It("uses the ODH EvalHub inspectai adapter image for the inspect provider", func() {
		Expect(paramsEnvMap["evalhub-provider-inspect-image"]).To(Equal("RELATED_IMAGE_ODH_EVAL_HUB_INSPECTAI_IMAGE"))
	})
})

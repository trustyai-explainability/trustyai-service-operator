package controllers

import "github.com/trustyai-explainability/trustyai-service-operator/controllers/tapm"

func init() {
	registerService(tapm.ServiceName, tapm.ControllerSetUp)
}

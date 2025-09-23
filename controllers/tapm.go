package controllers

import "github.com/trustyai-explainability/trustyai-service-operator/controllers/lmes"

func init() {
	registerService(lmes.ServiceName, lmes.ControllerSetUp)
}

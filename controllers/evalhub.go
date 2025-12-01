package controllers

import (
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/evalhub"
)

func init() {
	// Register the EvalHub controller
	registerService(evalhub.ServiceName, evalhub.ControllerSetUp)
}

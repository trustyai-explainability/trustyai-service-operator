package controllers

import "github.com/trustyai-explainability/trustyai-service-operator/controllers/guardrails"

func init() {
	registerService(guardrails.ServiceName, guardrails.ControllerSetUp)
}

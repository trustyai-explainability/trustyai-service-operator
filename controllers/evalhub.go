package controllers

import (
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/evalhub"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func init() {
	// Register the EvalHub controller
	registerService(evalhub.ServiceName, setupEvalHubController)
}

func setupEvalHubController(mgr manager.Manager, ns, _ string, recorder record.EventRecorder) error {
	return evalhub.ControllerSetUp(mgr, ns, recorder)
}

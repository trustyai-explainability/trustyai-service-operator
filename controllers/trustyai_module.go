package controllers

import (
	"os"

	"github.com/trustyai-explainability/trustyai-operator-module/pkg/trustyaimodule"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	serviceNameModule             = "TRUSTYAI_MODULE"
	defaultManifestsTemplatePath  = "/opt/manifests-template"
	envManifestsTemplatePath      = "MANIFESTS_TEMPLATE_PATH"
)

func init() {
	registerService(serviceNameModule, setupTrustyAIModuleController)
}

func setupTrustyAIModuleController(mgr manager.Manager, ns, _ string, recorder record.EventRecorder) error {
	manifestsPath := os.Getenv(envManifestsTemplatePath)
	if manifestsPath == "" {
		manifestsPath = defaultManifestsTemplatePath
	}

	return (&trustyaimodule.TrustyAIModuleReconciler{
		Client:                mgr.GetClient(),
		Scheme:                mgr.GetScheme(),
		Namespace:             ns,
		ManifestsTemplatePath: manifestsPath,
		EventRecorder:         recorder,
	}).SetupWithManager(mgr)
}

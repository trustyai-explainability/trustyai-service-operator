package gorch

import (
	"context"
	"reflect"

	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/gorch/templates"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const serviceAccountTemplatePath = "serviceaccount.tmpl.yaml"

type ServiceAccountConfig struct {
	Name      string
	Namespace string
	Version   string
}

func (r *GuardrailsOrchestratorReconciler) createServiceAccount(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) *corev1.ServiceAccount {
	serviceAccountConfig := ServiceAccountConfig{
		Name:      orchestrator.Name,
		Namespace: orchestrator.Namespace,
		Version:   Version,
	}
	var serviceAccount *corev1.ServiceAccount
	serviceAccount, err := templateParser.ParseResource[corev1.ServiceAccount](serviceAccountTemplatePath, serviceAccountConfig, reflect.TypeOf(&corev1.ServiceAccount{}))
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to parse service account template")
	}
	controllerutil.SetControllerReference(orchestrator, serviceAccount, r.Scheme)
	return serviceAccount
}

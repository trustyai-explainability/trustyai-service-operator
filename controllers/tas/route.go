package tas

import (
	"context"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"

	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1alpha1"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/tas/templates"
)

const (
	routeTemplatePath = "service/route.tmpl.yaml"
)

// ReconcileRoute will manage the creation, update and deletion of the
// TLS route when the service is reconciled
func (r *TrustyAIServiceReconciler) ReconcileRoute(
	instance *trustyaiopendatahubiov1alpha1.TrustyAIService, ctx context.Context, c client.Client) error {
	routeConfig := utils.RouteConfig{
		ServiceName: instance.Name + "-tls",
		PortName:    KubeRBACProxyServicePortName,
	}
	err := utils.ReconcileRoute(ctx, c, instance, routeConfig, routeTemplatePath, templateParser.ParseResource)
	return err
}

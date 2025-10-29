package gorch

import (
	"context"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"

	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/gorch/templates"
)

const (
	routeTemplatePath = "route.tmpl.yaml"
)

func (r *GuardrailsOrchestratorReconciler) reconcileGatewayRoute(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) error {
	gatewayTermination := utils.Edge
	if requiresOAuth(orchestrator) {
		gatewayTermination = utils.Reencrypt
	}
	routeConfig := utils.RouteConfig{
		Name:        utils.StringPointer(orchestrator.Name + "-gateway"),
		ServiceName: orchestrator.Name + "-service",
		PortName:    "gateway",
		Termination: utils.StringPointer(gatewayTermination),
	}
	_, err := utils.ReconcileRoute(ctx, r.Client, orchestrator, routeConfig, routeTemplatePath, templateParser.ParseResource)
	return err
}

func (r *GuardrailsOrchestratorReconciler) reconcileOrchestratorRoute(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) error {
	routeConfig := utils.RouteConfig{
		Name:        &orchestrator.Name,
		ServiceName: orchestrator.Name + "-service",
		PortName:    "https",
		Termination: utils.StringPointer(utils.Reencrypt),
	}
	_, err := utils.ReconcileRoute(ctx, r.Client, orchestrator, routeConfig, routeTemplatePath, templateParser.ParseResource)
	return err
}

func (r *GuardrailsOrchestratorReconciler) reconcileHealthRoute(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) error {
	routeConfig := utils.RouteConfig{
		Name:        utils.StringPointer(orchestrator.Name + "-health"),
		ServiceName: orchestrator.Name + "-service",
		PortName:    "health",
		Termination: utils.StringPointer(utils.Edge),
	}
	_, err := utils.ReconcileRoute(ctx, r.Client, orchestrator, routeConfig, routeTemplatePath, templateParser.ParseResource)
	return err
}

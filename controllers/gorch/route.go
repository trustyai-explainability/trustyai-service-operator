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
	if utils.RequiresAuth(orchestrator) {
		gatewayTermination = utils.Reencrypt
	}
	routeConfig := utils.RouteConfig{
		Name:        utils.StringPointer(orchestrator.Name + "-gateway"),
		ServiceName: orchestrator.Name + "-service",
		PortName:    "gateway",
		Termination: utils.StringPointer(gatewayTermination),
		Annotations: map[string]string{
			// Fix for RHOAIENG-33054: Set HAProxy timeout to 5 minutes
			// Gateway route handles LLM traffic that can exceed default 30s timeout
			"haproxy.router.openshift.io/timeout": "5m",
		},
	}
	err := utils.ReconcileRoute(ctx, r.Client, orchestrator, routeConfig, routeTemplatePath, templateParser.ParseResource)
	return err
}

func (r *GuardrailsOrchestratorReconciler) reconcileOrchestratorRoute(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) error {
	routeConfig := utils.RouteConfig{
		Name:        &orchestrator.Name,
		ServiceName: orchestrator.Name + "-service",
		PortName:    "https",
		Termination: utils.StringPointer(utils.Reencrypt),
		Annotations: map[string]string{
			// Fix for RHOAIENG-33054: Set HAProxy timeout to 5 minutes
			// LLM-based guardrails detection can take longer than the default 30s
			"haproxy.router.openshift.io/timeout": "5m",
		},
	}
	err := utils.ReconcileRoute(ctx, r.Client, orchestrator, routeConfig, routeTemplatePath, templateParser.ParseResource)
	return err
}

func (r *GuardrailsOrchestratorReconciler) reconcileBuiltInDetectorRoute(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) error {
	termination := utils.Edge
	if utils.RequiresAuth(orchestrator) {
		termination = utils.Reencrypt
	}
	routeConfig := utils.RouteConfig{
		Name:        utils.StringPointer(orchestrator.Name + "-built-in"),
		ServiceName: orchestrator.Name + "-service",
		PortName:    "built-in-detector",
		Termination: utils.StringPointer(termination),
		Annotations: map[string]string{
			// Fix for RHOAIENG-33054: Set HAProxy timeout to 5 minutes
			// Built-in detector route handles LLM traffic that can exceed default 30s timeout
			"haproxy.router.openshift.io/timeout": "5m",
		},
	}
	err := utils.ReconcileRoute(ctx, r.Client, orchestrator, routeConfig, routeTemplatePath, templateParser.ParseResource)
	return err
}

func (r *GuardrailsOrchestratorReconciler) reconcileHealthRoute(ctx context.Context, orchestrator *gorchv1alpha1.GuardrailsOrchestrator) error {
	routeConfig := utils.RouteConfig{
		Name:        utils.StringPointer(orchestrator.Name + "-health"),
		ServiceName: orchestrator.Name + "-service",
		PortName:    "health",
		Termination: utils.StringPointer(utils.Edge),
	}
	err := utils.ReconcileRoute(ctx, r.Client, orchestrator, routeConfig, routeTemplatePath, templateParser.ParseResource)
	return err
}

package trustyaimodule

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ServiceHealthResult describes the state of all operand instances for one
// enabled service. An enabled service with no operand instances is healthy:
// operand instances are optional user resources, and their absence does not
// indicate a module failure.
type ServiceHealthResult struct {
	Healthy  bool
	Degraded bool
	Unknown  bool
	Reason   string
}

// ServiceHealthChecker defines the interface for service-level health checks.
type ServiceHealthChecker interface {
	Name() string
	Check(ctx context.Context) ServiceHealthResult
}

type operandHealthDefinition struct {
	serviceName string
	gvk         schema.GroupVersionKind
}

var operandHealthDefinitions = map[string]operandHealthDefinition{
	"TAS":             {serviceName: "TAS", gvk: schema.GroupVersionKind{Group: "trustyai.opendatahub.io", Version: "v1", Kind: "TrustyAIServiceList"}},
	"LMES":            {serviceName: "LMES", gvk: schema.GroupVersionKind{Group: "trustyai.opendatahub.io", Version: "v1alpha1", Kind: "LMEvalJobList"}},
	"EVALHUB":         {serviceName: "EVALHUB", gvk: schema.GroupVersionKind{Group: "trustyai.opendatahub.io", Version: "v1", Kind: "EvalHubList"}},
	"GORCH":           {serviceName: "GORCH", gvk: schema.GroupVersionKind{Group: "trustyai.opendatahub.io", Version: "v1alpha1", Kind: "GuardrailsOrchestratorList"}},
	"NEMO_GUARDRAILS": {serviceName: "NEMO_GUARDRAILS", gvk: schema.GroupVersionKind{Group: "trustyai.opendatahub.io", Version: "v1alpha1", Kind: "NemoGuardrailsList"}},
}

// OperandHealthChecker reads the CRs managed by the TrustyAI service operator
// cluster-wide. This deliberately does not use the module namespace: operand
// instances are namespaced and a service may have instances in many
// namespaces.
type OperandHealthChecker struct {
	definition operandHealthDefinition
	client     client.Client
}

func NewOperandHealthChecker(name string, c client.Client) *OperandHealthChecker {
	return &OperandHealthChecker{definition: operandHealthDefinitions[name], client: c}
}

func (r *OperandHealthChecker) Name() string { return r.definition.serviceName }

func (r *OperandHealthChecker) Check(ctx context.Context) ServiceHealthResult {
	operands := &unstructured.UnstructuredList{}
	operands.SetGroupVersionKind(r.definition.gvk)
	if err := r.client.List(ctx, operands); err != nil {
		return ServiceHealthResult{Reason: fmt.Sprintf("failed to list operand instances: %v", err), Unknown: true}
	}

	if len(operands.Items) == 0 {
		return ServiceHealthResult{
			Healthy: true,
			Reason:  "No operand instances found",
		}
	}

	instances := make([]string, 0, len(operands.Items))
	degraded := false
	for i := range operands.Items {
		operand := &operands.Items[i]
		state, reason := operandState(operand)
		if state == operandReady {
			continue
		}
		instance := operand.GetNamespace() + "/" + operand.GetName()
		if operand.GetNamespace() == "" {
			instance = operand.GetName()
		}
		instances = append(instances, fmt.Sprintf("%s: %s", instance, reason))
		if state == operandFailed {
			degraded = true
		}
	}

	if len(instances) == 0 {
		return ServiceHealthResult{Healthy: true, Reason: fmt.Sprintf("%d operand instance(s) healthy", len(operands.Items))}
	}
	sort.Strings(instances)
	return ServiceHealthResult{Reason: strings.Join(instances, "; "), Degraded: degraded}
}

type operandStateValue string

const (
	operandReady       operandStateValue = "ready"
	operandProgressing operandStateValue = "progressing"
	operandFailed      operandStateValue = "failed"
)

func operandState(operand *unstructured.Unstructured) (operandStateValue, string) {
	conditions, found, err := unstructured.NestedSlice(operand.Object, "status", "conditions")
	if err == nil && found {
		for _, raw := range conditions {
			condition, ok := raw.(map[string]interface{})
			if !ok || condition["type"] != "Ready" {
				continue
			}
			status, _ := condition["status"].(string)
			reason, _ := condition["reason"].(string)
			message, _ := condition["message"].(string)
			if message == "" {
				message, _ = condition["reason"].(string)
			}
			switch strings.ToLower(status) {
			case "true":
				return operandReady, "ready"
			case "false":
				if message == "" {
					message = "Ready condition is false"
				}
				if isTerminalFailureReason(reason) {
					return operandFailed, message
				}
				return operandProgressing, message
			default:
				if message == "" {
					message = "Ready condition is not yet true"
				}
				return operandProgressing, message
			}
		}
	}

	if ready, found, _ := unstructured.NestedString(operand.Object, "status", "ready"); found {
		if strings.EqualFold(ready, "true") {
			return operandReady, "ready"
		}
		if strings.EqualFold(ready, "false") {
			return operandFailed, "status.ready is false"
		}
	}
	if phase, found, _ := unstructured.NestedString(operand.Object, "status", "phase"); found {
		switch strings.ToLower(phase) {
		case "ready", "running", "succeeded", "complete", "completed":
			return operandReady, phase
		case "failed", "error", "degraded":
			return operandFailed, "phase is " + phase
		}
	}
	return operandProgressing, "operand status is not yet available"
}

func isTerminalFailureReason(reason string) bool {
	switch strings.ToLower(reason) {
	case "failed", "error", "reconcilefailed", "reconciliationfailed", "deploymentfailed", "invalid", "invalidspec", "validationfailed":
		return true
	default:
		return false
	}
}

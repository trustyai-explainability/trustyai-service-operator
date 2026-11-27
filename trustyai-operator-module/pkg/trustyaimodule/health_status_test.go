package trustyaimodule

import (
	"context"
	"testing"
)

type fakeHealthChecker struct {
	name   string
	scope  HealthScope
	result ServiceHealthResult
}

func (f fakeHealthChecker) Name() string       { return f.name }
func (f fakeHealthChecker) Scope() HealthScope { return f.scope }
func (f fakeHealthChecker) Check(context.Context) ServiceHealthResult {
	return f.result
}

func TestSummarizeHealthAttributesOperatorVsOperand(t *testing.T) {
	tests := []struct {
		name               string
		checkers           []ServiceHealthChecker
		results            []ServiceHealthResult
		wantOperatorFailed bool
		wantOperandFailed  bool
		wantDegradedReason string
		wantReadyReason    string
		wantWaitingReason  string
		wantDegradedMsg    string
		wantWaitingMsg     string
	}{
		{
			name: "operator-only progressing",
			checkers: []ServiceHealthChecker{
				fakeHealthChecker{name: "trustyai-service-operator", scope: HealthScopeOperator},
				fakeHealthChecker{name: "TAS", scope: HealthScopeOperand},
			},
			results: []ServiceHealthResult{
				{Reason: "operator Deployment is not ready (0/1 replicas available)"},
				{Healthy: true, Reason: "No operand instances found"},
			},
			wantOperatorFailed: true,
			wantReadyReason:    "OperatorNotReady",
			wantWaitingReason:  "WaitingForOperator",
			wantWaitingMsg:     "Waiting for TrustyAI service operator Deployment: trustyai-service-operator: operator Deployment is not ready (0/1 replicas available)",
		},
		{
			name: "operator-only degraded",
			checkers: []ServiceHealthChecker{
				fakeHealthChecker{name: "trustyai-service-operator", scope: HealthScopeOperator},
			},
			results: []ServiceHealthResult{
				{Reason: "failed to create pod", Degraded: true},
			},
			wantOperatorFailed: true,
			wantDegradedReason: "OperatorUnhealthy",
			wantReadyReason:    "OperatorNotReady",
			wantDegradedMsg:    "TrustyAI service operator Deployment is unhealthy: trustyai-service-operator: failed to create pod",
		},
		{
			name: "operand-only degraded",
			checkers: []ServiceHealthChecker{
				fakeHealthChecker{name: "trustyai-service-operator", scope: HealthScopeOperator},
				fakeHealthChecker{name: "EVALHUB", scope: HealthScopeOperand},
			},
			results: []ServiceHealthResult{
				{Healthy: true, Reason: "operator Deployment has 1 available replica(s)"},
				{Reason: "team-a/evalhub: missing required image key", Degraded: true},
			},
			wantOperandFailed:  true,
			wantDegradedReason: "OperandUnhealthy",
			wantReadyReason:    "OperandsNotReady",
			wantDegradedMsg:    "One or more operand instances are unhealthy: EVALHUB: team-a/evalhub: missing required image key",
		},
		{
			name: "operand-only waiting",
			checkers: []ServiceHealthChecker{
				fakeHealthChecker{name: "TAS", scope: HealthScopeOperand},
			},
			results: []ServiceHealthResult{
				{Reason: "team-a/tas: not ready yet"},
			},
			wantOperandFailed: true,
			wantReadyReason:   "OperandsNotReady",
			wantWaitingReason: "WaitingForOperands",
			wantWaitingMsg:    "Waiting for enabled operand instances: TAS: team-a/tas: not ready yet",
		},
		{
			name: "mixed operator and operand failures",
			checkers: []ServiceHealthChecker{
				fakeHealthChecker{name: "trustyai-service-operator", scope: HealthScopeOperator},
				fakeHealthChecker{name: "TAS", scope: HealthScopeOperand},
			},
			results: []ServiceHealthResult{
				{Reason: "operator Deployment is scaled to zero", Degraded: true},
				{Reason: "team-a/tas: Ready condition is false", Degraded: true},
			},
			wantOperatorFailed: true,
			wantOperandFailed:  true,
			wantDegradedReason: "ServicesUnhealthy",
			wantReadyReason:    "ServicesNotReady",
			wantDegradedMsg:    "Service operator and operand instances are unhealthy: trustyai-service-operator: operator Deployment is scaled to zero; TAS: team-a/tas: Ready condition is false",
		},
		{
			name: "operator unknown",
			checkers: []ServiceHealthChecker{
				fakeHealthChecker{name: "trustyai-service-operator", scope: HealthScopeOperator},
			},
			results: []ServiceHealthResult{
				{Reason: "failed to get operator Deployment: forbidden", Unknown: true},
			},
			wantOperatorFailed: true,
			wantReadyReason:    "OperatorHealthUnknown",
		},
		{
			name: "operand unknown",
			checkers: []ServiceHealthChecker{
				fakeHealthChecker{name: "TAS", scope: HealthScopeOperand},
			},
			results: []ServiceHealthResult{
				{Reason: "failed to list operand instances: forbidden", Unknown: true},
			},
			wantOperandFailed: true,
			wantReadyReason:   "OperandHealthUnknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := summarizeHealth(tt.checkers, tt.results)
			if summary.AllHealthy {
				t.Fatalf("expected unhealthy summary, got %#v", summary)
			}
			if summary.OperatorFailed != tt.wantOperatorFailed {
				t.Fatalf("OperatorFailed: got %v want %v", summary.OperatorFailed, tt.wantOperatorFailed)
			}
			if summary.OperandFailed != tt.wantOperandFailed {
				t.Fatalf("OperandFailed: got %v want %v", summary.OperandFailed, tt.wantOperandFailed)
			}
			if tt.wantDegradedReason != "" && summary.degradedReason() != tt.wantDegradedReason {
				t.Fatalf("degradedReason: got %q want %q", summary.degradedReason(), tt.wantDegradedReason)
			}
			if summary.readyReason() != tt.wantReadyReason {
				t.Fatalf("readyReason: got %q want %q", summary.readyReason(), tt.wantReadyReason)
			}
			if tt.wantWaitingReason != "" && summary.waitingReason() != tt.wantWaitingReason {
				t.Fatalf("waitingReason: got %q want %q", summary.waitingReason(), tt.wantWaitingReason)
			}
			if tt.wantDegradedMsg != "" && summary.degradedMessage() != tt.wantDegradedMsg {
				t.Fatalf("degradedMessage: got %q want %q", summary.degradedMessage(), tt.wantDegradedMsg)
			}
			if tt.wantWaitingMsg != "" && summary.waitingMessage() != tt.wantWaitingMsg {
				t.Fatalf("waitingMessage: got %q want %q", summary.waitingMessage(), tt.wantWaitingMsg)
			}
		})
	}
}

func TestSummarizeHealthAllHealthy(t *testing.T) {
	checkers := []ServiceHealthChecker{
		fakeHealthChecker{name: "trustyai-service-operator", scope: HealthScopeOperator},
		fakeHealthChecker{name: "TAS", scope: HealthScopeOperand},
	}
	results := []ServiceHealthResult{
		{Healthy: true, Reason: "operator Deployment has 1 available replica(s)"},
		{Healthy: true, Reason: "No operand instances found"},
	}
	summary := summarizeHealth(checkers, results)
	if !summary.AllHealthy || summary.OperatorFailed || summary.OperandFailed || summary.AnyDegraded {
		t.Fatalf("expected all healthy, got %#v", summary)
	}
}

func TestOperatorHealthCheckerReportsScope(t *testing.T) {
	checker := NewOperatorHealthChecker(nil, "default")
	if checker.Scope() != HealthScopeOperator {
		t.Fatalf("expected operator scope, got %q", checker.Scope())
	}
	if checker.Name() != "trustyai-service-operator" {
		t.Fatalf("expected service-operator name, got %q", checker.Name())
	}
}

func TestOperandHealthCheckerReportsScope(t *testing.T) {
	checker := NewOperandHealthChecker("EVALHUB", nil)
	if checker.Scope() != HealthScopeOperand {
		t.Fatalf("expected operand scope, got %q", checker.Scope())
	}
}

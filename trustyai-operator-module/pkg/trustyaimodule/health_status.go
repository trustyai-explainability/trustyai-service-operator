package trustyaimodule

import "strings"

// healthSummary classifies aggregated checker results so Ready/Degraded reasons
// attribute failures to the service-operator Deployment, operand CRs, or both —
// instead of always labeling them as operand problems.
type healthSummary struct {
	AllHealthy       bool
	AnyDegraded      bool
	AnyUnknown       bool
	OperatorFailed   bool
	OperandFailed    bool
	UnhealthyReasons []string
}

func summarizeHealth(checkers []ServiceHealthChecker, results []ServiceHealthResult) healthSummary {
	summary := healthSummary{AllHealthy: true}
	for i, result := range results {
		scope := checkers[i].Scope()
		name := checkers[i].Name()
		if !result.Healthy {
			summary.AllHealthy = false
			summary.UnhealthyReasons = append(summary.UnhealthyReasons, name+": "+result.Reason)
			switch scope {
			case HealthScopeOperator:
				summary.OperatorFailed = true
			case HealthScopeOperand:
				summary.OperandFailed = true
			}
		}
		if result.Degraded {
			summary.AnyDegraded = true
		}
		if result.Unknown {
			summary.AnyUnknown = true
		}
	}
	return summary
}

func (s healthSummary) reasonsMessage() string {
	return strings.Join(s.UnhealthyReasons, "; ")
}

func (s healthSummary) readyReason() string {
	switch {
	case s.AnyUnknown && !s.AnyDegraded:
		switch {
		case s.OperatorFailed && s.OperandFailed:
			return "ServiceHealthUnknown"
		case s.OperatorFailed:
			return "OperatorHealthUnknown"
		default:
			return "OperandHealthUnknown"
		}
	case s.OperatorFailed && s.OperandFailed:
		return "ServicesNotReady"
	case s.OperatorFailed:
		return "OperatorNotReady"
	default:
		return "OperandsNotReady"
	}
}

func (s healthSummary) degradedReason() string {
	switch {
	case s.OperatorFailed && s.OperandFailed:
		return "ServicesUnhealthy"
	case s.OperatorFailed:
		return "OperatorUnhealthy"
	default:
		return "OperandUnhealthy"
	}
}

func (s healthSummary) degradedMessage() string {
	msg := s.reasonsMessage()
	switch {
	case s.OperatorFailed && s.OperandFailed:
		return "Service operator and operand instances are unhealthy: " + msg
	case s.OperatorFailed:
		return "TrustyAI service operator Deployment is unhealthy: " + msg
	default:
		return "One or more operand instances are unhealthy: " + msg
	}
}

func (s healthSummary) waitingReason() string {
	switch {
	case s.OperatorFailed && s.OperandFailed:
		return "WaitingForServices"
	case s.OperatorFailed:
		return "WaitingForOperator"
	default:
		return "WaitingForOperands"
	}
}

func (s healthSummary) waitingMessage() string {
	msg := s.reasonsMessage()
	switch {
	case s.OperatorFailed && s.OperandFailed:
		return "Waiting for service operator and operand instances: " + msg
	case s.OperatorFailed:
		return "Waiting for TrustyAI service operator Deployment: " + msg
	default:
		return "Waiting for enabled operand instances: " + msg
	}
}

func (s healthSummary) eventReasonAndMessage() (string, string, string) {
	// returns event type, reason, message
	switch {
	case s.AnyDegraded && s.OperatorFailed && s.OperandFailed:
		return "Warning", "HealthCheckFailed", "Service operator and operand instances are unhealthy"
	case s.AnyDegraded && s.OperatorFailed:
		return "Warning", "HealthCheckFailed", "TrustyAI service operator Deployment is unhealthy"
	case s.AnyDegraded:
		return "Warning", "HealthCheckFailed", "One or more operand instances are unhealthy"
	case s.OperatorFailed && s.OperandFailed:
		return "Normal", "HealthCheckWaiting", "Waiting for service operator and operand instances"
	case s.OperatorFailed:
		return "Normal", "HealthCheckWaiting", "Waiting for TrustyAI service operator Deployment"
	default:
		return "Normal", "HealthCheckWaiting", "Waiting for enabled operand instances"
	}
}

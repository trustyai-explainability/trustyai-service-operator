package module

import (
	"context"
	"testing"
)

func TestRunningServiceCheckerDefaultUnhealthy(t *testing.T) {
	hc := NewRunningServiceChecker("TAS")

	healthy, reason := hc.IsHealthy(context.Background())
	if healthy {
		t.Error("new checker should not be healthy")
	}
	if reason != "controller not started" {
		t.Errorf("reason = %q, want 'controller not started'", reason)
	}
}

func TestRunningServiceCheckerSetRunning(t *testing.T) {
	hc := NewRunningServiceChecker("LMES")
	hc.SetRunning()

	healthy, reason := hc.IsHealthy(context.Background())
	if !healthy {
		t.Error("should be healthy after SetRunning")
	}
	if reason != "" {
		t.Errorf("reason = %q, want empty", reason)
	}
}

func TestRunningServiceCheckerSetFailed(t *testing.T) {
	hc := NewRunningServiceChecker("EVALHUB")
	hc.SetFailed("CRD not installed")

	healthy, reason := hc.IsHealthy(context.Background())
	if healthy {
		t.Error("should not be healthy after SetFailed")
	}
	if reason != "CRD not installed" {
		t.Errorf("reason = %q, want 'CRD not installed'", reason)
	}
}

func TestRunningServiceCheckerName(t *testing.T) {
	hc := NewRunningServiceChecker("GORCH")
	if hc.Name() != "GORCH" {
		t.Errorf("Name() = %q, want GORCH", hc.Name())
	}
}

func TestRunningServiceCheckerRecovery(t *testing.T) {
	hc := NewRunningServiceChecker("NEMO")
	hc.SetFailed("startup error")
	hc.SetRunning()

	healthy, _ := hc.IsHealthy(context.Background())
	if !healthy {
		t.Error("should be healthy after recovery from failed state")
	}
}

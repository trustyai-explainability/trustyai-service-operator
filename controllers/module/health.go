package module

import (
	"context"
	"sync"
)

// RunningServiceChecker reports a service as healthy if it was successfully
// registered during startup. This is the initial health check implementation —
// "controller started successfully = healthy". More granular checks (e.g.
// CR-level health, informer sync status) can be added later.
type RunningServiceChecker struct {
	name    string
	mu      sync.RWMutex
	running bool
	message string
}

func NewRunningServiceChecker(name string) *RunningServiceChecker {
	return &RunningServiceChecker{name: name}
}

func (c *RunningServiceChecker) Name() string { return c.name }

func (c *RunningServiceChecker) IsHealthy(_ context.Context) (bool, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.running {
		return true, ""
	}
	if c.message != "" {
		return false, c.message
	}
	return false, "controller not started"
}

// SetRunning marks the service as successfully started.
func (c *RunningServiceChecker) SetRunning() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.running = true
	c.message = ""
}

// SetFailed marks the service as failed with a reason.
func (c *RunningServiceChecker) SetFailed(reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.running = false
	c.message = reason
}

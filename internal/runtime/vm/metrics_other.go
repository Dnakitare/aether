//go:build !linux

// Package vm provides Firecracker microVM management.
package vm

import (
	"context"
	"time"

	"github.com/dnakitare/aether/pkg/api"
)

// IsRunning reports whether the Firecracker process is still alive.
// On non-Linux platforms process-liveness detection is not supported;
// this stub always returns false so the metrics collector emits zeros.
func (v *VM) IsRunning() bool {
	return false
}

// GetMetrics returns zero-value metrics on non-Linux platforms.
// Firecracker only runs on Linux; this stub exists so the package compiles
// on macOS for local development.
func (v *VM) GetMetrics(_ context.Context) (*api.AgentMetrics, error) {
	return &api.AgentMetrics{LastUpdated: time.Now()}, nil
}

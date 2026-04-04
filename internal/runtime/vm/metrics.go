//go:build linux

// Package vm provides Firecracker microVM management.
package vm

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dnakitare/aether/pkg/api"
)

// IsRunning reports whether the Firecracker process is still alive.
// It uses signal 0, which checks process existence without delivering a signal.
func (v *VM) IsRunning() bool {
	if v.cmd == nil || v.cmd.Process == nil {
		return false
	}
	// signal 0 returns ESRCH if the process no longer exists.
	err := syscall.Kill(v.cmd.Process.Pid, 0)
	return err == nil
}

// GetMetrics reads basic process-level metrics from /proc.
// On non-Linux systems or when the process is not running, zeros are returned.
func (v *VM) GetMetrics(_ context.Context) (*api.AgentMetrics, error) {
	if !v.IsRunning() {
		return &api.AgentMetrics{LastUpdated: time.Now()}, nil
	}

	pid := v.cmd.Process.Pid

	memMB, err := readProcMemoryMB(pid)
	if err != nil {
		// Non-fatal: return what we have
		return &api.AgentMetrics{LastUpdated: time.Now()}, nil
	}

	cpuPercent, err := readProcCPUPercent(pid)
	if err != nil {
		return &api.AgentMetrics{MemoryUsageMB: memMB, LastUpdated: time.Now()}, nil
	}

	return &api.AgentMetrics{
		CPUUsagePercent: cpuPercent,
		MemoryUsageMB:   memMB,
		LastUpdated:     time.Now(),
	}, nil
}

// readProcMemoryMB parses VmRSS from /proc/{pid}/status.
// Returns resident set size in MB.
func readProcMemoryMB(pid int) (int64, error) {
	path := fmt.Sprintf("/proc/%d/status", pid)
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		// Format: "VmRSS:   12345 kB"
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, fmt.Errorf("unexpected VmRSS format")
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("failed to parse VmRSS: %w", err)
		}
		return kb / 1024, nil
	}

	return 0, fmt.Errorf("VmRSS not found in %s", path)
}

// readProcCPUPercent computes average CPU utilisation since process start.
// It reads accumulated jiffies from /proc/{pid}/stat and the total elapsed
// time from /proc/uptime, giving a lifetime-average CPU %.
// This avoids a blocking sleep while still providing a meaningful signal.
func readProcCPUPercent(pid int) (float64, error) {
	// Read /proc/{pid}/stat
	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	statData, err := os.ReadFile(statPath)
	if err != nil {
		return 0, err
	}

	// The format is: pid (comm) state ppid ... utime(14) stime(15) ... starttime(22)
	// The comm field may contain spaces and parentheses, so we split after the closing ')'.
	raw := string(statData)
	rp := strings.LastIndex(raw, ")")
	if rp < 0 {
		return 0, fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	fields := strings.Fields(raw[rp+1:])
	// After the closing ')' the fields are 0-indexed:
	// field[0] = state, field[11] = utime, field[12] = stime, field[19] = starttime
	if len(fields) < 20 {
		return 0, fmt.Errorf("too few fields in /proc/%d/stat", pid)
	}

	utime, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse utime: %w", err)
	}
	stime, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse stime: %w", err)
	}
	starttime, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse starttime: %w", err)
	}

	// Read system uptime
	uptimeData, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, err
	}
	uptimeFields := strings.Fields(string(uptimeData))
	if len(uptimeFields) < 1 {
		return 0, fmt.Errorf("malformed /proc/uptime")
	}
	uptimeSeconds, err := strconv.ParseFloat(uptimeFields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse uptime: %w", err)
	}

	// Clock ticks per second (HZ), typically 100 on Linux.
	const clkTck = 100.0

	processStartSeconds := float64(starttime) / clkTck
	elapsed := uptimeSeconds - processStartSeconds
	if elapsed <= 0 {
		return 0, nil
	}

	totalCPUSeconds := float64(utime+stime) / clkTck
	cpuPercent := (totalCPUSeconds / elapsed) * 100.0

	return cpuPercent, nil
}

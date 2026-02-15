// Package main is the entry point for the Aether CLI.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aether-runtime/aether/internal/observability"
	"github.com/aether-runtime/aether/internal/runtime"
	"github.com/aether-runtime/aether/internal/runtime/vm"
	"github.com/aether-runtime/aether/pkg/api"
	"github.com/spf13/cobra"
)

var (
	// Global flags
	logLevel   string
	configFile string

	// Runtime instance (shared across commands)
	rt     api.Runtime
	logger *slog.Logger
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "aether",
	Short: "Aether - A class-leading AI agent runtime",
	Long: `Aether is a production-grade runtime for AI agents.
It provides secure isolation using Firecracker microVMs,
orchestration, and observability at scale.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Initialize observability
		ctx := context.Background()
		level := observability.LogLevelInfo
		switch logLevel {
		case "debug":
			level = observability.LogLevelDebug
		case "warn":
			level = observability.LogLevelWarn
		case "error":
			level = observability.LogLevelError
		}

		var err error
		logger, _, err = observability.Setup(ctx, observability.Config{
			LogLevel:       level,
			ServiceName:    "aether",
			ServiceVersion: "0.1.0",
			Environment:    "development",
			EnableTracing:  false,
		})
		if err != nil {
			return fmt.Errorf("failed to setup observability: %w", err)
		}

		slog.SetDefault(logger)
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().StringVar(&configFile, "config", "", "Config file path")

	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(agentCmd)
}

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Start the Aether runtime daemon",
	Long:  "Start the Aether runtime daemon to manage AI agents.",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		logger.InfoContext(ctx, "starting Aether daemon")

		// Create runtime configuration
		config := runtime.Config{
			VMManagerConfig: vm.ManagerConfig{
				FirecrackerBinary: "/usr/local/bin/firecracker",
				WorkspaceDir:      "/var/lib/aether/vms",
				KernelImage:       "/var/lib/aether/images/vmlinux",
				RootFSImage:       "/var/lib/aether/images/rootfs.ext4",
			},
			DefaultResources: api.ResourceLimits{
				CPUCount: 1,
				MemoryMB: 512,
			},
			WorkspaceDir: "/var/lib/aether",
		}

		// Create runtime (without state store for simple daemon mode)
		var err error
		rt, err = runtime.New(logger, config, nil)
		if err != nil {
			return fmt.Errorf("failed to create runtime: %w", err)
		}

		// Handle shutdown signals
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

		logger.InfoContext(ctx, "Aether daemon started successfully")
		logger.InfoContext(ctx, "Press Ctrl+C to stop")

		// Wait for shutdown signal
		<-sigCh

		logger.InfoContext(ctx, "shutting down daemon")
		if err := rt.Shutdown(ctx); err != nil {
			logger.ErrorContext(ctx, "error during shutdown", "error", err)
			return err
		}

		logger.InfoContext(ctx, "daemon stopped")
		return nil
	},
}

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage agents",
	Long:  "Create, start, stop, and manage AI agents.",
}

func init() {
	agentCmd.AddCommand(agentCreateCmd)
	agentCmd.AddCommand(agentListCmd)
	agentCmd.AddCommand(agentLogsCmd)
	agentCmd.AddCommand(agentStopCmd)
	agentCmd.AddCommand(agentDestroyCmd)
	agentCmd.AddCommand(agentHealthCmd)
}

var (
	agentImage    string
	agentName     string
	agentTenantID string
	agentCPU      int
	agentMemoryMB int64
)

var agentCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new agent",
	Long:  "Create and start a new AI agent with the specified configuration.",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		if rt == nil {
			return fmt.Errorf("runtime not initialized - run 'aether daemon' first")
		}

		// Generate agent ID
		agentID := api.AgentID(fmt.Sprintf("agent-%d", os.Getpid()))

		config := api.AgentConfig{
			ID:       agentID,
			TenantID: api.TenantID(agentTenantID),
			Name:     agentName,
			Image:    agentImage,
			Resources: api.ResourceLimits{
				CPUCount: agentCPU,
				MemoryMB: agentMemoryMB,
			},
		}

		logger.InfoContext(ctx, "creating agent", "id", agentID)

		if err := rt.CreateAgent(ctx, config); err != nil {
			return fmt.Errorf("failed to create agent: %w", err)
		}

		if err := rt.StartAgent(ctx, agentID); err != nil {
			return fmt.Errorf("failed to start agent: %w", err)
		}

		fmt.Printf("Agent created and started: %s\n", agentID)
		return nil
	},
}

func init() {
	agentCreateCmd.Flags().StringVar(&agentImage, "image", "python:3.11", "Container image to run")
	agentCreateCmd.Flags().StringVar(&agentName, "name", "", "Agent name")
	agentCreateCmd.Flags().StringVar(&agentTenantID, "tenant", "default", "Tenant ID")
	agentCreateCmd.Flags().IntVar(&agentCPU, "cpu", 1, "Number of CPUs")
	agentCreateCmd.Flags().Int64Var(&agentMemoryMB, "memory", 512, "Memory in MB")
	agentCreateCmd.MarkFlagRequired("name")
}

var agentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all agents",
	Long:  "List all agents managed by the runtime.",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		if rt == nil {
			return fmt.Errorf("runtime not initialized - run 'aether daemon' first")
		}

		agents, err := rt.ListAgents(ctx, nil)
		if err != nil {
			return fmt.Errorf("failed to list agents: %w", err)
		}

		if len(agents) == 0 {
			fmt.Println("No agents found")
			return nil
		}

		fmt.Printf("%-20s %-15s %-10s %-20s\n", "ID", "NAME", "STATUS", "TENANT")
		fmt.Println("--------------------------------------------------------------------------------")
		for _, agent := range agents {
			fmt.Printf("%-20s %-15s %-10s %-20s\n",
				agent.Config.ID,
				agent.Config.Name,
				agent.Status,
				agent.Config.TenantID,
			)
		}

		return nil
	},
}

var (
	logsFollow bool
)

var agentLogsCmd = &cobra.Command{
	Use:   "logs <agent-id>",
	Short: "Get agent logs",
	Long:  "Retrieve logs from a running agent.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		agentID := api.AgentID(args[0])

		if rt == nil {
			return fmt.Errorf("runtime not initialized - run 'aether daemon' first")
		}

		reader, err := rt.GetAgentLogs(ctx, agentID, logsFollow)
		if err != nil {
			return fmt.Errorf("failed to get logs: %w", err)
		}
		defer reader.Close()

		// Stream logs to stdout
		buf := make([]byte, 1024)
		for {
			n, err := reader.Read(buf)
			if n > 0 {
				fmt.Print(string(buf[:n]))
			}
			if err != nil {
				break
			}
		}

		return nil
	},
}

func init() {
	agentLogsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Follow log output")
}

var agentStopCmd = &cobra.Command{
	Use:   "stop <agent-id>",
	Short: "Stop an agent",
	Long:  "Gracefully stop a running agent.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		agentID := api.AgentID(args[0])

		if rt == nil {
			return fmt.Errorf("runtime not initialized - run 'aether daemon' first")
		}

		logger.InfoContext(ctx, "stopping agent", "id", agentID)

		if err := rt.StopAgent(ctx, agentID, 30*time.Second); err != nil {
			return fmt.Errorf("failed to stop agent: %w", err)
		}

		fmt.Printf("Agent stopped: %s\n", agentID)
		return nil
	},
}

var agentDestroyCmd = &cobra.Command{
	Use:   "destroy <agent-id>",
	Short: "Destroy an agent",
	Long:  "Stop and remove an agent, cleaning up all resources.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		agentID := api.AgentID(args[0])

		if rt == nil {
			return fmt.Errorf("runtime not initialized - run 'aether daemon' first")
		}

		logger.InfoContext(ctx, "destroying agent", "id", agentID)

		if err := rt.DestroyAgent(ctx, agentID); err != nil {
			return fmt.Errorf("failed to destroy agent: %w", err)
		}

		fmt.Printf("Agent destroyed: %s\n", agentID)
		return nil
	},
}

var agentHealthCmd = &cobra.Command{
	Use:   "health <agent-id>",
	Short: "Check agent health",
	Long:  "Check the health status of a running agent.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		agentID := api.AgentID(args[0])

		if rt == nil {
			return fmt.Errorf("runtime not initialized - run 'aether daemon' first")
		}

		health, err := rt.GetAgentHealth(ctx, agentID)
		if err != nil {
			return fmt.Errorf("failed to check health: %w", err)
		}

		if health.Healthy {
			fmt.Printf("✓ Agent %s is healthy\n", agentID)
		} else {
			fmt.Printf("✗ Agent %s is unhealthy: %s\n", agentID, health.Message)
		}

		return nil
	},
}

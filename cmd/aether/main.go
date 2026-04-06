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

	"github.com/dnakitare/aether/internal/cli"
	"github.com/dnakitare/aether/internal/observability"
	"github.com/dnakitare/aether/internal/runtime"
	"github.com/dnakitare/aether/internal/runtime/vm"
	"github.com/dnakitare/aether/internal/state"
	"github.com/dnakitare/aether/pkg/api"
	"github.com/spf13/cobra"
)

// Build-time variables injected via -ldflags.
var (
	Version   = "dev"
	CommitSHA = "unknown"
	BuildDate = "unknown"
)

var (
	// Global flags
	logLevel   string
	configFile string

	// Runtime instance (shared across commands)
	rt     api.Runtime
	logger *slog.Logger
)

// @title           Aether Runtime API
// @version         0.2.0
// @description     Agent runtime orchestration platform — create, schedule, and manage isolated AI agents on Firecracker microVMs.
// @termsOfService  https://aether.io/terms
//
// @contact.name   Aether Support
// @contact.url    https://aether.io/support
//
// @license.name  Apache 2.0
// @license.url   http://www.apache.org/licenses/LICENSE-2.0.html
//
// @host      localhost:8080
// @BasePath  /v1
//
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and JWT token.
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
			ServiceVersion: Version,
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

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("aether %s (commit: %s, built: %s)\n", Version, CommitSHA, BuildDate)
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().StringVar(&configFile, "config", "", "Config file path")

	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(completionCmd)
	rootCmd.AddCommand(versionCmd)
}

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion script",
	Long: `Generate shell completion script for Aether.

To load completions:

Bash:
  $ source <(aether completion bash)

  # To load completions for each session, execute once:
  # Linux:
  $ aether completion bash > /etc/bash_completion.d/aether
  # macOS:
  $ aether completion bash > $(brew --prefix)/etc/bash_completion.d/aether

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it. You can execute the following once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ aether completion zsh > "${fpath[1]}/_aether"

  # You will need to start a new shell for this setup to take effect.

Fish:
  $ aether completion fish | source

  # To load completions for each session, execute once:
  $ aether completion fish > ~/.config/fish/completions/aether.fish

PowerShell:
  PS> aether completion powershell | Out-String | Invoke-Expression

  # To load completions for every new session, run:
  PS> aether completion powershell > aether.ps1
  # and source this file from your PowerShell profile.
`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return rootCmd.GenBashCompletion(os.Stdout)
		case "zsh":
			return rootCmd.GenZshCompletion(os.Stdout)
		case "fish":
			return rootCmd.GenFishCompletion(os.Stdout, true)
		case "powershell":
			return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
		default:
			return fmt.Errorf("unsupported shell: %s", args[0])
		}
	},
}

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Start the Aether runtime daemon",
	Long:  "Start the Aether runtime daemon to manage AI agents.",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		cli.Header("Aether Runtime Daemon")
		fmt.Println()

		spinner := cli.NewSpinner("Initializing runtime...")
		spinner.Start()

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

		// Optionally wire PostgreSQL when DATABASE_URL is set.
		var stateStore runtime.StateStore
		if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
			spinner.UpdateMessage("Connecting to PostgreSQL...")
			pgStore, pgErr := state.NewPostgresStore(logger, state.PostgresConfig{DSN: dsn})
			if pgErr != nil {
				spinner.Error("Failed to connect to PostgreSQL")
				cli.ErrorWithHelp(pgErr, "Check DATABASE_URL and ensure PostgreSQL is reachable")
				return pgErr
			}
			defer pgStore.Close()
			stateStore = pgStore
			logger.InfoContext(ctx, "daemon using PostgreSQL state store")
		}

		var err error
		rt, err = runtime.New(logger, config, stateStore)
		if err != nil {
			spinner.Error("Failed to initialize runtime")
			cli.ErrorWithHelp(err, "Check if Firecracker is installed and configured")
			return err
		}

		spinner.Success("Runtime initialized")
		fmt.Println()
		cli.Info("Daemon started successfully")
		cli.Dim("Press Ctrl+C to stop")
		fmt.Println()

		// Handle shutdown signals
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

		// Wait for shutdown signal
		<-sigCh

		fmt.Println()
		spinner = cli.NewSpinner("Shutting down daemon...")
		spinner.Start()

		if err := rt.Shutdown(ctx); err != nil {
			spinner.Error("Error during shutdown")
			logger.ErrorContext(ctx, "error during shutdown", "error", err)
			return err
		}

		spinner.Success("Daemon stopped")
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
	agentCmd.AddCommand(agentCheckpointCmd)
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
			cli.ErrorWithSuggestion(
				fmt.Errorf("runtime not initialized"),
				"aether daemon",
			)
			return fmt.Errorf("runtime not initialized")
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

		// Show configuration
		cli.Info("Creating agent:")
		cli.Dim("  Name:     %s", agentName)
		cli.Dim("  Image:    %s", agentImage)
		cli.Dim("  CPU:      %d cores", agentCPU)
		cli.Dim("  Memory:   %d MB", agentMemoryMB)
		cli.Dim("  Tenant:   %s", agentTenantID)
		fmt.Println()

		// Create agent with spinner
		spinner := cli.NewSpinner("Creating agent...")
		spinner.Start()

		if err := rt.CreateAgent(ctx, config); err != nil {
			spinner.Error("Failed to create agent")
			cli.ErrorWithHelp(err, "Check if the image exists and resources are available")
			return err
		}

		spinner.UpdateMessage("Starting agent...")

		if err := rt.StartAgent(ctx, agentID); err != nil {
			spinner.Error("Failed to start agent")
			cli.ErrorWithHelp(err, "Check agent logs for details")
			return err
		}

		spinner.Success(fmt.Sprintf("Agent %s created and started", agentID))
		return nil
	},
}

func init() {
	agentCreateCmd.Flags().StringVar(&agentImage, "image", "python:3.11", "Container image to run")
	agentCreateCmd.Flags().StringVar(&agentName, "name", "", "Agent name")
	agentCreateCmd.Flags().StringVar(&agentTenantID, "tenant", "default", "Tenant ID")
	agentCreateCmd.Flags().IntVar(&agentCPU, "cpu", 1, "Number of CPUs")
	agentCreateCmd.Flags().Int64Var(&agentMemoryMB, "memory", 512, "Memory in MB")
	_ = agentCreateCmd.MarkFlagRequired("name") // Error only occurs if flag doesn't exist
}

var agentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all agents",
	Long:  "List all agents managed by the runtime.",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		if rt == nil {
			cli.ErrorWithSuggestion(
				fmt.Errorf("runtime not initialized"),
				"aether daemon",
			)
			return fmt.Errorf("runtime not initialized")
		}

		agents, err := rt.ListAgents(ctx, nil)
		if err != nil {
			cli.Error("Failed to list agents: %v", err)
			return err
		}

		if len(agents) == 0 {
			cli.Info("No agents found")
			return nil
		}

		// Create and populate table
		table := cli.NewTable("ID", "NAME", "STATUS", "TENANT", "CPU", "MEMORY")
		for _, agent := range agents {
			table.AddRow(
				string(agent.Config.ID),
				agent.Config.Name,
				string(agent.Status),
				string(agent.Config.TenantID),
				fmt.Sprintf("%d", agent.Config.Resources.CPUCount),
				fmt.Sprintf("%d MB", agent.Config.Resources.MemoryMB),
			)
		}

		fmt.Println()
		table.Print()
		fmt.Println()
		cli.Dim("Total: %d agent(s)", len(agents))

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
		defer func() {
			_ = reader.Close() // Best effort close
		}()

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
			cli.ErrorWithSuggestion(
				fmt.Errorf("runtime not initialized"),
				"aether daemon",
			)
			return fmt.Errorf("runtime not initialized")
		}

		spinner := cli.NewSpinner(fmt.Sprintf("Stopping agent %s...", agentID))
		spinner.Start()

		if err := rt.StopAgent(ctx, agentID, 30*time.Second); err != nil {
			spinner.Error("Failed to stop agent")
			cli.ErrorWithHelp(err, "The agent may have already stopped or timed out")
			return err
		}

		spinner.Success(fmt.Sprintf("Agent %s stopped successfully", agentID))
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
			cli.ErrorWithSuggestion(
				fmt.Errorf("runtime not initialized"),
				"aether daemon",
			)
			return fmt.Errorf("runtime not initialized")
		}

		cli.Warn("This will permanently destroy agent %s", agentID)

		spinner := cli.NewSpinner("Destroying agent...")
		spinner.Start()

		if err := rt.DestroyAgent(ctx, agentID); err != nil {
			spinner.Error("Failed to destroy agent")
			cli.ErrorWithHelp(err, "The agent may not exist or is already destroyed")
			return err
		}

		spinner.Success(fmt.Sprintf("Agent %s destroyed successfully", agentID))
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
			cli.ErrorWithSuggestion(
				fmt.Errorf("runtime not initialized"),
				"aether daemon",
			)
			return fmt.Errorf("runtime not initialized")
		}

		spinner := cli.NewSpinner("Checking agent health...")
		spinner.Start()

		health, err := rt.GetAgentHealth(ctx, agentID)
		if err != nil {
			spinner.Error("Failed to check health")
			cli.ErrorWithHelp(err, "The agent may not exist or is not running")
			return err
		}

		spinner.Stop()

		if health.Healthy {
			cli.Success("Agent %s is healthy", agentID)
		} else {
			cli.Error("Agent %s is unhealthy", agentID)
			cli.Dim("  Reason: %s", health.Message)
		}

		return nil
	},
}

var agentCheckpointCmd = &cobra.Command{
	Use:   "checkpoint",
	Short: "Manage agent checkpoints",
	Long:  "Create, list, restore, and manage agent state checkpoints.",
}

func init() {
	agentCheckpointCmd.AddCommand(checkpointCreateCmd)
	agentCheckpointCmd.AddCommand(checkpointListCmd)
	agentCheckpointCmd.AddCommand(checkpointRestoreCmd)
	agentCheckpointCmd.AddCommand(checkpointDeleteCmd)
}

var checkpointCreateCmd = &cobra.Command{
	Use:   "create <agent-id>",
	Short: "Create a checkpoint for an agent",
	Long:  "Create a state checkpoint for the specified agent. Checkpoints can be used to restore agent state later.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		agentID := api.AgentID(args[0])

		spinner := cli.NewSpinner(fmt.Sprintf("Creating checkpoint for agent %s...", agentID))
		spinner.Start()
		defer spinner.Stop()

		// Access checkpoint methods via concrete runtime type
		concreteRT, ok := rt.(*runtime.Runtime)
		if !ok {
			spinner.Stop()
			return fmt.Errorf("checkpoint functionality not available in current runtime")
		}

		checkpoint, err := concreteRT.CreateCheckpoint(ctx, agentID)
		if err != nil {
			spinner.Stop()
			return fmt.Errorf("failed to create checkpoint: %w", err)
		}

		spinner.Stop()
		cli.Success("✓ Checkpoint created successfully")
		fmt.Println()
		table := cli.NewTable("FIELD", "VALUE")
		table.AddRow("Version", fmt.Sprintf("%d", checkpoint.Version))
		table.AddRow("Created", checkpoint.CreatedAt.Format(time.RFC3339))
		table.AddRow("Size", fmt.Sprintf("%d bytes", checkpoint.Size))
		table.AddRow("State Keys", fmt.Sprintf("%d", len(checkpoint.State)))
		table.Print()

		return nil
	},
}

var checkpointListCmd = &cobra.Command{
	Use:   "list <agent-id>",
	Short: "List checkpoints for an agent",
	Long:  "List all available checkpoints for the specified agent.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		agentID := api.AgentID(args[0])

		// Access checkpoint methods via concrete runtime type
		concreteRT, ok := rt.(*runtime.Runtime)
		if !ok {
			return fmt.Errorf("checkpoint functionality not available in current runtime")
		}

		checkpoints, err := concreteRT.ListCheckpoints(ctx, agentID)
		if err != nil {
			return fmt.Errorf("failed to list checkpoints: %w", err)
		}

		if len(checkpoints) == 0 {
			cli.Info("No checkpoints found for agent %s", agentID)
			return nil
		}

		cli.Info("Checkpoints for agent %s:", agentID)
		fmt.Println()
		table := cli.NewTable("VERSION", "CREATED", "SIZE", "STATE KEYS")
		for _, cp := range checkpoints {
			sizeStr := fmt.Sprintf("%d bytes", cp.Size)
			if cp.Size > 1024*1024 {
				sizeStr = fmt.Sprintf("%.2f MB", float64(cp.Size)/(1024*1024))
			} else if cp.Size > 1024 {
				sizeStr = fmt.Sprintf("%.2f KB", float64(cp.Size)/1024)
			}
			table.AddRow(
				fmt.Sprintf("%d", cp.Version),
				cp.CreatedAt.Format("2006-01-02 15:04:05"),
				sizeStr,
				fmt.Sprintf("%d", len(cp.State)),
			)
		}
		table.Print()

		return nil
	},
}

var (
	restoreVersion int
)

var checkpointRestoreCmd = &cobra.Command{
	Use:   "restore <agent-id>",
	Short: "Restore an agent from a checkpoint",
	Long:  "Restore an agent's state from a checkpoint. By default, restores from the latest checkpoint.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		agentID := api.AgentID(args[0])

		versionMsg := "latest checkpoint"
		if restoreVersion > 0 {
			versionMsg = fmt.Sprintf("checkpoint version %d", restoreVersion)
		}

		spinner := cli.NewSpinner(fmt.Sprintf("Restoring agent %s from %s...", agentID, versionMsg))
		spinner.Start()
		defer spinner.Stop()

		// Access checkpoint methods via concrete runtime type
		concreteRT, ok := rt.(*runtime.Runtime)
		if !ok {
			spinner.Stop()
			return fmt.Errorf("checkpoint functionality not available in current runtime")
		}

		if err := concreteRT.RestoreFromCheckpoint(ctx, agentID, restoreVersion); err != nil {
			spinner.Stop()
			return fmt.Errorf("failed to restore from checkpoint: %w", err)
		}

		spinner.Stop()
		cli.Success("✓ Agent %s restored from %s", agentID, versionMsg)
		fmt.Println()
		cli.Dim("  The agent has been stopped and restarted with the restored state")
		cli.Dim("  Any unsaved state since the checkpoint has been lost")

		return nil
	},
}

func init() {
	checkpointRestoreCmd.Flags().IntVar(&restoreVersion, "version", 0, "Checkpoint version to restore (default: latest)")
}

var checkpointDeleteCmd = &cobra.Command{
	Use:   "delete <agent-id> <version>",
	Short: "Delete a checkpoint",
	Long:  "Delete a specific checkpoint version for an agent.",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		agentID := api.AgentID(args[0])

		var versionNum int
		_, err := fmt.Sscanf(args[1], "%d", &versionNum)
		if err != nil {
			return fmt.Errorf("invalid version number: %s", args[1])
		}

		cli.Warn("⚠ Deleting checkpoint version %d for agent %s", versionNum, agentID)
		cli.Dim("  This action cannot be undone")
		fmt.Println()

		// Access checkpoint methods via concrete runtime type
		concreteRT, ok := rt.(*runtime.Runtime)
		if !ok {
			return fmt.Errorf("checkpoint functionality not available in current runtime")
		}

		spinner := cli.NewSpinner(fmt.Sprintf("Deleting checkpoint version %d...", versionNum))
		spinner.Start()
		defer spinner.Stop()

		if err := concreteRT.DeleteCheckpoint(ctx, agentID, versionNum); err != nil {
			spinner.Stop()
			return fmt.Errorf("failed to delete checkpoint: %w", err)
		}

		spinner.Stop()
		cli.Success("✓ Checkpoint version %d deleted successfully", versionNum)

		return nil
	},
}

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ecopelan/remoteclaw/internal/agent"
	"github.com/ecopelan/remoteclaw/internal/config"
	"github.com/ecopelan/remoteclaw/internal/logging"
	"github.com/ecopelan/remoteclaw/internal/service"
	"github.com/spf13/cobra"
)

var cfgPath string

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "remoteclaw",
		Short: "RemoteClaw — AI-powered remote system control via Webex",
		Long:  "RemoteClaw is a local agent that lets users remotely control a system via a Webex bot, powered by AI.",
		SilenceUsage: true,
	}

	root.PersistentFlags().StringVar(&cfgPath, "config", "config.yaml", "path to config file")

	root.AddCommand(
		newRunCmd(),
		newInstallCmd(),
		newUninstallCmd(),
		newStatusCmd(),
		newVersionCmd(),
	)

	return root
}

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Start the RemoteClaw agent in the foreground",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			if err := logging.Setup(cfg.Logging.Level, cfg.Logging.Format, cfg.Logging.File); err != nil {
				return fmt.Errorf("setting up logging: %w", err)
			}
			defer func() { _ = logging.Close() }()

			log := logging.Get()
			log.Info().
				Str("mode", cfg.Mode).
				Str("version", config.Version).
				Msg("starting RemoteClaw agent")

			a, err := agent.New(cfg)
			if err != nil {
				return fmt.Errorf("creating agent: %w", err)
			}

			return a.Run(cmd.Context())
		},
	}
}

func newInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install RemoteClaw as a system service",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Resolve config path to absolute path
			absConfigPath, err := filepath.Abs(cfgPath)
			if err != nil {
				return fmt.Errorf("resolving config path: %w", err)
			}

			// Get current executable path
			binPath, err := os.Executable()
			if err != nil {
				return fmt.Errorf("getting executable path: %w", err)
			}

			// Create service manager
			mgr, err := service.New(service.Config{
				Name:        "remoteclaw",
				DisplayName: "RemoteClaw Agent",
				Description: "RemoteClaw — AI-powered remote system control via Webex",
				ConfigPath:  absConfigPath,
				BinaryPath:  binPath,
			})
			if err != nil {
				return fmt.Errorf("creating service manager: %w", err)
			}

			// Install the service
			if err := mgr.Install(); err != nil {
				return fmt.Errorf("installing service: %w", err)
			}

			fmt.Println("RemoteClaw service installed successfully")

			// Start the service
			if err := mgr.Start(); err != nil {
				return fmt.Errorf("starting service: %w", err)
			}

			fmt.Println("RemoteClaw service started successfully")
			return nil
		},
	}
}

func newUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall the RemoteClaw system service",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Resolve config path to absolute path (needed even though we're stopping/uninstalling)
			absConfigPath, err := filepath.Abs(cfgPath)
			if err != nil {
				return fmt.Errorf("resolving config path: %w", err)
			}

			// Get current executable path
			binPath, err := os.Executable()
			if err != nil {
				return fmt.Errorf("getting executable path: %w", err)
			}

			// Create service manager
			mgr, err := service.New(service.Config{
				Name:        "remoteclaw",
				DisplayName: "RemoteClaw Agent",
				Description: "RemoteClaw — AI-powered remote system control via Webex",
				ConfigPath:  absConfigPath,
				BinaryPath:  binPath,
			})
			if err != nil {
				return fmt.Errorf("creating service manager: %w", err)
			}

			// Try to stop the service (ignore errors - it might not be running)
			_ = mgr.Stop()

			// Uninstall the service
			if err := mgr.Uninstall(); err != nil {
				return fmt.Errorf("uninstalling service: %w", err)
			}

			fmt.Println("RemoteClaw service uninstalled successfully")
			return nil
		},
	}
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show RemoteClaw service status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Resolve config path to absolute path
			absConfigPath, err := filepath.Abs(cfgPath)
			if err != nil {
				return fmt.Errorf("resolving config path: %w", err)
			}

			// Get current executable path
			binPath, err := os.Executable()
			if err != nil {
				return fmt.Errorf("getting executable path: %w", err)
			}

			// Create service manager
			mgr, err := service.New(service.Config{
				Name:        "remoteclaw",
				DisplayName: "RemoteClaw Agent",
				Description: "RemoteClaw — AI-powered remote system control via Webex",
				ConfigPath:  absConfigPath,
				BinaryPath:  binPath,
			})
			if err != nil {
				return fmt.Errorf("creating service manager: %w", err)
			}

			// Get service status
			status, err := mgr.Status()
			if err != nil {
				return fmt.Errorf("getting service status: %w", err)
			}

			fmt.Printf("RemoteClaw service status: %s\n", status)
			return nil
		},
	}
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print RemoteClaw version information",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("remoteclaw %s (commit: %s, built: %s)\n", config.Version, config.Commit, config.Date)
		},
	}
}

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/3rg0n/remoteclaw/internal/agent"
	"github.com/3rg0n/remoteclaw/internal/config"
	"github.com/3rg0n/remoteclaw/internal/logging"
	"github.com/3rg0n/remoteclaw/internal/security"
	"github.com/3rg0n/remoteclaw/internal/service"
	"github.com/spf13/cobra"
)

var cfgPath string
var svcUser string
var svcSystem bool

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:          "remoteclaw",
		Short:        "RemoteClaw — AI-powered remote system control via Webex",
		Long:         "RemoteClaw is a local agent that lets users remotely control a system via a Webex bot, powered by AI.",
		SilenceUsage: true,
	}

	root.PersistentFlags().StringVar(&cfgPath, "config", "config.yaml", "path to config file")

	root.AddCommand(
		newRunCmd(),
		newInstallCmd(),
		newUninstallCmd(),
		newStatusCmd(),
		newVersionCmd(),
		newEncryptChallengeCmd(),
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
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install RemoteClaw to run automatically (per-user by default; --system for headless)",
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

			// Default to a per-user service (runs as the installing user, ADR
			// 0004). --system opts into a system-wide service for headless use.
			mode := service.ModeUser
			if svcSystem {
				mode = service.ModeSystem
			}

			// Create service manager
			mgr, err := service.New(service.Config{
				Name:        "remoteclaw",
				DisplayName: "RemoteClaw Agent",
				Description: "RemoteClaw — AI-powered remote system control via Webex",
				ConfigPath:  absConfigPath,
				BinaryPath:  binPath,
				Mode:        mode,
				UserName:    svcUser,
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
	cmd.Flags().BoolVar(&svcSystem, "system", false, "install a system-wide service for headless/always-on use (default: per-user service running as the installing user)")
	cmd.Flags().StringVar(&svcUser, "user", "", "with --system: OS account the system service runs as (empty = platform default)")
	return cmd
}

// serviceManagerForMode builds a service manager whose Mode matches the
// install mode. kardianos resolves the service location (user vs system unit)
// from the same Option set used at install time, so uninstall/status MUST pass
// the same --system flag that install did, or they look in the wrong place.
func serviceManagerForMode() (*service.Manager, error) {
	absConfigPath, err := filepath.Abs(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("resolving config path: %w", err)
	}
	binPath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("getting executable path: %w", err)
	}
	mode := service.ModeUser
	if svcSystem {
		mode = service.ModeSystem
	}
	return service.New(service.Config{
		Name:        "remoteclaw",
		DisplayName: "RemoteClaw Agent",
		Description: "RemoteClaw — AI-powered remote system control via Webex",
		ConfigPath:  absConfigPath,
		BinaryPath:  binPath,
		Mode:        mode,
		UserName:    svcUser,
	})
}

func newUninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall the RemoteClaw service (use --system if it was installed with --system)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mgr, err := serviceManagerForMode()
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
	cmd.Flags().BoolVar(&svcSystem, "system", false, "the service was installed as a system-wide service (--system at install time)")
	return cmd
}

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show RemoteClaw service status (use --system if it was installed with --system)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Create service manager
			mgr, err := serviceManagerForMode()
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
	cmd.Flags().BoolVar(&svcSystem, "system", false, "the service was installed as a system-wide service (--system at install time)")
	return cmd
}

func newEncryptChallengeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "encrypt-challenge <passphrase>",
		Short: "Encrypt a challenge passphrase into the CHALLENGE ciphertext",
		Long: "Produces the AES-256-GCM ciphertext to set as the CHALLENGE value. " +
			"The passphrase itself is never stored — only this ciphertext. Used by the installer.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			ciphertext, err := security.EncryptChallenge(args[0])
			if err != nil {
				return fmt.Errorf("encrypting challenge: %w", err)
			}
			// Print only the ciphertext to stdout so the installer can capture it.
			fmt.Println(ciphertext)
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

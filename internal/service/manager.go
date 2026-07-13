package service

import (
	"fmt"
	"os"
	"runtime"

	"github.com/kardianos/service"
)

// InstallMode selects how RemoteClaw is installed to run.
type InstallMode string

const (
	// ModeUser installs a per-user service that runs as the installing user
	// (systemd --user on Linux, a LaunchAgent on macOS). This is the default
	// and matches ADR 0004: RemoteClaw runs with the installing user's
	// privileges. Not supported on Windows via this manager — the Windows
	// installer uses a run-at-login Scheduled Task instead.
	ModeUser InstallMode = "user"
	// ModeSystem installs a system-wide service (systemd system unit, launchd
	// daemon, Windows service), optionally under a dedicated account via
	// UserName. For headless / always-on deployments.
	ModeSystem InstallMode = "system"
)

// Config holds service installation configuration
type Config struct {
	Name        string      // service name
	DisplayName string      // human-readable display name
	Description string      // service description
	ConfigPath  string      // path to config file
	BinaryPath  string      // path to binary (empty = current executable)
	Mode        InstallMode // user (default) or system; see InstallMode
	UserName    string      // for ModeSystem: OS account to run the service as (empty = platform default)
}

// Manager wraps kardianos/service for service management
type Manager struct {
	svc service.Service
}

// program implements the service.Interface for kardianos/service
type program struct{}

// Start implements service.Interface
func (p *program) Start(s service.Service) error {
	// Service start is handled by the "run" subcommand
	// The service manager just launches the binary with "run" args
	return nil
}

// Stop implements service.Interface
func (p *program) Stop(s service.Service) error {
	// Stop is handled via OS signals (SIGTERM/SIGINT)
	return nil
}

// New creates a Manager with the given configuration.
// If BinaryPath is empty, uses the current executable.
func New(cfg Config) (*Manager, error) {
	// Set defaults
	if cfg.Name == "" {
		cfg.Name = "remoteclaw"
	}
	if cfg.DisplayName == "" {
		cfg.DisplayName = "RemoteClaw Agent"
	}
	if cfg.Description == "" {
		cfg.Description = "RemoteClaw — AI-powered remote system control via Webex"
	}
	if cfg.Mode == "" {
		cfg.Mode = ModeUser // secure/default per ADR 0004: run as the installing user
	}

	// Windows has no per-user service concept in kardianos; the Windows
	// installer uses a run-at-login Scheduled Task for the user path instead.
	if cfg.Mode == ModeUser && runtime.GOOS == "windows" {
		return nil, fmt.Errorf("user-mode service install is not supported on Windows; " +
			"use the run-at-login task set up by install.ps1, or install --system")
	}

	// Get binary path
	binPath := cfg.BinaryPath
	if binPath == "" {
		var err error
		binPath, err = os.Executable()
		if err != nil {
			return nil, fmt.Errorf("getting executable path: %w", err)
		}
	}

	// Options: for user mode, install as a current-user service (systemd
	// --user / launchd LaunchAgent), which runs as the installing user.
	options := service.KeyValue{}
	if cfg.Mode == ModeUser {
		options["UserService"] = true
	}

	// Create service configuration
	svcCfg := &service.Config{
		Name:        cfg.Name,
		DisplayName: cfg.DisplayName,
		Description: cfg.Description,
		Executable:  binPath,
		Arguments: []string{
			"run",
			"--config", cfg.ConfigPath,
		},
		Option: options,
	}
	// UserName only applies to a system service running under a dedicated
	// account. A user service already runs as the installing user.
	if cfg.Mode == ModeSystem {
		svcCfg.UserName = cfg.UserName
	}

	// Create service
	svc, err := service.New(&program{}, svcCfg)
	if err != nil {
		return nil, fmt.Errorf("creating service: %w", err)
	}

	return &Manager{
		svc: svc,
	}, nil
}

// Install installs the service on the system
func (m *Manager) Install() error {
	if err := m.svc.Install(); err != nil {
		return fmt.Errorf("installing service: %w", err)
	}
	return nil
}

// Uninstall uninstalls the service from the system
func (m *Manager) Uninstall() error {
	if err := m.svc.Uninstall(); err != nil {
		return fmt.Errorf("uninstalling service: %w", err)
	}
	return nil
}

// Start starts the service
func (m *Manager) Start() error {
	if err := m.svc.Start(); err != nil {
		return fmt.Errorf("starting service: %w", err)
	}
	return nil
}

// Stop stops the service
func (m *Manager) Stop() error {
	if err := m.svc.Stop(); err != nil {
		return fmt.Errorf("stopping service: %w", err)
	}
	return nil
}

// Status returns the current service status as a human-readable string
func (m *Manager) Status() (string, error) {
	status, err := m.svc.Status()
	if err != nil {
		return "unknown", fmt.Errorf("getting service status: %w", err)
	}

	switch status {
	case service.StatusRunning:
		return "running", nil
	case service.StatusStopped:
		return "stopped", nil
	default:
		return "unknown", nil
	}
}

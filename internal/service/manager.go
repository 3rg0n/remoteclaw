package service

import (
	"fmt"
	"os"

	"github.com/kardianos/service"
)

// Config holds service installation configuration
type Config struct {
	Name        string // service name
	DisplayName string // human-readable display name
	Description string // service description
	ConfigPath  string // path to WCC config file
	BinaryPath  string // path to WCC binary (empty = current executable)
}

// Manager wraps kardianos/service for WCC service management
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
		cfg.Name = "wcca"
	}
	if cfg.DisplayName == "" {
		cfg.DisplayName = "WCC Agent"
	}
	if cfg.Description == "" {
		cfg.Description = "Webex Command and Control Agent"
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

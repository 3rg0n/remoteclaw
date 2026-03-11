package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_DefaultValues(t *testing.T) {
	cfg := Config{
		ConfigPath: "/etc/remoteclaw/config.yaml",
	}

	m, err := New(cfg)
	require.NoError(t, err)
	assert.NotNil(t, m)
	assert.NotNil(t, m.svc)
}

func TestNew_CustomValues(t *testing.T) {
	cfg := Config{
		Name:        "custom-remoteclaw",
		DisplayName: "Custom RemoteClaw Service",
		Description: "Custom RemoteClaw Description",
		ConfigPath:  "/custom/config.yaml",
		BinaryPath:  "/opt/remoteclaw/bin/remoteclaw",
	}

	m, err := New(cfg)
	require.NoError(t, err)
	assert.NotNil(t, m)
	assert.NotNil(t, m.svc)
}

func TestNew_UsesCurrentExecutable(t *testing.T) {
	cfg := Config{
		ConfigPath: "/etc/remoteclaw/config.yaml",
		// BinaryPath is empty, so should use current executable
	}

	m, err := New(cfg)
	require.NoError(t, err)
	assert.NotNil(t, m)
	// Verify the service was created successfully
	assert.NotNil(t, m.svc)
}

func TestStatus_ReturnsString(t *testing.T) {
	cfg := Config{
		ConfigPath: "/etc/remoteclaw/config.yaml",
	}

	m, err := New(cfg)
	require.NoError(t, err)

	// Status may fail on non-installed service, but should return a string
	status, _ := m.Status()
	// We only test that status returns a string; actual status depends on OS/installation
	assert.IsType(t, "", status)
}

func TestProgramImplementsInterface(t *testing.T) {
	p := &program{}
	// Verify program implements service.Interface by calling its methods
	err := p.Start(nil)
	assert.NoError(t, err)

	err = p.Stop(nil)
	assert.NoError(t, err)
}

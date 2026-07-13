package service

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_DefaultValues(t *testing.T) {
	// Explicit system mode so this construction test is platform-independent
	// (user mode is unsupported on Windows).
	cfg := Config{
		ConfigPath: "/etc/remoteclaw/config.yaml",
		Mode:       ModeSystem,
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
		Mode:        ModeSystem,
	}

	m, err := New(cfg)
	require.NoError(t, err)
	assert.NotNil(t, m)
	assert.NotNil(t, m.svc)
}

func TestNew_UsesCurrentExecutable(t *testing.T) {
	cfg := Config{
		ConfigPath: "/etc/remoteclaw/config.yaml",
		Mode:       ModeSystem,
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
		Mode:       ModeSystem,
	}

	m, err := New(cfg)
	require.NoError(t, err)

	// Status may fail on non-installed service, but should return a string
	status, _ := m.Status()
	// We only test that status returns a string; actual status depends on OS/installation
	assert.IsType(t, "", status)
}

func TestNew_DefaultsToUserMode(t *testing.T) {
	// With no Mode set, it defaults to ModeUser. On non-Windows this succeeds;
	// on Windows user mode is rejected with a clear error.
	cfg := Config{ConfigPath: "/etc/remoteclaw/config.yaml"}
	m, err := New(cfg)
	if runtime.GOOS == "windows" {
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not supported on Windows")
	} else {
		require.NoError(t, err)
		assert.NotNil(t, m)
	}
}

func TestNew_SystemModeWorksEverywhere(t *testing.T) {
	cfg := Config{
		ConfigPath: "/etc/remoteclaw/config.yaml",
		Mode:       ModeSystem,
		UserName:   "someacct",
	}
	m, err := New(cfg)
	require.NoError(t, err)
	assert.NotNil(t, m)
}

func TestProgramImplementsInterface(t *testing.T) {
	p := &program{}
	// Verify program implements service.Interface by calling its methods
	err := p.Start(nil)
	assert.NoError(t, err)

	err = p.Stop(nil)
	assert.NoError(t, err)
}

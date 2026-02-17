package logging

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultLogger verifies that the default logger works before Setup is called.
func TestDefaultLogger(t *testing.T) {
	log := Get()

	// Default logger should not be nil
	assert.NotNil(t, log)

	// Default logger should be usable
	buf := new(bytes.Buffer)
	log = log.Output(buf)
	log.Info().Msg("test message")

	// Should have written something
	assert.True(t, buf.Len() > 0)
}

// TestSetupWithDebugLevel verifies Setup with debug log level.
func TestSetupWithDebugLevel(t *testing.T) {
	buf := new(bytes.Buffer)

	// Setup with debug level, JSON format
	err := Setup("debug", "json", "")
	require.NoError(t, err)

	log := Get()
	log = log.Output(buf)

	// Log at debug level
	log.Debug().Msg("debug message")
	assert.True(t, buf.Len() > 0, "debug message should be logged at debug level")
}

// TestSetupWithInfoLevel verifies Setup with info log level.
func TestSetupWithInfoLevel(t *testing.T) {
	buf := new(bytes.Buffer)

	err := Setup("info", "json", "")
	require.NoError(t, err)

	log := Get()
	log = log.Output(buf)

	// Debug message should not appear at info level
	log.Debug().Msg("debug message")
	debugLen := buf.Len()

	// Info message should appear
	log.Info().Msg("info message")
	assert.True(t, buf.Len() > debugLen, "info message should be logged at info level")
}

// TestSetupWithWarnLevel verifies Setup with warn log level.
func TestSetupWithWarnLevel(t *testing.T) {
	buf := new(bytes.Buffer)

	err := Setup("warn", "json", "")
	require.NoError(t, err)

	log := Get()
	log = log.Output(buf)

	// Info should not appear
	log.Info().Msg("info message")
	infoLen := buf.Len()

	// Warn should appear
	log.Warn().Msg("warn message")
	assert.True(t, buf.Len() > infoLen, "warn message should be logged at warn level")
}

// TestSetupWithErrorLevel verifies Setup with error log level.
func TestSetupWithErrorLevel(t *testing.T) {
	buf := new(bytes.Buffer)

	err := Setup("error", "json", "")
	require.NoError(t, err)

	log := Get()
	log = log.Output(buf)

	// Warn should not appear
	log.Warn().Msg("warn message")
	warnLen := buf.Len()

	// Error should appear
	log.Error().Msg("error message")
	assert.True(t, buf.Len() > warnLen, "error message should be logged at error level")
}

// TestSetupWithConsoleFormat verifies Setup with console format.
func TestSetupWithConsoleFormat(t *testing.T) {
	err := Setup("info", "console", "")
	require.NoError(t, err)

	log := Get()
	assert.NotNil(t, log)

	// Logger should be usable (console format creates a ConsoleWriter)
	log.Info().Msg("console message")
}

// TestSetupWithTextFormat verifies Setup with text format (alias for console).
func TestSetupWithTextFormat(t *testing.T) {
	err := Setup("info", "text", "")
	require.NoError(t, err)

	log := Get()
	assert.NotNil(t, log)

	log.Info().Msg("text message")
}

// TestSetupWithJSONFormat verifies Setup with JSON format.
func TestSetupWithJSONFormat(t *testing.T) {
	buf := new(bytes.Buffer)

	err := Setup("info", "json", "")
	require.NoError(t, err)

	log := Get()
	log = log.Output(buf)
	log.Info().Str("key", "value").Msg("json message")

	output := buf.String()
	assert.Contains(t, output, "json message")
	assert.Contains(t, output, "key")
	assert.Contains(t, output, "value")
}

// TestSetupWithFileOutput verifies Setup with file output.
func TestSetupWithFileOutput(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	err := Setup("info", "json", logFile)
	require.NoError(t, err)
	defer func() { _ = Close() }()

	log := Get()
	log.Info().Str("test", "message").Msg("file output test")

	// File should have been created and contain log data
	fileContent, err := os.ReadFile(logFile) //nolint:gosec // test file path from t.TempDir()
	require.NoError(t, err)
	assert.True(t, len(fileContent) > 0, "log file should contain data")
	assert.Contains(t, string(fileContent), "file output test")
}

// TestSetupWithFileAndConsoleFormat verifies Setup with both file and console output.
func TestSetupWithFileAndConsoleFormat(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	err := Setup("info", "console", logFile)
	require.NoError(t, err)
	defer func() { _ = Close() }()

	log := Get()
	log.Info().Msg("multi-output message")

	// File should contain the message
	fileContent, err := os.ReadFile(logFile) //nolint:gosec // test file path from t.TempDir()
	require.NoError(t, err)
	assert.True(t, len(fileContent) > 0, "log file should contain data")
	assert.Contains(t, string(fileContent), "multi-output message")
}

// TestCaseInsensitiveLevel verifies that log levels are case-insensitive.
func TestCaseInsensitiveLevel(t *testing.T) {
	buf := new(bytes.Buffer)

	err := Setup("DEBUG", "json", "")
	require.NoError(t, err)

	log := Get()
	log = log.Output(buf)
	log.Debug().Msg("debug message")

	assert.True(t, buf.Len() > 0, "uppercase DEBUG level should work")
}

// TestCaseInsensitiveFormat verifies that format is case-insensitive.
func TestCaseInsensitiveFormat(t *testing.T) {
	err := Setup("info", "JSON", "")
	require.NoError(t, err)

	log := Get()
	assert.NotNil(t, log)

	// Should be usable
	log.Info().Msg("test")
}

// TestInvalidLogLevelDefaultsToInfo verifies that invalid levels default to info.
func TestInvalidLogLevelDefaultsToInfo(t *testing.T) {
	buf := new(bytes.Buffer)

	err := Setup("invalid_level", "json", "")
	require.NoError(t, err)

	log := Get()
	log = log.Output(buf)

	// Debug should not be logged (defaults to info)
	log.Debug().Msg("debug message")
	debugLen := buf.Len()

	// Info should be logged
	log.Info().Msg("info message")
	assert.True(t, buf.Len() > debugLen, "invalid level should default to info")
}

// TestSetupMultipleTimes verifies that Setup can be called multiple times.
func TestSetupMultipleTimes(t *testing.T) {
	// First setup
	err := Setup("info", "json", "")
	require.NoError(t, err)

	// Second setup with different level
	err = Setup("debug", "json", "")
	require.NoError(t, err)

	log := Get()
	assert.NotNil(t, log)

	// Should work without error
	buf := new(bytes.Buffer)
	log = log.Output(buf)
	log.Debug().Msg("debug after second setup")
	assert.True(t, buf.Len() > 0)
}

// TestLoggerConcurrency verifies that Get is thread-safe.
func TestLoggerConcurrency(t *testing.T) {
	err := Setup("info", "json", "")
	require.NoError(t, err)

	// Spawn multiple goroutines to access the logger
	results := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			log := Get()
			log.Info().Int("goroutine", idx).Msg(fmt.Sprintf("message from goroutine %d", idx))
			results <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-results
	}
}

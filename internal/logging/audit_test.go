package logging

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAuditLogger_EmptyPath(t *testing.T) {
	logger, err := NewAuditLogger("")
	assert.NoError(t, err)
	assert.Nil(t, logger)
}

func TestNewAuditLogger_CreatesFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "audit.log")

	logger, err := NewAuditLogger(filePath)
	require.NoError(t, err)
	require.NotNil(t, logger)
	defer func() {
		err := logger.Close()
		assert.NoError(t, err)
	}()

	// Verify file was created as a regular file
	info, err := os.Stat(filePath)
	require.NoError(t, err)
	assert.True(t, info.Mode().IsRegular())
}

func TestAuditLogger_Log(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "audit.log")

	logger, err := NewAuditLogger(filePath)
	require.NoError(t, err)
	require.NotNil(t, logger)
	defer func() {
		err := logger.Close()
		assert.NoError(t, err)
	}()

	// Log an entry
	entry := AuditEntry{
		Timestamp:  time.Date(2026, 2, 17, 12, 30, 45, 0, time.UTC),
		Email:      "user@example.com",
		SpaceID:    "space123",
		RawMessage: "restart service",
		ToolCalls:  []string{"execute_command", "get_status"},
		Response:   "Service restarted successfully",
		Duration:   1500 * time.Millisecond,
		Error:      "",
	}

	logger.Log(entry)

	// Read and verify the file
	content, err := os.ReadFile(filePath) //nolint:gosec // filePath is from t.TempDir()
	require.NoError(t, err)
	assert.NotEmpty(t, content)

	// Parse JSON line
	var logEntry map[string]interface{}
	err = json.Unmarshal(content, &logEntry)
	require.NoError(t, err)

	assert.Equal(t, "user@example.com", logEntry["email"])
	assert.Equal(t, "space123", logEntry["space_id"])
	assert.Equal(t, "restart service", logEntry["raw_message"])
	assert.Equal(t, "Service restarted successfully", logEntry["response"])
	assert.Equal(t, float64(1500), logEntry["duration_ms"])
	assert.Equal(t, "audit", logEntry["message"])
}

func TestAuditLogger_LogWithError(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "audit.log")

	logger, err := NewAuditLogger(filePath)
	require.NoError(t, err)
	require.NotNil(t, logger)
	defer func() {
		err := logger.Close()
		assert.NoError(t, err)
	}()

	// Log an entry with error
	entry := AuditEntry{
		Timestamp:  time.Now(),
		Email:      "user@example.com",
		SpaceID:    "space123",
		RawMessage: "invalid command",
		ToolCalls:  []string{},
		Response:   "",
		Duration:   100 * time.Millisecond,
		Error:      "command not recognized",
	}

	logger.Log(entry)

	// Read and verify the file
	content, err := os.ReadFile(filePath) //nolint:gosec // filePath is from t.TempDir()
	require.NoError(t, err)

	var logEntry map[string]interface{}
	err = json.Unmarshal(content, &logEntry)
	require.NoError(t, err)

	assert.Equal(t, "command not recognized", logEntry["error"])
}

func TestAuditLogger_Close(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "audit.log")

	logger, err := NewAuditLogger(filePath)
	require.NoError(t, err)
	require.NotNil(t, logger)

	err = logger.Close()
	assert.NoError(t, err)

	// Second close should not error
	err = logger.Close()
	assert.NoError(t, err)
}

func TestAuditLogger_CloseNil(t *testing.T) {
	var logger *AuditLogger
	err := logger.Close()
	assert.NoError(t, err)
}

func TestAuditLogger_LogNil(t *testing.T) {
	var logger *AuditLogger
	entry := AuditEntry{
		Email:   "test@example.com",
		Response: "test",
	}
	// Should not panic
	logger.Log(entry)
}

func TestAuditLogger_MultipleEntries(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "audit.log")

	logger, err := NewAuditLogger(filePath)
	require.NoError(t, err)
	require.NotNil(t, logger)
	defer func() {
		err := logger.Close()
		assert.NoError(t, err)
	}()

	// Log multiple entries
	for i := 0; i < 3; i++ {
		entry := AuditEntry{
			Timestamp:  time.Now(),
			Email:      "user@example.com",
			SpaceID:    "space123",
			RawMessage: "command",
			Response:   "success",
			Duration:   100 * time.Millisecond,
		}
		logger.Log(entry)
	}

	// Verify file contains entries
	content, err := os.ReadFile(filePath) //nolint:gosec // filePath is from t.TempDir()
	require.NoError(t, err)
	assert.NotEmpty(t, content)
}

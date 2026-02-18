package logging

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// todayFile returns the expected audit log filename for today's date.
func todayFile(basePath string) string {
	return fmt.Sprintf("%s-%s.jsonl", basePath, time.Now().Format(auditDateFormat))
}

func TestNewAuditLogger_EmptyPath(t *testing.T) {
	logger, err := NewAuditLogger("")
	assert.NoError(t, err)
	assert.Nil(t, logger)
}

func TestNewAuditLogger_CreatesFile(t *testing.T) {
	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "audit")

	logger, err := NewAuditLogger(basePath)
	require.NoError(t, err)
	require.NotNil(t, logger)
	defer func() {
		err := logger.Close()
		assert.NoError(t, err)
	}()

	// Verify the date-stamped file was created
	expected := todayFile(basePath)
	info, err := os.Stat(expected)
	require.NoError(t, err)
	assert.True(t, info.Mode().IsRegular())
}

func TestNewAuditLogger_StripsExtension(t *testing.T) {
	tmpDir := t.TempDir()

	for _, ext := range []string{".jsonl", ".log", ".json"} {
		t.Run(ext, func(t *testing.T) {
			basePath := filepath.Join(tmpDir, "audit"+ext)
			logger, err := NewAuditLogger(basePath)
			require.NoError(t, err)
			require.NotNil(t, logger)
			defer func() { _ = logger.Close() }()

			// File should be named with the stripped base, not double extension
			stripped := filepath.Join(tmpDir, "audit")
			expected := todayFile(stripped)
			_, err = os.Stat(expected)
			require.NoError(t, err)
		})
	}
}

func TestAuditLogger_Log(t *testing.T) {
	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "audit")

	logger, err := NewAuditLogger(basePath)
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

	// Read and verify the date-stamped file
	content, err := os.ReadFile(todayFile(basePath)) //nolint:gosec // test path
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
	basePath := filepath.Join(tmpDir, "audit")

	logger, err := NewAuditLogger(basePath)
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

	// Read and verify the date-stamped file
	content, err := os.ReadFile(todayFile(basePath)) //nolint:gosec // test path
	require.NoError(t, err)

	var logEntry map[string]interface{}
	err = json.Unmarshal(content, &logEntry)
	require.NoError(t, err)

	assert.Equal(t, "command not recognized", logEntry["error"])
}

func TestAuditLogger_Close(t *testing.T) {
	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "audit")

	logger, err := NewAuditLogger(basePath)
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
		Email:    "test@example.com",
		Response: "test",
	}
	// Should not panic
	logger.Log(entry)
}

func TestAuditLogger_MultipleEntries(t *testing.T) {
	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "audit")

	logger, err := NewAuditLogger(basePath)
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
			RawMessage: fmt.Sprintf("command-%d", i),
			Response:   "success",
			Duration:   100 * time.Millisecond,
		}
		logger.Log(entry)
	}

	// Verify file contains 3 NDJSON lines
	file, err := os.Open(todayFile(basePath)) //nolint:gosec // test path
	require.NoError(t, err)
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	lineCount := 0
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var entry map[string]interface{}
		err := json.Unmarshal([]byte(line), &entry)
		require.NoError(t, err, "each line should be valid JSON")
		lineCount++
	}
	require.NoError(t, scanner.Err())
	assert.Equal(t, 3, lineCount, "should have 3 NDJSON lines")
}

func TestAuditLogger_CleanupOldFiles(t *testing.T) {
	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "audit")

	// Create some fake old audit files (older than 30 days)
	oldDate := time.Now().AddDate(0, 0, -35).Format(auditDateFormat)
	oldFile := fmt.Sprintf("%s-%s.jsonl", basePath, oldDate)
	err := os.WriteFile(oldFile, []byte(`{"test":"old"}`+"\n"), 0600)
	require.NoError(t, err)

	// Create a recent file (within 30 days)
	recentDate := time.Now().AddDate(0, 0, -5).Format(auditDateFormat)
	recentFile := fmt.Sprintf("%s-%s.jsonl", basePath, recentDate)
	err = os.WriteFile(recentFile, []byte(`{"test":"recent"}`+"\n"), 0600)
	require.NoError(t, err)

	// Creating the logger triggers cleanup on startup
	logger, err := NewAuditLogger(basePath)
	require.NoError(t, err)
	defer func() { _ = logger.Close() }()

	// Old file should be deleted
	_, err = os.Stat(oldFile)
	assert.True(t, os.IsNotExist(err), "old audit file should be deleted")

	// Recent file should still exist
	_, err = os.Stat(recentFile)
	assert.NoError(t, err, "recent audit file should not be deleted")

	// Today's file should exist
	_, err = os.Stat(todayFile(basePath))
	assert.NoError(t, err, "today's file should be created")
}

package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

const (
	// auditRetentionDays is the number of days to keep audit log files.
	auditRetentionDays = 30
	// auditDateFormat is the date format used in audit log filenames.
	auditDateFormat = "2006-01-02"
)

// AuditEntry represents a single audit log entry
type AuditEntry struct {
	Timestamp  time.Time     `json:"timestamp"`
	Email      string        `json:"email"`
	SpaceID    string        `json:"space_id"`
	RawMessage string        `json:"raw_message"`
	ToolCalls  []string      `json:"tool_calls,omitempty"`
	Response   string        `json:"response"`
	Duration   time.Duration `json:"duration_ms"`
	Error      string        `json:"error,omitempty"`
}

// AuditLogger writes structured NDJSON audit entries with daily rotation and 30-day retention.
// Files are named <base>-YYYY-MM-DD.jsonl (e.g., audit-2026-02-18.jsonl).
type AuditLogger struct {
	basePath string // e.g., "/var/log/wcc/audit" → files are audit-2026-02-18.jsonl
	dir      string
	prefix   string
	logger   zerolog.Logger
	file     *os.File
	fileDate string // date of currently open file
	mu       sync.Mutex
}

// NewAuditLogger creates an audit logger that writes NDJSON to date-stamped files.
// basePath is the path without extension (e.g., "audit" or "/var/log/wcc/audit").
// Files are created as <basePath>-YYYY-MM-DD.jsonl.
// If basePath is empty, returns nil (audit logging disabled).
func NewAuditLogger(basePath string) (*AuditLogger, error) {
	if basePath == "" {
		return nil, nil
	}

	// Strip .jsonl or .log extension if provided
	basePath = strings.TrimSuffix(basePath, ".jsonl")
	basePath = strings.TrimSuffix(basePath, ".log")
	basePath = strings.TrimSuffix(basePath, ".json")

	dir := filepath.Dir(basePath)
	prefix := filepath.Base(basePath)

	// Ensure directory exists
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create audit log directory: %w", err)
	}

	al := &AuditLogger{
		basePath: basePath,
		dir:      dir,
		prefix:   prefix,
	}

	// Open today's file
	if err := al.rotateIfNeeded(); err != nil {
		return nil, err
	}

	// Clean up old files on startup
	al.cleanupOldFiles()

	return al, nil
}

// Log writes an audit entry as an NDJSON line. Rotates the file if the date has changed.
func (al *AuditLogger) Log(entry AuditEntry) {
	if al == nil {
		return
	}

	al.mu.Lock()
	defer al.mu.Unlock()

	// Rotate if we've crossed into a new day
	if err := al.rotateIfNeeded(); err != nil {
		// If rotation fails, attempt to log to the current file anyway
		_ = err
	}

	al.logger.Info().
		Time("timestamp", entry.Timestamp).
		Str("email", entry.Email).
		Str("space_id", entry.SpaceID).
		Str("raw_message", entry.RawMessage).
		Strs("tool_calls", entry.ToolCalls).
		Str("response", entry.Response).
		Int64("duration_ms", entry.Duration.Milliseconds()).
		Str("error", entry.Error).
		Msg("audit")
}

// Close closes the current file handle.
func (al *AuditLogger) Close() error {
	if al == nil {
		return nil
	}

	al.mu.Lock()
	defer al.mu.Unlock()

	if al.file != nil {
		err := al.file.Close()
		al.file = nil
		return err
	}
	return nil
}

// rotateIfNeeded opens a new file if the date has changed. Must be called with mu held.
func (al *AuditLogger) rotateIfNeeded() error {
	today := time.Now().Format(auditDateFormat)
	if al.fileDate == today && al.file != nil {
		return nil
	}

	// Close the old file
	if al.file != nil {
		_ = al.file.Close()
	}

	// Open new file
	filename := fmt.Sprintf("%s-%s.jsonl", al.basePath, today)
	fileOut, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600) //nolint:gosec // path from trusted config
	if err != nil {
		return fmt.Errorf("failed to open audit file %s: %w", filename, err)
	}

	al.file = fileOut
	al.fileDate = today
	al.logger = zerolog.New(fileOut).With().Timestamp().Logger()

	return nil
}

// cleanupOldFiles removes audit files older than auditRetentionDays.
func (al *AuditLogger) cleanupOldFiles() {
	entries, err := os.ReadDir(al.dir)
	if err != nil {
		return
	}

	cutoff := time.Now().AddDate(0, 0, -auditRetentionDays)
	prefix := al.prefix + "-"
	suffix := ".jsonl"

	var toDelete []string
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}

		// Extract date from filename
		datePart := strings.TrimPrefix(name, prefix)
		datePart = strings.TrimSuffix(datePart, suffix)

		fileDate, err := time.Parse(auditDateFormat, datePart)
		if err != nil {
			continue
		}

		if fileDate.Before(cutoff) {
			toDelete = append(toDelete, filepath.Join(al.dir, name))
		}
	}

	// Sort oldest first for logging
	sort.Strings(toDelete)
	for _, path := range toDelete {
		_ = os.Remove(path)
	}
}

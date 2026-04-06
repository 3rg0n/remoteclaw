package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

// maxAuditFieldBytes caps individual string fields in audit entries to prevent log bloat.
const maxAuditFieldBytes = 10 * 1024 // 10KB

// AuditEntry represents a single audit log entry
type AuditEntry struct {
	Timestamp      time.Time     `json:"timestamp"`
	Email          string        `json:"email"`
	SpaceID        string        `json:"space_id"`
	RawMessage     string        `json:"raw_message"`
	ToolCalls      []string      `json:"tool_calls,omitempty"`
	ToolInputs     []string      `json:"tool_inputs,omitempty"` // serialized tool parameters
	Response       string        `json:"response"`
	Duration       time.Duration `json:"duration_ms"`
	Error          string        `json:"error,omitempty"`
	Confirmed      bool          `json:"confirmed,omitempty"` // true if via challenge-response
}

// AuditLogger writes structured NDJSON audit entries with daily rotation and 30-day retention.
// Files are named <base>-YYYY-MM-DD.jsonl (e.g., audit-2026-02-18.jsonl).
type AuditLogger struct {
	basePath string // e.g., "/var/log/remoteclaw/audit" → files are audit-2026-02-18.jsonl
	dir      string
	prefix   string
	logger   zerolog.Logger
	file     *os.File
	fileDate string // date of currently open file
	mu       sync.Mutex
}

// NewAuditLogger creates an audit logger that writes NDJSON to date-stamped files.
// basePath is the path without extension (e.g., "audit" or "/var/log/remoteclaw/audit").
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

// secretPatterns matches common secret formats for redaction in audit logs.
var secretPatterns = regexp.MustCompile(
	`(?i)` + // case-insensitive
		`(?:` +
		`(?:api[_-]?key|token|secret|password|passwd|authorization|bearer)\s*[:=]\s*\S+` + // key=value patterns
		`|` +
		`(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9_]{36,}` + // GitHub tokens
		`|` +
		`(?:xoxb|xoxp|xoxs)-[A-Za-z0-9-]+` + // Slack tokens
		`|` +
		`AKIA[0-9A-Z]{16}` + // AWS access key
		`|` +
		`-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----` + // Private keys
		`)`,
)

// scrubSecrets redacts common secret patterns from a string.
func scrubSecrets(s string) string {
	return secretPatterns.ReplaceAllString(s, "[REDACTED]")
}

// truncateField truncates a string to maxAuditFieldBytes.
func truncateField(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...[truncated]"
}

// Log writes an audit entry as an NDJSON line. Rotates the file if the date has changed.
// Scrubs secrets and truncates large fields before writing.
func (al *AuditLogger) Log(entry AuditEntry) {
	if al == nil {
		return
	}

	al.mu.Lock()
	defer al.mu.Unlock()

	// Rotate if we've crossed into a new day
	if err := al.rotateIfNeeded(); err != nil {
		// Log rotation failure to stderr as a last resort
		_, _ = fmt.Fprintf(os.Stderr, "audit log rotation failed: %v\n", err)
	}

	// Scrub secrets and truncate fields
	rawMsg := truncateField(scrubSecrets(entry.RawMessage), maxAuditFieldBytes)
	response := truncateField(scrubSecrets(entry.Response), maxAuditFieldBytes)

	evt := al.logger.Info().
		Time("timestamp", entry.Timestamp).
		Str("email", entry.Email).
		Str("space_id", entry.SpaceID).
		Str("raw_message", rawMsg).
		Strs("tool_calls", entry.ToolCalls).
		Str("response", response).
		Int64("duration_ms", entry.Duration.Milliseconds()).
		Str("error", entry.Error)

	if len(entry.ToolInputs) > 0 {
		evt = evt.Strs("tool_inputs", entry.ToolInputs)
	}
	if entry.Confirmed {
		evt = evt.Bool("confirmed", true)
	}

	evt.Msg("audit")
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

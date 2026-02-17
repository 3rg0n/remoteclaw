package logging

import (
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// AuditEntry represents a single audit log entry
type AuditEntry struct {
	Timestamp   time.Time     `json:"timestamp"`
	Email       string        `json:"email"`
	SpaceID     string        `json:"space_id"`
	RawMessage  string        `json:"raw_message"`
	ToolCalls   []string      `json:"tool_calls,omitempty"`
	Response    string        `json:"response"`
	Duration    time.Duration `json:"duration_ms"`
	Error       string        `json:"error,omitempty"`
}

// AuditLogger writes structured audit entries for command tracking
type AuditLogger struct {
	logger zerolog.Logger
	file   *os.File
	mu     sync.Mutex
}

// NewAuditLogger creates an audit logger that writes JSON lines to the specified file.
// If filePath is empty, returns nil (audit logging disabled).
// File permissions are set to 0600.
func NewAuditLogger(filePath string) (*AuditLogger, error) {
	if filePath == "" {
		return nil, nil
	}

	fileOut, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600) //nolint:gosec // filePath is from trusted config
	if err != nil {
		return nil, err
	}

	logger := zerolog.New(fileOut).
		With().
		Timestamp().
		Logger()

	return &AuditLogger{
		logger: logger,
		file:   fileOut,
	}, nil
}

// Log writes an audit entry as a JSON log line
func (al *AuditLogger) Log(entry AuditEntry) {
	if al == nil {
		return
	}

	al.mu.Lock()
	defer al.mu.Unlock()

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

// Close closes the file handle
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

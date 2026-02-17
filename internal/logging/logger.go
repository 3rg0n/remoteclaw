package logging

import (
	"io"
	"os"
	"strings"
	"sync"

	"github.com/rs/zerolog"
)

var (
	// logger is the global logger instance
	logger zerolog.Logger

	// logFile tracks the current log file handle for cleanup
	logFile *os.File

	// mu protects logger and logFile access
	mu sync.RWMutex
)

func init() {
	// Initialize with a default logger (info level, console output)
	logger = createConsoleLogger(zerolog.InfoLevel)
}

// Setup configures the global logger with the given settings.
//
// Parameters:
//   - level: log level as string ("debug", "info", "warn", "error"; case-insensitive)
//   - format: output format ("json", "console", or "text"; case-insensitive)
//   - filePath: file path for file output (empty string means no file output)
//
// If filePath is non-empty, logs are written to both file (JSON) and stdout (console).
// If format is "console" or "text", console output uses pretty formatting.
// If format is "json", console output uses JSON formatting.
// Invalid log levels default to info level.
func Setup(level, format, filePath string) error {
	logLevel := parseLogLevel(strings.ToLower(level))
	outputFormat := strings.ToLower(format)

	mu.Lock()
	defer mu.Unlock()

	// Close previous log file if it exists
	if logFile != nil {
		_ = logFile.Close()
		logFile = nil
	}

	if filePath != "" {
		// Multi-output: file (JSON) + console
		fileOut, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600) //nolint:gosec // filePath is from trusted config
		if err != nil {
			return err
		}

		logFile = fileOut

		// Console output depends on format
		var consoleOut io.Writer
		if outputFormat == "console" || outputFormat == "text" {
			consoleOut = createConsoleOutput()
		} else {
			consoleOut = os.Stdout
		}

		logger = zerolog.New(io.MultiWriter(fileOut, consoleOut)).
			Level(logLevel).
			With().
			Timestamp().
			Logger()
	} else if outputFormat == "console" || outputFormat == "text" {
		// Console output (pretty formatting)
		logger = createConsoleLogger(logLevel)
	} else {
		// JSON output to stdout
		logger = zerolog.New(os.Stdout).
			Level(logLevel).
			With().
			Timestamp().
			Logger()
	}

	return nil
}

// Get returns the configured logger.
func Get() zerolog.Logger {
	mu.RLock()
	defer mu.RUnlock()

	return logger
}

// Close closes the log file if one is open.
// This should be called during application shutdown.
func Close() error {
	mu.Lock()
	defer mu.Unlock()

	if logFile != nil {
		err := logFile.Close()
		logFile = nil
		return err
	}
	return nil
}

// parseLogLevel converts a string to zerolog level.
// Defaults to InfoLevel if the string is unrecognized.
func parseLogLevel(level string) zerolog.Level {
	switch level {
	case "debug":
		return zerolog.DebugLevel
	case "info":
		return zerolog.InfoLevel
	case "warn":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}

// createConsoleLogger creates a logger with pretty console output.
func createConsoleLogger(level zerolog.Level) zerolog.Logger {
	return zerolog.New(createConsoleOutput()).
		Level(level).
		With().
		Timestamp().
		Logger()
}

// createConsoleOutput creates a pretty-printed console output writer.
func createConsoleOutput() io.Writer {
	return zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: "2006-01-02 15:04:05",
	}
}

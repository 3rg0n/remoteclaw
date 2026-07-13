package secrets

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// PassGetter retrieves secrets from the pass password manager.
type PassGetter struct {
	storePrefix string
	runner      func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// NewPassGetter creates a new PassGetter with the given store prefix.
// The prefix is prepended to secret keys when looking up pass entries.
// Default storePrefix is "remoteclaw", so a key maps to pass entry "remoteclaw/webex_bot_token".
func NewPassGetter(storePrefix string) *PassGetter {
	if storePrefix == "" {
		storePrefix = "remoteclaw"
	}
	pg := &PassGetter{
		storePrefix: storePrefix,
	}
	pg.runner = pg.defaultRunner
	return pg
}

// defaultRunner executes the pass command with a 5-second timeout.
func (pg *PassGetter) defaultRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	// Create a new context with a 5-second timeout if the incoming context
	// doesn't have a shorter deadline already set.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}

	//#nosec G204 -- fixed command 'pass'; args are the literal "show" and an allowlisted key path
	cmd := exec.CommandContext(ctx, name, args...)

	// Capture both stdout and stderr
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		// Return both stdout and stderr for error diagnosis
		return stdout.Bytes(), fmt.Errorf("exec %s failed: %w (stderr: %s)", name, err, stderr.String())
	}

	return stdout.Bytes(), nil
}

// WithRunner sets a custom runner for testing. This allows injecting a fake
// pass command without actually invoking the real binary.
func (pg *PassGetter) WithRunner(runner func(ctx context.Context, name string, args ...string) ([]byte, error)) *PassGetter {
	pg.runner = runner
	return pg
}

// Available reports whether the pass backend is usable.
// Returns true iff the pass binary is on PATH and the password store directory exists.
func (pg *PassGetter) Available() bool {
	// Check if pass binary is on PATH
	if _, err := exec.LookPath("pass"); err != nil {
		return false
	}

	// Determine the password store directory
	storeDir := os.Getenv("PASSWORD_STORE_DIR")
	if storeDir == "" {
		home := os.Getenv("HOME")
		if home == "" {
			// Windows fallback
			home = os.Getenv("USERPROFILE")
		}
		if home == "" {
			return false
		}
		storeDir = filepath.Join(home, ".password-store")
	}

	// Check if the directory exists. storeDir is the operator's own
	// PASSWORD_STORE_DIR (or $HOME/.password-store) — not agent-controlled input
	// — and is only stat'd for an existence check, never opened for content.
	_, err := os.Stat(storeDir) //#nosec G703 -- path from operator env; existence check only, never opened
	return err == nil
}

// Name returns the backend name for logging.
func (pg *PassGetter) Name() string {
	return "pass"
}

// Get retrieves a secret from the pass store.
// Returns (value, found, error) where:
//   - value is the secret (single-line, newlines trimmed)
//   - found is true if the secret exists, false if it doesn't (not an error)
//   - error is non-nil for real failures (store unreachable, exec error, disallowed key)
func (pg *PassGetter) Get(key Key) (string, bool, error) {
	if !IsAllowed(key) {
		return "", false, fmt.Errorf("secret key %q not allowed", key)
	}

	// Build the pass entry path: prefix/key (always use forward slashes for pass)
	passPath := pg.storePrefix + "/" + string(key)

	// Run pass show passPath
	ctx := context.Background()
	output, err := pg.runner(ctx, "pass", "show", passPath)
	if err != nil {
		// Check if the error indicates "not found"
		errStr := err.Error()
		if strings.Contains(errStr, "is not in the password store") ||
			strings.Contains(errStr, "no such file") ||
			strings.Contains(errStr, "no such directory") {
			// Secret not found (not an error)
			return "", false, nil
		}
		// Real error
		return "", false, err
	}

	// Extract the first line and trim trailing whitespace
	lines := strings.Split(string(output), "\n")
	secret := strings.TrimRight(lines[0], "\r\n")

	return secret, true, nil
}

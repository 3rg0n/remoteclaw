package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

// executeCommand executes a shell command and returns the result.
// Required params: "command" (string) - the shell command to execute
// Optional params: "timeout" (string) - timeout as a duration string (e.g., "30s")
func (e *Executor) executeCommand(ctx context.Context, params map[string]any) (*ToolResult, error) {
	command, err := getStringParam(params, "command")
	if err != nil {
		return &ToolResult{Error: err.Error(), ExitCode: 1}, nil
	}

	// Parse optional timeout
	timeoutDur := e.defaultTimeout
	if timeoutStr, err := getStringParamOpt(params, "timeout"); err == nil && timeoutStr != "" {
		if parsed, err := time.ParseDuration(timeoutStr); err == nil {
			timeoutDur = parsed
		}
	}

	// Cap timeout at maxTimeout
	if timeoutDur > e.maxTimeout {
		timeoutDur = e.maxTimeout
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, timeoutDur)
	defer cancel()

	// Determine shell based on OS
	var shellCmd string
	var shellArgs []string

	if runtime.GOOS == "windows" {
		shellCmd = "powershell"
		shellArgs = []string{"-Command", command}
	} else {
		shellCmd = "sh"
		shellArgs = []string{"-c", command}
	}

	// Create command - gosec: G204 is acceptable here as we control the shell and command construction
	//nolint:gosec // G204: command and shell are controlled; this is the executor's purpose
	cmd := exec.CommandContext(ctx, shellCmd, shellArgs...)

	// Capture stdout and stderr separately
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Execute command
	err = cmd.Run()

	// Determine exit code
	exitCode := 0
	if err != nil {
		// Check if it's a context deadline exceeded error
		if errors.Is(err, context.DeadlineExceeded) {
			return &ToolResult{
				Output:   stdout.String(),
				Error:    "command timed out",
				ExitCode: 124, // Standard timeout exit code
			}, nil
		}

		// Extract exit code if available
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if exitErr.ExitCode() >= 0 {
				exitCode = exitErr.ExitCode()
			}
		}
	}

	// Combine output: stdout first, then stderr
	output := stdout.String()
	if stderrStr := stderr.String(); stderrStr != "" {
		if output != "" {
			output += "\n"
		}
		output += stderrStr
	}

	result := &ToolResult{
		Output:   output,
		ExitCode: exitCode,
	}

	// If there was an error and we didn't already set a specific error message, set one
	if err != nil && result.Error == "" && exitCode != 0 {
		result.Error = fmt.Sprintf("command exited with code %d", exitCode)
	}

	return result, nil
}

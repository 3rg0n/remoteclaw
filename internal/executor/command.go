package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"time"
)

const maxOutputBytes = 1024 * 1024 // 1MB per stream

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

	// Determine shell: use configured shell if set, otherwise auto-detect by OS
	var shellCmd string
	var shellArgs []string

	switch {
	case e.shell != "":
		shellCmd = e.shell
		if runtime.GOOS == "windows" {
			shellArgs = []string{"-Command", command}
		} else {
			shellArgs = []string{"-c", command}
		}
	case runtime.GOOS == "windows":
		shellCmd = "powershell"
		shellArgs = []string{"-Command", command}
	default:
		shellCmd = "sh"
		shellArgs = []string{"-c", command}
	}

	// Create command - gosec: G204 is acceptable here as we control the shell and command construction
	//nolint:gosec // G204: command and shell are controlled; this is the executor's purpose
	cmd := exec.CommandContext(ctx, shellCmd, shellArgs...)

	// Capture stdout and stderr with size limits to prevent OOM
	stdoutR, stdoutW := io.Pipe()
	stderrR, stderrW := io.Pipe()
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW

	// Read output in background with size limits
	type readResult struct {
		data []byte
		err  error
	}
	stdoutCh := make(chan readResult, 1)
	stderrCh := make(chan readResult, 1)
	go func() {
		data, err := io.ReadAll(io.LimitReader(stdoutR, maxOutputBytes))
		stdoutCh <- readResult{data, err}
	}()
	go func() {
		data, err := io.ReadAll(io.LimitReader(stderrR, maxOutputBytes))
		stderrCh <- readResult{data, err}
	}()

	// Execute command
	err = cmd.Run()

	// Close write ends so readers can finish
	_ = stdoutW.Close()
	_ = stderrW.Close()

	stdoutRes := <-stdoutCh
	stderrRes := <-stderrCh

	// Determine exit code
	exitCode := 0
	if err != nil {
		// Check if it's a context deadline exceeded error (check both the
		// error chain and the context directly, since Go 1.20+ may wrap
		// the context error differently when killing a process group)
		if errors.Is(err, context.DeadlineExceeded) || ctx.Err() == context.DeadlineExceeded {
			return &ToolResult{
				Output:   string(stdoutRes.data),
				Error:    "command timed out",
				ExitCode: 124, // Standard timeout exit code
			}, nil
		}

		// Extract exit code if available
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code := exitErr.ExitCode()
			if code >= 0 {
				exitCode = code
			} else {
				// Negative exit code means killed by signal (e.g., SIGKILL)
				exitCode = 137 // 128 + 9 (SIGKILL)
			}
		}
	}

	// Combine output: stdout first, then stderr
	output := string(stdoutRes.data)
	if stderrStr := string(stderrRes.data); stderrStr != "" {
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

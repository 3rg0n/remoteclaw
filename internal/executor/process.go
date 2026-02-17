package executor

import (
	"context"
	"fmt"
	"os"
	"runtime"
)

// listProcesses lists all running processes on the system.
// Uses "ps aux" on Linux/macOS and "tasklist" on Windows.
func (e *Executor) listProcesses(ctx context.Context, params map[string]any) (*ToolResult, error) {
	// Use the command handler to execute the appropriate process listing command
	var command string
	if runtime.GOOS == "windows" {
		command = "tasklist"
	} else {
		command = "ps aux"
	}

	// Call executeCommand directly with the OS-specific command
	result, err := e.executeCommand(ctx, map[string]any{
		"command": command,
	})
	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("failed to list processes: %v", err), ExitCode: 1}, nil //nolint:nilerr // error is captured in ToolResult
	}

	return result, nil
}

// killProcess kills a process by PID.
// Required params: "pid" (number) - the process ID to kill
func (e *Executor) killProcess(ctx context.Context, params map[string]any) (*ToolResult, error) {
	pid, err := getIntParamOpt(params, "pid")
	if err != nil {
		return &ToolResult{Error: err.Error(), ExitCode: 1}, nil //nolint:nilerr // error is captured in ToolResult
	}

	// Use os.FindProcess and process.Kill()
	process, err := os.FindProcess(pid)
	if err != nil {
		return &ToolResult{
			Error:    fmt.Sprintf("process %d not found: %v", pid, err),
			ExitCode: 1,
		}, nil
	}

	err = process.Kill()
	if err != nil {
		return &ToolResult{
			Error:    fmt.Sprintf("failed to kill process %d: %v", pid, err),
			ExitCode: 1,
		}, nil
	}

	return &ToolResult{
		Output:   fmt.Sprintf("Successfully killed process %d", pid),
		ExitCode: 0,
	}, nil
}

package executor

import (
	"context"
	"fmt"
	"time"

	"github.com/3rg0n/remoteclaw/internal/security"
)

// ToolResult holds the result of a tool execution
type ToolResult struct {
	Output   string `json:"output"`
	Error    string `json:"error,omitempty"`
	ExitCode int    `json:"exit_code"`
}

// Executor dispatches tool calls to handlers
type Executor struct {
	defaultTimeout   time.Duration
	maxTimeout       time.Duration
	shell            string
	dangerousChecker *security.DangerousChecker
	guard            *Guard
}

// New creates a new Executor with the given configuration.
// defaultTimeout is used if a tool doesn't specify a timeout.
// maxTimeout is the maximum allowed timeout (tool timeouts are capped at this value).
// shell is the shell to use for command execution (e.g., "bash", "powershell").
func New(defaultTimeout, maxTimeout time.Duration, shell string) *Executor {
	return &Executor{
		defaultTimeout: defaultTimeout,
		maxTimeout:     maxTimeout,
		shell:          shell,
	}
}

// SetDangerousChecker enables dangerous command checking on execute_command calls.
func (e *Executor) SetDangerousChecker(dc *security.DangerousChecker) {
	e.dangerousChecker = dc
}

// SetGuard enables the config/secret lockdown guard. When set and enabled, the
// file tools hard-deny protected paths and execute_command best-effort denies
// secret-reading commands.
func (e *Executor) SetGuard(g *Guard) {
	e.guard = g
}

// Execute dispatches a tool call to the appropriate handler.
// toolName specifies which tool to call, and params are the tool arguments.
// Returns a ToolResult with the tool output or error information.
func (e *Executor) Execute(ctx context.Context, toolName string, params map[string]any) (*ToolResult, error) {
	// Check dangerous commands before executing
	if toolName == "execute_command" && e.dangerousChecker != nil {
		if cmd, ok := params["command"].(string); ok {
			if blocked, reason := e.dangerousChecker.Check(cmd); blocked {
				return &ToolResult{
					Output:   "",
					Error:    fmt.Sprintf("Command blocked: %s", reason),
					ExitCode: 1,
				}, nil
			}
		}
	}

	// Lockdown guard: best-effort deny of secret-reading commands. The
	// authoritative protection is the OS file permission (config/secrets not
	// readable by the service account); this is defense-in-depth. See guard.go.
	if toolName == "execute_command" && e.guard.Enabled() {
		if cmd, ok := params["command"].(string); ok {
			if blocked, reason := e.guard.IsSecretReadCommand(cmd); blocked {
				return &ToolResult{
					Error:    fmt.Sprintf("Command blocked by lockdown: %s (config/secret access requires local administration)", reason),
					ExitCode: 1,
				}, nil
			}
		}
	}

	// Lockdown guard: hard-deny file tools on protected paths.
	if e.guard.Enabled() {
		if blocked, res := e.guardFileTool(toolName, params); blocked {
			return res, nil
		}
	}

	return e.dispatch(ctx, toolName, params)
}

// ForceExecuteCommand runs a command after challenge-response confirmation.
// Re-validates the command against the dangerous checker but logs it as
// a confirmed execution. Still enforces timeouts.
func (e *Executor) ForceExecuteCommand(ctx context.Context, command string) (*ToolResult, error) {
	if command == "" {
		return &ToolResult{Error: "empty command", ExitCode: 1}, nil
	}
	// Re-validate: the dangerous checker patterns may have been updated since the
	// challenge was issued. Log but allow if the checker still blocks — the user
	// already confirmed via challenge-response.
	return e.executeCommand(ctx, map[string]any{"command": command})
}

// guardFileTool hard-denies read_file, write_file, and list_dir when their
// target path falls within a protected config/secret location. Returns
// (true, result) when the call must be blocked.
func (e *Executor) guardFileTool(toolName string, params map[string]any) (bool, *ToolResult) {
	switch toolName {
	case "read_file", "write_file", "list_dir":
		path, ok := params["path"].(string)
		if !ok || path == "" {
			return false, nil
		}
		if e.guard.IsProtectedPath(path) {
			return true, &ToolResult{
				Error:    fmt.Sprintf("access blocked by lockdown: %s is a protected config/secret path (requires local administration)", path),
				ExitCode: 1,
			}
		}
	}
	return false, nil
}

// dispatch routes a tool call to the appropriate handler.
func (e *Executor) dispatch(ctx context.Context, toolName string, params map[string]any) (*ToolResult, error) {
	switch toolName {
	case "execute_command":
		return e.executeCommand(ctx, params)
	case "read_file":
		return e.readFile(ctx, params)
	case "write_file":
		return e.writeFile(ctx, params)
	case "list_dir":
		return e.listDir(ctx, params)
	case "list_processes":
		return e.listProcesses(ctx, params)
	case "kill_process":
		return e.killProcess(ctx, params)
	case "system_info":
		return e.systemInfo(ctx, params)
	default:
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}
}

// getStringParam extracts a required string parameter from the params map.
func getStringParam(params map[string]any, key string) (string, error) {
	value, ok := params[key]
	if !ok {
		return "", fmt.Errorf("required parameter %q not provided", key)
	}

	str, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("parameter %q must be a string, got %T", key, value)
	}

	return str, nil
}

// getStringParamOpt extracts an optional string parameter from the params map.
func getStringParamOpt(params map[string]any, key string) (string, error) {
	value, ok := params[key]
	if !ok {
		return "", nil
	}

	str, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("parameter %q must be a string, got %T", key, value)
	}

	return str, nil
}

// getBoolParamOpt extracts an optional bool parameter from the params map.
func getBoolParamOpt(params map[string]any, key string, defaultVal bool) (bool, error) {
	value, ok := params[key]
	if !ok {
		return defaultVal, nil
	}

	b, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("parameter %q must be a bool, got %T", key, value)
	}

	return b, nil
}

// getFloatParamOpt extracts an optional float parameter from the params map (for numeric values).
func getFloatParamOpt(params map[string]any, key string, defaultVal float64) (float64, error) {
	value, ok := params[key]
	if !ok {
		return defaultVal, nil
	}

	switch v := value.(type) {
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	default:
		return 0, fmt.Errorf("parameter %q must be a number, got %T", key, value)
	}
}

// getIntParamOpt extracts an optional int parameter from the params map (for PID).
func getIntParamOpt(params map[string]any, key string) (int, error) {
	value, ok := params[key]
	if !ok {
		return 0, fmt.Errorf("required parameter %q not provided", key)
	}

	switch v := value.(type) {
	case float64:
		return int(v), nil
	case int:
		return v, nil
	case int64:
		return int(v), nil
	default:
		return 0, fmt.Errorf("parameter %q must be a number, got %T", key, value)
	}
}

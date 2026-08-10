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

	// Denial carries the policy verdict when a command was refused by the
	// command policy. Callers that need to know *how* a command was refused —
	// notably the challenge-response handoff, which may only offer confirmation
	// for a confirmable disposition — must read this rather than parse Error.
	Denial *security.Verdict `json:"-"`
}

// Executor dispatches tool calls to handlers
type Executor struct {
	defaultTimeout time.Duration
	maxTimeout     time.Duration
	shell          string
	policy         *security.CommandPolicy
	guard          *Guard
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

// SetCommandPolicy installs the deny-list policy applied to execute_command.
// One policy covers destructive commands and config/secret reads alike; which
// rule groups it carries is decided when it is built. See ADR 0006.
func (e *Executor) SetCommandPolicy(p *security.CommandPolicy) {
	e.policy = p
}

// SetGuard enables the config/secret lockdown guard, which hard-denies the file
// tools (read_file, write_file, list_dir) on protected paths. Command-string
// matching for secret reads lives in the command policy, not here.
func (e *Executor) SetGuard(g *Guard) {
	e.guard = g
}

// Execute dispatches a tool call to the appropriate handler.
// toolName specifies which tool to call, and params are the tool arguments.
// Returns a ToolResult with the tool output or error information.
func (e *Executor) Execute(ctx context.Context, toolName string, params map[string]any) (*ToolResult, error) {
	// An abandoned request must not touch the system. Checked once here instead
	// of in seven handlers: by the time a tool call is dispatched the caller's
	// deadline (the processor's 5-minute cap, or a per-command timeout) may
	// already have passed, and a write or a kill nobody is waiting for is still
	// a side effect. Handlers whose body can block — readFile, the recursive
	// walk — check again as they go, since cancellation also arrives mid-call.
	if res := cancelled(ctx); res != nil {
		return res, nil
	}

	// Single command-policy evaluation for execute_command: destructive
	// commands and config/secret reads are one rule table with one ordering, so
	// there is one place to add a rule and one denial shape to audit.
	// See ADR 0006.
	if toolName == "execute_command" {
		if cmd, ok := params["command"].(string); ok {
			if v := e.policy.Check(cmd); v != nil {
				return denied(v), nil
			}
		}
	}

	// Lockdown guard: hard-deny file tools on protected paths. Path-based and
	// canonicalizing, so it stays separate from command-string matching.
	if e.guard.Enabled() {
		if blocked, res := e.guardFileTool(toolName, params); blocked {
			return res, nil
		}
	}

	return e.dispatch(ctx, toolName, params)
}

// cancelled reports a cancelled or expired context as a tool result, or nil if
// the context is still live. The error text names the cause (deadline vs.
// cancellation) because the two mean different things to the operator: a
// deadline is the request taking too long, a cancellation is shutdown.
func cancelled(ctx context.Context) *ToolResult {
	err := ctx.Err()
	if err == nil {
		return nil
	}
	return &ToolResult{Error: fmt.Sprintf("tool call aborted: %v", err), ExitCode: 1}
}

// denied renders a policy verdict as a tool result. One shape for every
// command denial, whatever the category, so the audit log and the model see a
// consistent message.
func denied(v *security.Verdict) *ToolResult {
	msg := fmt.Sprintf("Command blocked: %s", v.Reason)
	if v.Disposition == security.DispositionHard {
		msg += " (config/secret access requires local administration)"
	}
	return &ToolResult{Error: msg, ExitCode: 1, Denial: v}
}

// ForceExecuteCommand runs a command the operator confirmed via
// challenge-response.
//
// It re-checks the hard-denial rules. Confirmation proves the operator intended
// a destructive command; it does not authorize config/secret access, because
// the response travels over the same chat channel whose credentials the
// lockdown protects. Confirmable denials are, by definition, not re-applied.
func (e *Executor) ForceExecuteCommand(ctx context.Context, command string) (*ToolResult, error) {
	if command == "" {
		return &ToolResult{Error: "empty command", ExitCode: 1}, nil
	}
	// Confirmation arrives as a separate message, so time has passed since the
	// command was proposed; this path bypasses Execute and needs its own check.
	if res := cancelled(ctx); res != nil {
		return res, nil
	}
	if v := e.policy.CheckHard(command); v != nil {
		return denied(v), nil
	}
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

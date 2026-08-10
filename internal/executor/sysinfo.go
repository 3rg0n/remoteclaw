package executor

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
)

// systemInfo returns system information about the local machine.
//
// The context is unused past the entry check in Execute: every field comes from
// runtime or a single non-blocking syscall, so there is no point during it at
// which cancellation could be observed.
func (e *Executor) systemInfo(_ context.Context, params map[string]any) (*ToolResult, error) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	// Build output with system info
	var output strings.Builder
	fmt.Fprintf(&output, "=== System Information ===\n")
	fmt.Fprintf(&output, "Hostname: %s\n", hostname)
	fmt.Fprintf(&output, "OS: %s\n", runtime.GOOS)
	fmt.Fprintf(&output, "Architecture: %s\n", runtime.GOARCH)
	fmt.Fprintf(&output, "CPU Count: %d\n", runtime.NumCPU())
	fmt.Fprintf(&output, "Go Version: %s\n", runtime.Version())

	return &ToolResult{
		Output:   output.String(),
		ExitCode: 0,
	}, nil
}

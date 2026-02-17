package executor

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
)

// systemInfo returns system information about the local machine.
func (e *Executor) systemInfo(ctx context.Context, params map[string]any) (*ToolResult, error) {
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

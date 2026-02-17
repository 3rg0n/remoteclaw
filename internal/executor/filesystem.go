package executor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// DefaultMaxReadBytes is the default maximum number of bytes to read from a file
	DefaultMaxReadBytes = 1024 * 1024 // 1MB
)

// readFile reads the contents of a file.
// Required params: "path" (string) - the file path to read
// Optional params: "max_bytes" (number) - maximum bytes to read (default 1MB)
func (e *Executor) readFile(ctx context.Context, params map[string]any) (*ToolResult, error) {
	path, err := getStringParam(params, "path")
	if err != nil {
		return &ToolResult{Error: err.Error(), ExitCode: 1}, nil //nolint:nilerr // error is captured in ToolResult
	}

	// Get max_bytes parameter (default 1MB)
	maxBytes, err := getFloatParamOpt(params, "max_bytes", float64(DefaultMaxReadBytes))
	if err != nil {
		return &ToolResult{Error: err.Error(), ExitCode: 1}, nil //nolint:nilerr // error is captured in ToolResult
	}

	// Read file with size limit
	data, err := os.ReadFile(path) //nolint:gosec // file path from AI model
	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("failed to read file: %v", err), ExitCode: 1}, nil //nolint:nilerr // error is captured in ToolResult
	}

	// Check if file exceeds max_bytes
	output := string(data)
	if len(data) > int(maxBytes) {
		output = string(data[:int(maxBytes)])
		output += fmt.Sprintf("\n\n[File truncated: %d of %d bytes shown]", int(maxBytes), len(data))
	}

	return &ToolResult{
		Output:   output,
		ExitCode: 0,
	}, nil
}

// writeFile writes content to a file.
// Required params: "path" (string) - the file path to write
//                  "content" (string) - the content to write
func (e *Executor) writeFile(ctx context.Context, params map[string]any) (*ToolResult, error) {
	path, err := getStringParam(params, "path")
	if err != nil {
		return &ToolResult{Error: err.Error(), ExitCode: 1}, nil //nolint:nilerr // error is captured in ToolResult
	}

	content, err := getStringParam(params, "content")
	if err != nil {
		return &ToolResult{Error: err.Error(), ExitCode: 1}, nil //nolint:nilerr // error is captured in ToolResult
	}

	// Create directories if they don't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil { //nolint:gosec // directory creation with standard permissions
		return &ToolResult{Error: fmt.Sprintf("failed to create directories: %v", err), ExitCode: 1}, nil //nolint:nilerr // error is captured in ToolResult
	}

	// Write file with 0600 permissions
	if err := os.WriteFile(path, []byte(content), 0600); err != nil { //nolint:gosec // intentional permission setting
		return &ToolResult{Error: fmt.Sprintf("failed to write file: %v", err), ExitCode: 1}, nil //nolint:nilerr // error is captured in ToolResult
	}

	return &ToolResult{
		Output:   fmt.Sprintf("Successfully wrote to %s", path),
		ExitCode: 0,
	}, nil
}

// listDir lists the contents of a directory.
// Required params: "path" (string) - the directory path to list
// Optional params: "recursive" (bool) - list recursively (default false)
func (e *Executor) listDir(ctx context.Context, params map[string]any) (*ToolResult, error) {
	path, err := getStringParam(params, "path")
	if err != nil {
		return &ToolResult{Error: err.Error(), ExitCode: 1}, nil //nolint:nilerr // error is captured in ToolResult
	}

	// Get recursive flag (default false)
	recursive, err := getBoolParamOpt(params, "recursive", false)
	if err != nil {
		return &ToolResult{Error: err.Error(), ExitCode: 1}, nil //nolint:nilerr // error is captured in ToolResult
	}

	if recursive {
		return e.listDirRecursive(path)
	}

	// Non-recursive: list immediate children
	entries, err := os.ReadDir(path) //nolint:gosec // directory path from AI model
	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("failed to list directory: %v", err), ExitCode: 1}, nil //nolint:nilerr // error is captured in ToolResult
	}

	var output []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		output = append(output, name)
	}

	// Sort for consistent output
	sort.Strings(output)

	return &ToolResult{
		Output:   strings.Join(output, "\n"),
		ExitCode: 0,
	}, nil
}

// listDirRecursive lists directory contents recursively using filepath.WalkDir.
func (e *Executor) listDirRecursive(path string) (*ToolResult, error) {
	var output []string

	err := filepath.WalkDir(path, func(filePath string, d os.DirEntry, err error) error {
		if err != nil {
			return err //nolint:wrapcheck // error is handled at higher level
		}

		// Get relative path from the root
		relPath, err := filepath.Rel(path, filePath)
		if err != nil {
			relPath = filePath
		}

		// Skip the root directory itself
		if relPath == "." {
			return nil
		}

		// Format: add trailing slash for directories
		entry := relPath
		if d.IsDir() {
			entry += "/"
		}

		output = append(output, entry)
		return nil
	})

	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("failed to walk directory: %v", err), ExitCode: 1}, nil //nolint:nilerr // error is captured in ToolResult
	}

	// Sort for consistent output
	sort.Strings(output)

	return &ToolResult{
		Output:   strings.Join(output, "\n"),
		ExitCode: 0,
	}, nil
}

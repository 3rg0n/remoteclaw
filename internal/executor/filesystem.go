package executor

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
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
	limit := int64(maxBytes)

	// Open file and read only up to limit+1 bytes to detect truncation
	f, err := os.Open(path) //nolint:gosec // file path from AI model
	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("failed to read file: %v", err), ExitCode: 1}, nil //nolint:nilerr // error is captured in ToolResult
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return &ToolResult{Error: fmt.Sprintf("failed to read file: %v", err), ExitCode: 1}, nil //nolint:nilerr // error is captured in ToolResult
	}

	truncated := int64(len(data)) > limit
	if truncated {
		data = data[:limit]
	}

	output := string(data)
	if truncated {
		output += "\n\n[File truncated: output limited to specified max_bytes]"
	}

	return &ToolResult{
		Output:   output,
		ExitCode: 0,
	}, nil
}

// sensitivePaths contains directories that write_file should never write to.
var sensitivePaths = func() []string {
	paths := []string{
		"/etc", "/boot", "/sbin", "/usr/sbin",
		"/root", "/proc", "/sys", "/dev",
	}
	if runtime.GOOS == "windows" {
		paths = append(paths, `C:\Windows`, `C:\Program Files`, `C:\Program Files (x86)`)
	}
	// Add home-directory sensitive subdirs
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".ssh"))
		paths = append(paths, filepath.Join(home, ".gnupg"))
		paths = append(paths, filepath.Join(home, ".config", "systemd"))
	}
	return paths
}()

// isSensitivePath returns true if the given path falls within a sensitive directory.
// Resolves symlinks to prevent symlink-based bypasses.
func isSensitivePath(path string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return true // block if we can't resolve
	}
	absPath = filepath.Clean(absPath)

	// Resolve symlinks to prevent symlink bypass attacks
	if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
		absPath = resolved
	}
	// If EvalSymlinks fails (target doesn't exist yet for writes), use the cleaned path

	for _, sensitive := range sensitivePaths {
		senAbs, err := filepath.Abs(sensitive)
		if err != nil {
			continue
		}
		senAbs = filepath.Clean(senAbs)
		// Check if absPath is inside or equal to the sensitive dir
		rel, err := filepath.Rel(senAbs, absPath)
		if err != nil {
			continue
		}
		if !strings.HasPrefix(rel, "..") {
			return true
		}
	}
	return false
}

// writeFile writes content to a file.
// Required params: "path" (string) - the file path to write
//
//	"content" (string) - the content to write
func (e *Executor) writeFile(ctx context.Context, params map[string]any) (*ToolResult, error) {
	path, err := getStringParam(params, "path")
	if err != nil {
		return &ToolResult{Error: err.Error(), ExitCode: 1}, nil //nolint:nilerr // error is captured in ToolResult
	}

	content, err := getStringParam(params, "content")
	if err != nil {
		return &ToolResult{Error: err.Error(), ExitCode: 1}, nil //nolint:nilerr // error is captured in ToolResult
	}

	// Block writes to sensitive system directories
	if isSensitivePath(path) {
		return &ToolResult{
			Error:    fmt.Sprintf("write blocked: %s is in a sensitive system directory", path),
			ExitCode: 1,
		}, nil
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

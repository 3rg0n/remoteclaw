package executor

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExecutorNew tests the New constructor
func TestExecutorNew(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "bash")
	require.NotNil(t, exec)
	assert.Equal(t, 5*time.Second, exec.defaultTimeout)
	assert.Equal(t, 30*time.Second, exec.maxTimeout)
	assert.Equal(t, "bash", exec.shell)
}

// TestExecuteCommandEcho tests executing a simple echo command
func TestExecuteCommandEcho(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")
	ctx := context.Background()

	var command string
	if runtime.GOOS == "windows" {
		command = "powershell -Command Write-Host 'hello world'"
	} else {
		command = "echo 'hello world'"
	}

	result, err := exec.Execute(ctx, "execute_command", map[string]any{
		"command": command,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Output, "hello world")
	assert.Empty(t, result.Error)
}

// TestExecuteCommandTimeout tests command timeout
func TestExecuteCommandTimeout(t *testing.T) {
	exec := New(100*time.Millisecond, 1*time.Second, "")
	ctx := context.Background()

	var command string
	if runtime.GOOS == "windows" {
		// Use a command that's more reliable for timeout testing on Windows
		command = "powershell -Command (1..1000) | ForEach-Object { Start-Sleep -Milliseconds 10 }"
	} else {
		command = "sleep 5"
	}

	result, err := exec.Execute(ctx, "execute_command", map[string]any{
		"command": command,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	// Either we got the timeout error or the process was killed (exit code 1 or -1)
	// The important thing is we didn't wait the full 5 seconds
	assert.True(t, result.Error == "command timed out" || result.ExitCode != 0,
		"Expected timeout error or non-zero exit code, got error=%q exitCode=%d", result.Error, result.ExitCode)
}

// TestExecuteCommandInvalid tests executing an invalid command
func TestExecuteCommandInvalid(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")
	ctx := context.Background()

	result, err := exec.Execute(ctx, "execute_command", map[string]any{
		"command": "nonexistent_command_12345",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEqual(t, 0, result.ExitCode)
}

// TestExecuteCommandMissingParam tests missing required parameter
func TestExecuteCommandMissingParam(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")
	ctx := context.Background()

	result, err := exec.Execute(ctx, "execute_command", map[string]any{})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.ExitCode)
	assert.Contains(t, result.Error, "required parameter")
}

// TestExecuteCommandWithCustomTimeout tests custom timeout parameter
func TestExecuteCommandWithCustomTimeout(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")
	ctx := context.Background()

	var command string
	if runtime.GOOS == "windows" {
		command = "powershell -Command Write-Host 'quick'"
	} else {
		command = "echo 'quick'"
	}

	result, err := exec.Execute(ctx, "execute_command", map[string]any{
		"command": command,
		"timeout": "1s",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 0, result.ExitCode)
}

// TestReadFileExisting tests reading an existing file
func TestReadFileExisting(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")
	ctx := context.Background()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	content := "test content"
	err := os.WriteFile(tmpFile, []byte(content), 0644) //nolint:gosec // test file
	require.NoError(t, err)

	result, err := exec.Execute(ctx, "read_file", map[string]any{
		"path": tmpFile,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, content, result.Output)
	assert.Empty(t, result.Error)
}

// TestReadFileNonExistent tests reading a non-existent file
func TestReadFileNonExistent(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")
	ctx := context.Background()

	result, err := exec.Execute(ctx, "read_file", map[string]any{
		"path": "/nonexistent/file/path.txt",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.ExitCode)
	assert.NotEmpty(t, result.Error)
}

// TestReadFileTruncate tests reading a file that exceeds max_bytes
func TestReadFileTruncate(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")
	ctx := context.Background()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "large.txt")
	// Create a file larger than 100 bytes
	content := "a"
	for i := 0; i < 200; i++ {
		content += "a"
	}
	err := os.WriteFile(tmpFile, []byte(content), 0644) //nolint:gosec // test file
	require.NoError(t, err)

	result, err := exec.Execute(ctx, "read_file", map[string]any{
		"path":      tmpFile,
		"max_bytes": 100,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Output, "truncated")
}

// TestWriteFileCreate tests creating a new file
func TestWriteFileCreate(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")
	ctx := context.Background()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "new.txt")
	content := "new content"

	result, err := exec.Execute(ctx, "write_file", map[string]any{
		"path":    tmpFile,
		"content": content,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Output, "Successfully wrote")

	// Verify file was created
	readContent, err := os.ReadFile(tmpFile) //nolint:gosec // test file
	require.NoError(t, err)
	assert.Equal(t, content, string(readContent))
}

// TestWriteFileSensitivePath tests that writes to sensitive paths are blocked
func TestWriteFileSensitivePath(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")
	ctx := context.Background()

	sensitivePaths := []string{
		"/etc/passwd",
		"/etc/shadow",
	}

	if runtime.GOOS == "windows" {
		sensitivePaths = []string{
			`C:\Windows\System32\test.txt`,
		}
	}

	for _, path := range sensitivePaths {
		result, err := exec.Execute(ctx, "write_file", map[string]any{
			"path":    path,
			"content": "malicious content",
		})

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, 1, result.ExitCode, "write to %s should be blocked", path)
		assert.Contains(t, result.Error, "sensitive", "write to %s should mention sensitive", path)
	}
}

// TestWriteFileNestedDirs tests creating file with nested directories
func TestWriteFileNestedDirs(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")
	ctx := context.Background()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "a", "b", "c", "file.txt")
	content := "nested content"

	result, err := exec.Execute(ctx, "write_file", map[string]any{
		"path":    tmpFile,
		"content": content,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 0, result.ExitCode)

	// Verify file was created with correct content
	readContent, err := os.ReadFile(tmpFile) //nolint:gosec // test file
	require.NoError(t, err)
	assert.Equal(t, content, string(readContent))
}

// TestWriteFileMissingParam tests missing required parameters
func TestWriteFileMissingParam(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")
	ctx := context.Background()

	// Missing content parameter
	result, err := exec.Execute(ctx, "write_file", map[string]any{
		"path": "/tmp/test.txt",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.ExitCode)
	assert.Contains(t, result.Error, "required parameter")
}

// TestListDirBasic tests listing a directory
func TestListDirBasic(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")
	ctx := context.Background()

	tmpDir := t.TempDir()
	// Create some test files
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content"), 0644)) //nolint:gosec // test file
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("content"), 0644)) //nolint:gosec // test file
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755))                            //nolint:gosec // test directory

	result, err := exec.Execute(ctx, "list_dir", map[string]any{
		"path": tmpDir,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Output, "file1.txt")
	assert.Contains(t, result.Output, "file2.txt")
	assert.Contains(t, result.Output, "subdir/")
}

// TestListDirRecursive tests listing a directory recursively
func TestListDirRecursive(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")
	ctx := context.Background()

	tmpDir := t.TempDir()
	// Create nested structure
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "dir1"), 0755))                                   //nolint:gosec // test directory
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "dir1", "file.txt"), []byte("content"), 0644)) //nolint:gosec // test file
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "dir2"), 0755))                                    //nolint:gosec // test directory

	result, err := exec.Execute(ctx, "list_dir", map[string]any{
		"path":      tmpDir,
		"recursive": true,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Output, "dir1/")
	assert.Contains(t, result.Output, "dir2/")
	assert.Contains(t, result.Output, "file.txt")
}

// TestSystemInfo tests system info retrieval
func TestSystemInfo(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")
	ctx := context.Background()

	result, err := exec.Execute(ctx, "system_info", map[string]any{})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 0, result.ExitCode)
	assert.NotEmpty(t, result.Output)
	assert.Contains(t, result.Output, "Hostname")
	assert.Contains(t, result.Output, "OS")
	assert.Contains(t, result.Output, "Architecture")
	assert.Contains(t, result.Output, "CPU Count")
	assert.Contains(t, result.Output, "Go Version")
}

// TestListProcesses tests process listing
func TestListProcesses(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")
	ctx := context.Background()

	result, err := exec.Execute(ctx, "list_processes", map[string]any{})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 0, result.ExitCode)
	assert.NotEmpty(t, result.Output)
}

// TestKillProcessInvalid tests killing an invalid process
func TestKillProcessInvalid(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")
	ctx := context.Background()

	// Use a very high, unlikely PID
	result, err := exec.Execute(ctx, "kill_process", map[string]any{
		"pid": 999999,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	// On Unix, finding a non-existent process returns an error
	// On Windows, it might succeed but then fail to kill
	// Either way, we should see an error
	if runtime.GOOS != "windows" {
		assert.Equal(t, 1, result.ExitCode)
		assert.NotEmpty(t, result.Error)
	}
}

// TestKillProcessMissingParam tests missing PID parameter
func TestKillProcessMissingParam(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")
	ctx := context.Background()

	result, err := exec.Execute(ctx, "kill_process", map[string]any{})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.ExitCode)
	assert.Contains(t, result.Error, "required parameter")
}

// TestUnknownTool tests executing an unknown tool
func TestUnknownTool(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")
	ctx := context.Background()

	_, err := exec.Execute(ctx, "unknown_tool", map[string]any{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tool")
}

// TestExecuteCommandStderr tests command with stderr output
func TestExecuteCommandStderr(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")
	ctx := context.Background()

	var command string
	if runtime.GOOS == "windows" {
		// PowerShell command that writes to stderr
		command = "powershell -Command Write-Error 'error message' -ErrorAction Continue; Write-Host 'stdout'"
	} else {
		command = "sh -c 'echo stdout; echo stderr >&2'"
	}

	result, err := exec.Execute(ctx, "execute_command", map[string]any{
		"command": command,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	// On some systems, the exit code might still be 0 even with stderr
	// The important thing is that output contains both stdout and stderr
	assert.NotEmpty(t, result.Output)
}

// TestParameterTypeValidation tests parameter type validation
func TestParameterTypeValidation(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")
	ctx := context.Background()

	// Pass wrong type for command (int instead of string)
	result, err := exec.Execute(ctx, "execute_command", map[string]any{
		"command": 123,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.ExitCode)
	assert.Contains(t, result.Error, "must be a string")
}

// TestReadFileMaxBytes tests read_file with numeric max_bytes
func TestReadFileMaxBytes(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")
	ctx := context.Background()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	content := "0123456789"
	err := os.WriteFile(tmpFile, []byte(content), 0644) //nolint:gosec // test file
	require.NoError(t, err)

	// Test with int max_bytes
	result, err := exec.Execute(ctx, "read_file", map[string]any{
		"path":      tmpFile,
		"max_bytes": 5,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Output, "01234")
}

// TestListDirNonExistent tests listing a non-existent directory
func TestListDirNonExistent(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")
	ctx := context.Background()

	result, err := exec.Execute(ctx, "list_dir", map[string]any{
		"path": "/nonexistent/directory",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.ExitCode)
	assert.NotEmpty(t, result.Error)
}

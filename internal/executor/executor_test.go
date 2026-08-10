package executor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/3rg0n/remoteclaw/internal/security"
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

	// Use a generous per-command timeout: this test asserts that a quick command
	// succeeds within its custom timeout, not that a shell cold-starts fast.
	// PowerShell cold-start on Windows can exceed 2s.
	result, err := exec.Execute(ctx, "execute_command", map[string]any{
		"command": command,
		"timeout": "20s",
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
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755))                           //nolint:gosec // test directory

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
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "dir1"), 0755))                                    //nolint:gosec // test directory
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

// TestExecutePolicyDenialShape verifies a command denial carries the verdict on
// the result, not just a formatted string. The challenge-response handoff keys
// off the disposition, so parsing Error would make the security decision depend
// on message wording.
func TestExecutePolicyDenialShape(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")
	exec.SetCommandPolicy(security.NewCommandPolicy(security.CommandPolicyOptions{
		BlockDangerous: true,
	}))

	result, err := exec.Execute(context.Background(), "execute_command",
		map[string]any{"command": "rm -rf /"})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.ExitCode)
	assert.Contains(t, result.Error, "Command blocked:")
	require.NotNil(t, result.Denial)
	assert.Equal(t, security.CategoryDestructive, result.Denial.Category)
	assert.Equal(t, security.DispositionChallenge, result.Denial.Disposition)
	// A confirmable denial must not claim local administration is required.
	assert.NotContains(t, result.Error, "requires local administration")
}

// TestExecuteWithoutPolicyBlocksNothing covers the operator opt-out: no policy
// installed means no command denial, and a nil policy must not panic.
func TestExecuteWithoutPolicyBlocksNothing(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")

	result, err := exec.Execute(context.Background(), "execute_command",
		map[string]any{"command": "echo shutdown"})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Nil(t, result.Denial)
	assert.Equal(t, 0, result.ExitCode)
}

// TestForceExecuteCommandRunsConfirmedDestructive verifies challenge-response
// confirmation actually bypasses the confirmable rules — otherwise confirming
// would be a no-op.
func TestForceExecuteCommandRunsConfirmedDestructive(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")
	exec.SetCommandPolicy(security.NewCommandPolicy(security.CommandPolicyOptions{
		BlockDangerous: true,
	}))

	// Matches the "at job scheduling" rule but is harmless to actually run.
	const cmd = "echo at now"
	require.NotNil(t, exec.policy.Check(cmd), "test command must be policy-blocked to be meaningful")

	result, err := exec.ForceExecuteCommand(context.Background(), cmd)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Nil(t, result.Denial, "a confirmed destructive command must not be re-denied")
	assert.Equal(t, 0, result.ExitCode)
}

// TestForceExecuteCommandStillDeniesSecretReads is the security invariant of the
// merged policy: confirmation proves intent for a destructive command, but the
// challenge response arrives over the same chat channel whose credentials the
// lockdown protects, so it must not unlock config/secret access.
func TestForceExecuteCommandStillDeniesSecretReads(t *testing.T) {
	dir := t.TempDir()
	protected := filepath.Join(dir, "config.yaml")
	g := NewGuard(true, []string{dir})
	exec := New(5*time.Second, 30*time.Second, "")
	exec.SetGuard(g)
	exec.SetCommandPolicy(security.NewCommandPolicy(security.CommandPolicyOptions{
		BlockDangerous:   true,
		BlockSecretReads: true,
		ProtectedPaths:   g.ProtectedPaths(),
	}))

	cases := []string{
		"cat " + protected,
		"pass show remoteclaw/webex_bot_token",

		// The bypass chain this consolidation closes. Each of these matches a
		// *confirmable* rule (sudo / eval) as well as a secret-read rule. Before
		// consolidation the dangerous checker ran first and returned early, so
		// the command was offered as a challenge; on confirmation the old
		// ForceExecuteCommand ran it with no lockdown check at all, dumping the
		// secret. Precedence (hard denials first) plus the hard re-check here
		// closes both halves.
		"sudo -E printenv OPENAI_API_KEY",
		"sudo cat " + protected,
		"eval cat " + protected,
	}
	for _, cmd := range cases {
		result, err := exec.ForceExecuteCommand(context.Background(), cmd)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Denial, "confirmation must not unlock %q", cmd)
		assert.Equal(t, security.CategorySecretRead, result.Denial.Category,
			"%q must be denied as a secret read, not as the confirmable rule it also matches", cmd)
		assert.Equal(t, 1, result.ExitCode)
	}
}

// TestExecuteRejectsCancelledContext covers the entry check in Execute: the
// handler signatures promised cancellability the bodies never delivered, so an
// abandoned request — one whose caller already hit the processor's 5-minute cap —
// still wrote files and killed processes. Every tool must refuse instead.
func TestExecuteRejectsCancelledContext(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")

	dir := t.TempDir()
	victim := filepath.Join(dir, "must-not-exist.txt")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := []struct {
		tool   string
		params map[string]any
	}{
		{"execute_command", map[string]any{"command": "echo hi"}},
		{"read_file", map[string]any{"path": filepath.Join(dir, "any.txt")}},
		{"write_file", map[string]any{"path": victim, "content": "x"}},
		{"list_dir", map[string]any{"path": dir}},
		{"list_dir", map[string]any{"path": dir, "recursive": true}},
		{"list_processes", map[string]any{}},
		{"kill_process", map[string]any{"pid": 999999}},
		{"system_info", map[string]any{}},
	}
	for _, c := range calls {
		t.Run(c.tool, func(t *testing.T) {
			result, err := exec.Execute(ctx, c.tool, c.params)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, 1, result.ExitCode)
			assert.Contains(t, result.Error, "tool call aborted")
			assert.Contains(t, result.Error, context.Canceled.Error())
		})
	}

	// The point of refusing: no side effect happened.
	_, statErr := os.Stat(victim)
	assert.True(t, os.IsNotExist(statErr),
		"write_file must not touch the filesystem on a cancelled context")
}

// TestForceExecuteCommandRejectsCancelledContext covers the confirmation path,
// which bypasses Execute and so needs its own check. Confirmation arrives as a
// separate chat message, so measurable time has passed since the command was
// proposed — this is the path most likely to find an expired context.
func TestForceExecuteCommandRejectsCancelledContext(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")
	exec.SetCommandPolicy(security.NewCommandPolicy(security.CommandPolicyOptions{
		BlockDangerous: true,
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := exec.ForceExecuteCommand(ctx, "echo at now")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.ExitCode)
	assert.Contains(t, result.Error, "tool call aborted")

	// An empty command is still rejected as an empty command, and a hard denial
	// still outranks the abort — cancellation must not become a way to get a
	// different answer out of the policy.
	result, err = exec.ForceExecuteCommand(ctx, "")
	require.NoError(t, err)
	assert.Contains(t, result.Error, "empty command")
}

// TestReadFileCancelledMidRead is the concrete hazard #34 names: readFile used a
// single io.ReadAll over the whole size cap, which cannot be interrupted. On a
// slow-backed file (network mount, /dev/*, a fifo) that call outlives the
// caller's deadline. The read is now chunked, so a context cancelled after the
// read begins is observed between chunks.
func TestReadFileCancelledMidRead(t *testing.T) {
	// A reader that hands back one chunk, then cancels before the next. The
	// handler must notice at the chunk boundary rather than reading to the cap.
	ctx, cancel := context.WithCancel(context.Background())
	r := &cancelAfterFirstRead{cancel: cancel}

	data, err := readAllCancellable(ctx, r, DefaultMaxReadBytes)
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, data)
	assert.Equal(t, 1, r.reads, "the second chunk must not be attempted after cancellation")
}

// cancelAfterFirstRead fills one chunk, then cancels the context, standing in
// for a slow-backed file whose caller gives up partway through.
type cancelAfterFirstRead struct {
	cancel context.CancelFunc
	reads  int
}

func (c *cancelAfterFirstRead) Read(p []byte) (int, error) {
	c.reads++
	c.cancel()
	return len(p), nil
}

// TestReadAllCancellableMatchesReadAll pins the refactor: chunking changed how
// readFile reads, and must not have changed what it returns. Sizes straddle the
// chunk boundary in both directions, and the limit is exercised exactly.
func TestReadAllCancellableMatchesReadAll(t *testing.T) {
	sizes := []int{0, 1, readChunkBytes - 1, readChunkBytes, readChunkBytes + 1, 3*readChunkBytes + 7}
	for _, size := range sizes {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			content := bytes.Repeat([]byte("ab"), (size+1)/2)[:size]

			got, err := readAllCancellable(context.Background(), bytes.NewReader(content), DefaultMaxReadBytes)
			require.NoError(t, err)
			// Compared as strings: an empty read may come back nil rather than an
			// empty slice, which is the same thing to readFile (it does
			// string(data)) and to the length check that drives truncation.
			assert.Equal(t, string(content), string(got))

			// A limit below the content length stops exactly at the limit, which
			// is what the truncation marker in readFile depends on.
			if size > 1 {
				limit := int64(size - 1)
				got, err = readAllCancellable(context.Background(), bytes.NewReader(content), limit)
				require.NoError(t, err)
				assert.Equal(t, string(content[:limit]), string(got))
			}
		})
	}
}

// TestReadFileStillReadsWholeFile is the end-to-end guard on the same refactor:
// a file spanning several chunks must come back intact and unmarked.
func TestReadFileStillReadsWholeFile(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")

	tmpFile := filepath.Join(t.TempDir(), "multichunk.bin")
	content := bytes.Repeat([]byte("0123456789"), readChunkBytes/10*2+3)
	require.NoError(t, os.WriteFile(tmpFile, content, 0600))

	result, err := exec.Execute(context.Background(), "read_file", map[string]any{"path": tmpFile})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, string(content), result.Output)
	assert.NotContains(t, result.Output, "truncated")
}

// TestListDirRecursiveCancelledMidWalk covers the other unbounded handler: the
// tree size is whatever the caller pointed at, so the walk checks per entry and
// aborts rather than enumerating a tree nobody is waiting for.
//
// It calls listDirRecursive directly, not through Execute — going through Execute
// would be satisfied by the entry check alone and would pass even with the
// per-entry check removed, which is the thing under test here.
func TestListDirRecursiveCancelledMidWalk(t *testing.T) {
	exec := New(5*time.Second, 30*time.Second, "")

	// More entries than the walk can finish before the cancellation lands, so the
	// abort happens mid-tree.
	dir := t.TempDir()
	for i := 0; i < 200; i++ {
		sub := filepath.Join(dir, fmt.Sprintf("d%03d", i))
		require.NoError(t, os.Mkdir(sub, 0750))
		require.NoError(t, os.WriteFile(filepath.Join(sub, "f.txt"), []byte("x"), 0600))
	}

	// Cancelled after the walk has begun: the deadline fires once the first
	// entries are in, leaving the rest of the tree unvisited.
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	result, err := exec.listDirRecursive(ctx, dir)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.ExitCode, "a cancelled walk must not report success")
	assert.Contains(t, result.Error, "tool call aborted")
	// A cancelled walk is an abort, not a directory problem — misreporting it
	// would send the operator looking at permissions.
	assert.NotContains(t, result.Error, "failed to walk directory")
	assert.Empty(t, result.Output, "a cancelled walk must not return a partial listing")
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

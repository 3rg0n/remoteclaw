package executor

import (
	"context"
	"path/filepath"
	"testing"
)

func TestGuardDisabledAllowsEverything(t *testing.T) {
	g := NewGuard(false, []string{"/etc/remoteclaw"})
	if g.Enabled() {
		t.Fatal("guard should be disabled")
	}
	if g.IsProtectedPath("/etc/remoteclaw/config.yaml") {
		t.Error("disabled guard must not protect any path")
	}
	if blocked, _ := g.IsSecretReadCommand("pass show remoteclaw/webex_bot_token"); blocked {
		t.Error("disabled guard must not block any command")
	}
}

func TestGuardProtectedPathExactAndNested(t *testing.T) {
	dir := t.TempDir()
	g := NewGuard(true, []string{dir})

	cases := []struct {
		path string
		want bool
	}{
		{dir, true}, // the dir itself
		{filepath.Join(dir, "config.yaml"), true},     // nested file
		{filepath.Join(dir, "sub", "deep.env"), true}, // deeper
		{filepath.Dir(dir), false},                    // parent is not protected
		{"/some/unrelated/path", false},
	}
	for _, c := range cases {
		if got := g.IsProtectedPath(c.path); got != c.want {
			t.Errorf("IsProtectedPath(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestGuardProtectedPathRelativeBypass(t *testing.T) {
	dir := t.TempDir()
	g := NewGuard(true, []string{dir})

	// A relative path that canonicalizes into the protected dir must be caught.
	// Build a path with a ".." segment that resolves back inside dir.
	sneaky := filepath.Join(dir, "sub", "..", "config.yaml")
	if !g.IsProtectedPath(sneaky) {
		t.Errorf("relative path %q resolving into protected dir should be blocked", sneaky)
	}
}

func TestGuardSecretReadCommands(t *testing.T) {
	dir := t.TempDir()
	protected := filepath.Join(dir, "config.yaml")
	g := NewGuard(true, []string{protected})

	blockedCmds := []string{
		"pass show remoteclaw/webex_bot_token",
		"pass ls",
		"gpg -d secret.gpg",
		"gpg --decrypt secret.gpg",
		"printenv",
		"env",
		"echo $CHALLENGE",
		"cat /proc/self/environ",
		"echo $env:WEBEX_BOT_TOKEN",
		"cat " + protected, // direct read of a protected path
	}
	for _, cmd := range blockedCmds {
		if blocked, _ := g.IsSecretReadCommand(cmd); !blocked {
			t.Errorf("expected command to be blocked: %q", cmd)
		}
	}

	allowedCmds := []string{
		"ls -la /tmp",
		"echo hello",
		"df -h",
		"systemctl status nginx",
	}
	for _, cmd := range allowedCmds {
		if blocked, reason := g.IsSecretReadCommand(cmd); blocked {
			t.Errorf("expected command to be allowed: %q (blocked as: %s)", cmd, reason)
		}
	}
}

// TestExecutorGuardBlocksFileTools verifies the guard is enforced through the
// Executor's Execute entry point, not just the Guard unit.
func TestExecutorGuardBlocksFileTools(t *testing.T) {
	dir := t.TempDir()
	protected := filepath.Join(dir, "config.yaml")
	e := New(0, 0, "")
	e.SetGuard(NewGuard(true, []string{dir}))

	ctx := context.Background()

	// read_file on a protected path is denied.
	res, err := e.Execute(ctx, "read_file", map[string]any{"path": protected})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ExitCode == 0 {
		t.Error("read_file on protected path should be blocked")
	}

	// write_file on a protected path is denied.
	res, _ = e.Execute(ctx, "write_file", map[string]any{"path": protected, "content": "x"})
	if res.ExitCode == 0 {
		t.Error("write_file on protected path should be blocked")
	}

	// execute_command reading the protected path is denied (best-effort).
	res, _ = e.Execute(ctx, "execute_command", map[string]any{"command": "cat " + protected})
	if res.ExitCode == 0 {
		t.Error("execute_command reading protected path should be blocked")
	}
}

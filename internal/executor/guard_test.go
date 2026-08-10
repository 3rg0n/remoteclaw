package executor

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/3rg0n/remoteclaw/internal/security"
)

func TestGuardDisabledAllowsEverything(t *testing.T) {
	g := NewGuard(false, []string{"/etc/remoteclaw"})
	if g.Enabled() {
		t.Fatal("guard should be disabled")
	}
	if g.IsProtectedPath("/etc/remoteclaw/config.yaml") {
		t.Error("disabled guard must not protect any path")
	}
	// A disabled guard exposes no protected paths, so the command policy built
	// from it carries no protected-path rule.
	if paths := g.ProtectedPaths(); len(paths) != 0 {
		t.Errorf("disabled guard must expose no protected paths, got %v", paths)
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

// TestGuardProtectedPathsCanonicalized verifies the guard hands the command
// policy canonicalized paths — the policy matches path literals in a command
// string and cannot canonicalize on its own.
func TestGuardProtectedPathsCanonicalized(t *testing.T) {
	// A relative path: canonicalization must make it absolute, or the policy
	// would be matching a literal that never appears in a real command.
	const relative = "config.yaml"
	g := NewGuard(true, []string{relative, ""})

	paths := g.ProtectedPaths()
	if len(paths) != 1 {
		t.Fatalf("expected 1 protected path (empty entries dropped), got %v", paths)
	}
	if !filepath.IsAbs(paths[0]) {
		t.Errorf("ProtectedPaths()[0] = %q, want an absolute path", paths[0])
	}
	if want := canonPath(relative); paths[0] != want {
		t.Errorf("ProtectedPaths()[0] = %q, want canonicalized %q", paths[0], want)
	}

	// The returned slice is a copy: a caller cannot mutate the guard's state.
	paths[0] = "/mutated"
	if g.ProtectedPaths()[0] == "/mutated" {
		t.Error("ProtectedPaths must return a copy, not the guard's own slice")
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
}

// TestExecutorPolicyBlocksSecretReadCommand verifies the secret-read half of the
// lockdown still reaches execute_command after moving into the command policy —
// wired the way agent.New wires it, from the guard's canonicalized paths.
func TestExecutorPolicyBlocksSecretReadCommand(t *testing.T) {
	dir := t.TempDir()
	protected := filepath.Join(dir, "config.yaml")
	g := NewGuard(true, []string{dir})
	e := New(0, 0, "")
	e.SetGuard(g)
	e.SetCommandPolicy(security.NewCommandPolicy(security.CommandPolicyOptions{
		BlockSecretReads: g.Enabled(),
		ProtectedPaths:   g.ProtectedPaths(),
	}))

	res, err := e.Execute(context.Background(), "execute_command",
		map[string]any{"command": "cat " + protected})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ExitCode == 0 {
		t.Fatal("execute_command reading protected path should be blocked")
	}
	if res.Denial == nil {
		t.Fatal("a policy denial must be reported on ToolResult.Denial")
	}
	if res.Denial.Disposition != security.DispositionHard {
		t.Errorf("secret reads must be hard denials, got %v", res.Denial.Disposition)
	}
	if !strings.Contains(res.Error, "requires local administration") {
		t.Errorf("hard denial should say confirmation is not available, got %q", res.Error)
	}
}

package ai

import (
	"context"
	"testing"
	"time"

	inferd "github.com/3rg0n/inferd/clients/go"
)

// TestInferd_UnreachableDaemon verifies that constructing an InferdClient
// against a bogus socket path fails fast with a wrapped, actionable error
// rather than hanging or panicking. This is verifiable without a daemon.
func TestInferd_UnreachableDaemon(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// A path that cannot resolve to a listening socket/pipe.
	_, err := NewInferdClient(ctx, "/nonexistent/remoteclaw-test/inferd.sock", 0.2)
	if err == nil {
		t.Fatal("expected error for unreachable inferd daemon, got nil")
	}
}

// TestInferd_LiveConverse drives the real InferdClient against a running
// inferd daemon at the platform-default socket. It skips when no daemon is
// reachable, so it is safe in CI. Run it locally with inferd installed to
// exercise the full streaming path.
func TestInferd_LiveConverse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Probe the default socket; skip if the daemon isn't up.
	probe, err := inferd.DialInfer(ctx)
	if err != nil {
		t.Skipf("inferd daemon not reachable at %s (%v); skipping live test", inferd.DefaultInferAddr(), err)
	}
	_ = probe.Close()

	client, err := NewInferdClient(ctx, "", 0.2)
	if err != nil {
		t.Fatalf("failed to create inferd client: %v", err)
	}

	genCtx, genCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer genCancel()

	msg, err := client.Converse(genCtx, "You are a test.", []Message{
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: "Reply with the single word OK."}}},
	}, nil, 64)
	if err != nil {
		t.Fatalf("Converse failed: %v", err)
	}
	if msg == nil || msg.Role != "assistant" {
		t.Fatalf("expected assistant message, got %+v", msg)
	}
	if len(msg.Content) == 0 {
		t.Error("expected non-empty content from live inferd daemon")
	}
}

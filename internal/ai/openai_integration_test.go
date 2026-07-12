package ai

import (
	"context"
	"net"
	"os"
	"testing"
	"time"
)

// mockAPIsBaseURL is the openai-compat endpoint of the local mock-apis server.
// Override with MOCK_APIS_OPENAI_URL. The test skips when nothing is listening,
// so it is safe in CI without the mock running.
func mockAPIsBaseURL() string {
	if v := os.Getenv("MOCK_APIS_OPENAI_URL"); v != "" {
		return v
	}
	return "http://localhost:8080/openai/v1"
}

func mockReachable(t *testing.T) string {
	t.Helper()
	base := mockAPIsBaseURL()
	// Probe the underlying host:port with a short dial.
	conn, err := net.DialTimeout("tcp", "localhost:8080", 300*time.Millisecond)
	if err != nil {
		t.Skipf("mock-apis not reachable at %s (%v); skipping live openai-compat test", base, err)
	}
	_ = conn.Close()
	return base
}

// TestOpenAICompat_LiveChat drives the real OpenAICompatClient against the
// mock-apis OpenAI dialect, which is CI-validated by openai/openai-go itself.
// This confirms our request marshals and the response unmarshals end-to-end.
func TestOpenAICompat_LiveChat(t *testing.T) {
	base := mockReachable(t)

	client, err := NewOpenAICompatClient(base, "test-key", "gpt-4o", 0.2)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	msg, err := client.Converse(ctx, "You are a test.", []Message{
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: "hi"}}},
	}, nil, 256)
	if err != nil {
		t.Fatalf("Converse failed: %v", err)
	}
	if msg == nil || msg.Role != "assistant" {
		t.Fatalf("expected assistant message, got %+v", msg)
	}
	// The mock echoes the prompt in a text block.
	var gotText bool
	for _, cb := range msg.Content {
		if cb.Type == "text" && cb.Text != "" {
			gotText = true
		}
	}
	if !gotText {
		t.Errorf("expected non-empty text content, got %+v", msg.Content)
	}
}

// TestOpenAICompat_LiveChatWithTools confirms a request carrying tool
// definitions is accepted and a well-formed response is parsed. The mock may
// or may not emit a tool_use block depending on its config; either way the
// client must round-trip without error.
func TestOpenAICompat_LiveChatWithTools(t *testing.T) {
	base := mockReachable(t)

	client, err := NewOpenAICompatClient(base, "test-key", "gpt-4o", 0.2)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	msg, err := client.Converse(ctx, "You are RemoteClaw.", []Message{
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: "list processes"}}},
	}, AllTools(), 256)
	if err != nil {
		t.Fatalf("Converse with tools failed: %v", err)
	}
	if msg == nil || msg.Role != "assistant" {
		t.Fatalf("expected assistant message, got %+v", msg)
	}
	// Any tool_use block that comes back must be well-formed (non-empty name,
	// non-nil input map) — this exercises the response→internal mapping.
	for _, cb := range msg.Content {
		if cb.Type == "tool_use" {
			if cb.ToolName == "" {
				t.Error("tool_use block has empty ToolName")
			}
			if cb.Input == nil {
				t.Error("tool_use block has nil Input map")
			}
		}
	}
}

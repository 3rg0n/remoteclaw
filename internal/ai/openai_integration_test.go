package ai

import (
	"context"
	"net/http"
	"os"
	"strings"
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

// mockReachable skips the test unless something at the configured base URL
// actually speaks the OpenAI chat-completions dialect.
//
// It probes the endpoint rather than the port. A bare TCP dial to localhost:8080
// only proves *something* is listening, and 8080 is a popular port — any
// unrelated local server (a wasm runtime, a dev proxy) makes the guard pass and
// the test then fails against the wrong server with a confusing 405. That is a
// false failure: it reports a broken client when the real condition is "the mock
// is not running." A test that cannot tell those apart is worse than one that
// skips, because the whole point of the guard is to be safe when the mock is
// absent.
//
// The probe POSTs to the real path and accepts any status the server produces,
// including 4xx from a rejected body — an OpenAI-dialect server answers that path
// with *something*, while an unrelated one 404s or 405s it. Only a status that
// says "this path is not a chat-completions endpoint" is treated as absent.
func mockReachable(t *testing.T) string {
	t.Helper()
	base := mockAPIsBaseURL()
	url := strings.TrimSuffix(base, "/") + "/chat/completions"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	body := strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"probe"}]}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		t.Skipf("cannot build probe request for %s (%v); skipping live openai-compat test", url, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Skipf("mock-apis not reachable at %s (%v); skipping live openai-compat test", base, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// 404/405 mean the listener is not an OpenAI-compatible endpoint — something
	// else owns the port. Treat that as "mock absent", not as a failure.
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		t.Skipf("something other than mock-apis is serving %s (HTTP %d); skipping live openai-compat test",
			url, resp.StatusCode)
	}
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

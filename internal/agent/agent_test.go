package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/3rg0n/remoteclaw/internal/ai"
	"github.com/3rg0n/remoteclaw/internal/config"
	"github.com/3rg0n/remoteclaw/internal/connect"
	"github.com/3rg0n/remoteclaw/internal/executor"
	"github.com/3rg0n/remoteclaw/internal/logging"
	"github.com/3rg0n/remoteclaw/internal/security"
)

// MockConverser is a mock implementation of ai.Converser for testing
type MockConverser struct {
	response *ai.Message
	err      error
}

func (m *MockConverser) Converse(
	ctx context.Context,
	system string,
	messages []ai.Message,
	tools []ai.ToolDef,
	maxTokens int,
) (*ai.Message, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

// MockMode is a mock implementation of connect.Mode for testing
type MockMode struct {
	connected bool
	handler   connect.MessageHandler
	closed    bool
	sentText  string // last message text sent, for assertions
}

func (m *MockMode) Connect(ctx context.Context) error {
	m.connected = true
	return nil
}

func (m *MockMode) OnMessage(handler connect.MessageHandler) {
	m.handler = handler
}

func (m *MockMode) SendMessage(ctx context.Context, spaceID string, text string) error {
	m.sentText = text
	return nil
}

func (m *MockMode) Close() error {
	m.closed = true
	return nil
}

// TestNew verifies that New() creates an agent with valid configuration
func TestNew(t *testing.T) {
	// Initialize logging
	if err := logging.Setup("info", "json", ""); err != nil {
		t.Fatalf("failed to setup logging: %v", err)
	}

	// Note: This test verifies the structure compiles.
	// Full New() testing requires a reachable inferd daemon or an
	// openai-compat endpoint; the message-handler tests below inject a
	// MockConverser to exercise behavior without a live backend.
	_ = &config.Config{
		Mode: "native",
		Webex: config.WebexConfig{
			BotToken:      "test-token",
			AllowedEmails: []string{},
		},
		AI: config.AIConfig{
			Provider:      config.ProviderOpenAICompat,
			Mode:          config.AIModeInterpret,
			Model:         "test-model",
			OpenAIBaseURL: "http://localhost:8080/openai/v1",
			MaxTokens:     4096,
			MaxIterations: 10,
		},
		Execution: config.ExecutionConfig{
			DefaultTimeout: 30 * time.Second,
			MaxTimeout:     5 * time.Minute,
			Shell:          "bash",
		},
		Logging: config.LoggingConfig{
			Level:  "info",
			Format: "json",
		},
		Health: config.HealthConfig{
			Enabled: false,
		},
	}
}

// TestToolExecutorBridge tests the tool executor bridge function
func TestToolExecutorBridge(t *testing.T) {
	exec := executor.New(30*time.Second, 5*time.Minute, "bash")

	// Create a simple bridge function
	bridge := func(ctx context.Context, toolName string, params map[string]any) (string, error) {
		result, err := exec.Execute(ctx, toolName, params)
		if err != nil {
			return "", err
		}
		output := result.Output
		if result.Error != "" {
			output += "\nError: " + result.Error
		}
		return output, nil
	}

	// Test with a valid tool
	ctx := context.Background()
	result, err := bridge(ctx, "system_info", map[string]any{})
	if err != nil {
		t.Fatalf("bridge failed: %v", err)
	}
	if result == "" {
		t.Error("bridge returned empty result")
	}
}

// TestToolExecutorBridgeError tests error handling in the tool executor bridge
func TestToolExecutorBridgeError(t *testing.T) {
	exec := executor.New(30*time.Second, 5*time.Minute, "bash")

	bridge := func(ctx context.Context, toolName string, params map[string]any) (string, error) {
		result, err := exec.Execute(ctx, toolName, params)
		if err != nil {
			return "", err
		}
		output := result.Output
		if result.Error != "" {
			output += "\nError: " + result.Error
		}
		return output, nil
	}

	// Test with unknown tool
	ctx := context.Background()
	_, err := bridge(ctx, "unknown_tool", map[string]any{})
	if err == nil {
		t.Error("bridge should have returned an error for unknown tool")
	}
}

// TestHealthHandlerJSON tests that the health endpoint returns proper JSON
func TestHealthHandlerJSON(t *testing.T) {
	agent := &Agent{
		logger:    logging.Get(),
		startTime: time.Now().Add(-1 * time.Minute), // 1 minute ago
	}

	// Create a test request
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	// Call the handler
	agent.healthHandler(w, req)

	// Check status code
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// Check content type
	if w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected content-type application/json, got %s", w.Header().Get("Content-Type"))
	}

	// Parse response
	var resp healthResponse
	body, err := io.ReadAll(w.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	err = json.Unmarshal(body, &resp)
	if err != nil {
		t.Fatalf("failed to parse JSON response: %v", err)
	}

	// Verify response fields — agent not connected in test, so status is "disconnected"
	if resp.Status != "disconnected" {
		t.Errorf("expected status 'disconnected', got %q", resp.Status)
	}
	if resp.Connected != false {
		t.Errorf("expected connected false, got %v", resp.Connected)
	}
	if resp.Uptime == "" {
		t.Error("expected non-empty uptime")
	}
}

// TestHealthHandlerInvalidMethod tests health endpoint rejects non-GET requests
func TestHealthHandlerInvalidMethod(t *testing.T) {
	agent := &Agent{
		logger:    logging.Get(),
		startTime: time.Now(),
	}

	req := httptest.NewRequest("POST", "/health", nil)
	w := httptest.NewRecorder()

	agent.healthHandler(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

// TestHealthHandlerWithLastMessage tests health endpoint includes last message time
func TestHealthHandlerWithLastMessage(t *testing.T) {
	agent := &Agent{
		logger:    logging.Get(),
		startTime: time.Now(),
		lastMsg:   time.Now(),
	}

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	agent.healthHandler(w, req)

	var resp healthResponse
	body, _ := io.ReadAll(w.Body)
	_ = json.Unmarshal(body, &resp)

	if resp.LastMsg == "" {
		t.Error("expected last_message to be set")
	}
}

// TestStartHealthServer tests that the health server starts correctly
func TestStartHealthServer(t *testing.T) {
	agent := &Agent{
		logger:    logging.Get(),
		startTime: time.Now(),
	}

	// Start the server on a random port
	err := agent.startHealthServer("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start health server: %v", err)
	}

	// Give the server a moment to start
	time.Sleep(100 * time.Millisecond)

	if agent.health == nil {
		t.Error("expected health server to be set")
	}

	// Cleanup
	if agent.health != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_ = agent.health.Shutdown(ctx)
	}
}

// TestMessageHandlerProcessing tests that message handler processes messages correctly
func TestMessageHandlerProcessing(t *testing.T) {
	// Create a mock converser that returns a simple text response
	mockConverser := &MockConverser{
		response: &ai.Message{
			Role: "assistant",
			Content: []ai.ContentBlock{
				{
					Type: "text",
					Text: "Command executed successfully",
				},
			},
		},
	}

	// Initialize logging
	if err := logging.Setup("info", "json", ""); err != nil {
		t.Fatalf("failed to setup logging: %v", err)
	}

	// Create an agent with the mock converser
	processor := ai.NewProcessor(ai.ProcessorConfig{
		Converser:     mockConverser,
		SystemPrompt:  "Test prompt",
		Tools:         []ai.ToolDef{},
		MaxTokens:     1024,
		MaxIterations: 5,
		ExecuteTool: func(ctx context.Context, toolName string, params map[string]any) (string, error) {
			return "tool result", nil
		},
	})

	agent := &Agent{
		logger:        logging.Get(),
		processor:     processor,
		mode:          &MockMode{},
		conversations: NewConversationManager(20),
		allowlist:     connect.NewAllowlist(nil),
		cfg: &config.Config{
			Logging: config.LoggingConfig{
				Level: "info",
			},
		},
		startTime: time.Now(),
	}

	// Create a mock mode to capture sent message
	mockMode := &MockMode{}
	agent.mode = mockMode

	ctx := context.Background()
	msg := connect.IncomingMessage{
		ID:       "msg-1",
		SpaceID:  "space-1",
		PersonID: "person-1",
		Email:    "user@example.com",
		Text:     "list processes",
	}

	// Call the message handler
	agent.messageHandler(ctx, msg)

	// Verify lastMsg was updated
	if agent.lastMsg.IsZero() {
		t.Error("expected lastMsg to be set")
	}
}

// TestGetUsername asserts getUsername resolves from the OS and ignores the
// caller-controlled USER/USERNAME env vars.
func TestGetUsername(t *testing.T) {
	current, err := user.Current()
	if err != nil {
		t.Fatalf("user.Current() failed: %v", err)
	}
	if got := getUsername(); got != current.Username {
		t.Errorf("getUsername() = %q, want %q", got, current.Username)
	}

	// Env vars must not influence the result.
	t.Setenv("USER", "spoofed")
	t.Setenv("USERNAME", "spoofed")
	if got := getUsername(); got != current.Username {
		t.Errorf("getUsername() = %q after env spoofing, want %q", got, current.Username)
	}
}

// TestMessageHandlerError tests error handling in message processing
func TestMessageHandlerError(t *testing.T) {
	// Create a mock converser that returns an error
	mockConverser := &MockConverser{
		err: fmt.Errorf("converser failed"),
	}

	if err := logging.Setup("info", "json", ""); err != nil {
		t.Fatalf("failed to setup logging: %v", err)
	}

	processor := ai.NewProcessor(ai.ProcessorConfig{
		Converser:     mockConverser,
		SystemPrompt:  "Test prompt",
		Tools:         []ai.ToolDef{},
		MaxTokens:     1024,
		MaxIterations: 5,
		ExecuteTool: func(ctx context.Context, toolName string, params map[string]any) (string, error) {
			return "", nil
		},
	})

	mockMode := &MockMode{}
	agent := &Agent{
		logger:        logging.Get(),
		processor:     processor,
		mode:          mockMode,
		conversations: NewConversationManager(20),
		allowlist:     connect.NewAllowlist(nil),
		cfg: &config.Config{
			Logging: config.LoggingConfig{
				Level: "info",
			},
		},
		startTime: time.Now(),
	}

	ctx := context.Background()
	msg := connect.IncomingMessage{
		ID:       "msg-1",
		SpaceID:  "space-1",
		PersonID: "person-1",
		Email:    "user@example.com",
		Text:     "test",
	}

	// This should not panic even though converser fails
	agent.messageHandler(ctx, msg)
	if agent.lastMsg.IsZero() {
		t.Error("expected lastMsg to be set even on error")
	}
}

// newPassthroughAgent builds an agent in passthrough mode (processor == nil)
// with a real executor whose command policy has the destructive rules enabled,
// mirroring production wiring.
func newPassthroughAgent(t *testing.T, challenge string) (*Agent, *MockMode) {
	t.Helper()
	if err := logging.Setup("info", "json", ""); err != nil {
		t.Fatalf("failed to setup logging: %v", err)
	}
	exec := executor.New(30*time.Second, 5*time.Minute, "")
	exec.SetCommandPolicy(security.NewCommandPolicy(security.CommandPolicyOptions{BlockDangerous: true}))
	mockMode := &MockMode{}
	agent := &Agent{
		logger:         logging.Get(),
		processor:      nil, // passthrough: no inference
		exec:           exec,
		mode:           mockMode,
		conversations:  NewConversationManager(20),
		challengeStore: security.NewChallengeStore(challenge),
		allowlist:      connect.NewAllowlist(nil),
		cfg: &config.Config{
			AI:      config.AIConfig{Mode: config.AIModePassthrough},
			Logging: config.LoggingConfig{Level: "info"},
		},
		startTime: time.Now(),
	}
	return agent, mockMode
}

// TestPassthroughExecutesCommand verifies that in passthrough mode a benign
// message is run directly as a command and its output is returned.
func TestPassthroughExecutesCommand(t *testing.T) {
	agent, mockMode := newPassthroughAgent(t, "")
	defer agent.challengeStore.Close()

	// A portable, harmless command. "echo" exists on both sh and PowerShell.
	msg := connect.IncomingMessage{
		SpaceID: "space-1",
		Email:   "user@example.com",
		Text:    "echo passthrough_ok",
	}
	agent.messageHandler(context.Background(), msg)

	if agent.lastMsg.IsZero() {
		t.Error("expected lastMsg to be set")
	}
	if !strings.Contains(mockMode.sentText, "passthrough_ok") {
		t.Errorf("expected command output in response, got %q", mockMode.sentText)
	}
}

// TestPassthroughBlocksDangerousCommand verifies that the dangerous-command
// guardrail still gates passthrough execution. With challenge-response
// disabled, a blocked command must NOT execute and the response must surface
// the block.
func TestPassthroughBlocksDangerousCommand(t *testing.T) {
	agent, mockMode := newPassthroughAgent(t, "")
	defer agent.challengeStore.Close()

	msg := connect.IncomingMessage{
		SpaceID: "space-1",
		Email:   "user@example.com",
		Text:    "rm -rf /",
	}
	agent.messageHandler(context.Background(), msg)

	if !strings.Contains(mockMode.sentText, "blocked") && !strings.Contains(mockMode.sentText, "Blocked") {
		t.Errorf("expected dangerous command to be blocked, got %q", mockMode.sentText)
	}
}

// TestPassthroughDangerousCommandPromptsChallenge verifies that when
// challenge-response is enabled, a blocked passthrough command registers a
// pending challenge and returns the confirmation prompt rather than executing.
func TestPassthroughDangerousCommandPromptsChallenge(t *testing.T) {
	// A valid AES-GCM challenge ciphertext so the store is enabled.
	ciphertext, err := security.EncryptChallenge("test-passphrase")
	if err != nil {
		t.Fatalf("failed to build challenge: %v", err)
	}
	agent, mockMode := newPassthroughAgent(t, ciphertext)
	defer agent.challengeStore.Close()

	msg := connect.IncomingMessage{
		SpaceID: "space-1",
		Email:   "user@example.com",
		Text:    "rm -rf /",
	}
	agent.messageHandler(context.Background(), msg)

	if !strings.Contains(mockMode.sentText, "requires confirmation") {
		t.Errorf("expected challenge confirmation prompt, got %q", mockMode.sentText)
	}
}

// TestPassthroughSecretReadGetsNoChallengePrompt is the agent-level half of the
// hard/confirmable split. A config/secret read must be refused outright, never
// offered as a challenge: the response would arrive over the same chat channel
// whose credentials the lockdown protects, so a leaked or coerced passphrase
// would unlock exactly the secrets at stake. Before consolidation every block
// carried the same "Command blocked:" prefix, and executeToolGuarded offered a
// challenge for all of them.
func TestPassthroughSecretReadGetsNoChallengePrompt(t *testing.T) {
	ciphertext, err := security.EncryptChallenge("test-passphrase")
	if err != nil {
		t.Fatalf("failed to build challenge: %v", err)
	}
	agent, mockMode := newPassthroughAgent(t, ciphertext)
	defer agent.challengeStore.Close()

	// Re-wire the executor with the lockdown rule group active, as agent.New does.
	dir := t.TempDir()
	guard := executor.NewGuard(true, []string{dir})
	agent.exec.SetGuard(guard)
	agent.exec.SetCommandPolicy(security.NewCommandPolicy(security.CommandPolicyOptions{
		BlockDangerous:   true,
		BlockSecretReads: true,
		ProtectedPaths:   guard.ProtectedPaths(),
	}))

	protected := filepath.Join(dir, "config.yaml")
	// "sudo cat <protected>" matches privilege escalation (confirmable) as well
	// as a protected-path read (hard). The hard denial must win.
	msg := connect.IncomingMessage{
		SpaceID: "space-1",
		Email:   "user@example.com",
		Text:    "sudo cat " + protected,
	}
	agent.messageHandler(context.Background(), msg)

	if strings.Contains(mockMode.sentText, "requires confirmation") {
		t.Errorf("secret reads must not be offered as a challenge, got %q", mockMode.sentText)
	}
	if !strings.Contains(mockMode.sentText, "local administration") {
		t.Errorf("expected a hard-denial message, got %q", mockMode.sentText)
	}
	// Nothing may be pending: replying with the correct passphrase must find no
	// stored command to run.
	if pc, ok := agent.challengeStore.CheckResponse("space-1", "test-passphrase"); ok {
		t.Errorf("no pending challenge may be registered for a hard denial, got %q", pc.Command)
	}
}

// TestAuthorizeChokePoint is the regression guard for the wmcp authz gap: the
// allowlist decision lives in Agent.messageHandler, so it applies to every
// connection mode identically. RoomType "group" is what WMCPMode reports (a
// relay cannot prove a 1:1 space), so the group cases below are the wmcp cases.
func TestAuthorizeChokePoint(t *testing.T) {
	tests := []struct {
		name     string
		allowed  []string
		email    string
		roomType string
		want     bool
	}{
		// wmcp mode reports RoomType "group".
		{"wmcp unlisted sender denied", []string{"alice@example.com"}, "eve@evil.com", "group", false},
		{"wmcp listed sender allowed", []string{"alice@example.com"}, "alice@example.com", "group", true},
		{"wmcp listed sender case-insensitive", []string{"Alice@Example.com"}, "ALICE@example.COM", "group", true},
		{"wmcp empty allowlist denies everyone", nil, "anyone@example.com", "group", false},

		// native group rooms — same strict semantics.
		{"native group unlisted denied", []string{"alice@example.com"}, "eve@evil.com", "group", false},
		{"native group empty allowlist denies", nil, "alice@example.com", "group", false},

		// native direct messages — empty list is permissive by design.
		{"direct unlisted denied when list populated", []string{"alice@example.com"}, "eve@evil.com", "direct", false},
		{"direct listed allowed", []string{"alice@example.com"}, "alice@example.com", "direct", true},
		{"direct empty allowlist allows all", nil, "anyone@example.com", "direct", true},
		{"empty roomType treated as direct", nil, "anyone@example.com", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &Agent{allowlist: connect.NewAllowlist(tt.allowed)}
			got := a.authorize(connect.IncomingMessage{Email: tt.email, RoomType: tt.roomType})
			if got != tt.want {
				t.Errorf("authorize(%q, %q) = %v, want %v", tt.email, tt.roomType, got, tt.want)
			}
		})
	}
}

// TestAuthorizeFailsClosedWithoutAllowlist verifies a misconstructed Agent
// denies rather than admits. Authorization must never depend on a field having
// been remembered.
func TestAuthorizeFailsClosedWithoutAllowlist(t *testing.T) {
	a := &Agent{}
	if a.authorize(connect.IncomingMessage{Email: "anyone@example.com", RoomType: "direct"}) {
		t.Error("authorize() with nil allowlist must deny")
	}
}

// TestMessageHandlerRejectsUnauthorizedSender proves the gate is wired into the
// request path — an unauthorized sender reaches neither the executor nor a
// reply. Uses passthrough mode so a pass would visibly execute a command.
func TestMessageHandlerRejectsUnauthorizedSender(t *testing.T) {
	agent, mockMode := newPassthroughAgent(t, "")
	defer agent.challengeStore.Close()

	// Populated allowlist that does not include the sender.
	agent.allowlist = connect.NewAllowlist([]string{"alice@example.com"})

	agent.messageHandler(context.Background(), connect.IncomingMessage{
		SpaceID:  "space-1",
		Email:    "eve@evil.com",
		Text:     "echo unauthorized_ran",
		RoomType: "direct",
	})

	if mockMode.sentText != "" {
		t.Errorf("unauthorized sender got a reply: %q", mockMode.sentText)
	}
}

// TestMessageHandlerRejectsUnauthorizedWMCPSender is the same check for the
// wmcp path: RoomType "group" with an empty allowlist must deny, which is the
// exact configuration that ran unauthorized before this fix.
func TestMessageHandlerRejectsUnauthorizedWMCPSender(t *testing.T) {
	agent, mockMode := newPassthroughAgent(t, "")
	defer agent.challengeStore.Close()

	// Empty allowlist — the default that wmcp mode previously ignored entirely.
	agent.allowlist = connect.NewAllowlist(nil)

	agent.messageHandler(context.Background(), connect.IncomingMessage{
		SpaceID:  "space-1",
		Email:    "relay-forwarded@anywhere.com",
		Text:     "echo wmcp_unauthorized_ran",
		RoomType: "group", // what WMCPMode reports
	})

	if mockMode.sentText != "" {
		t.Errorf("unauthorized wmcp sender got a reply: %q", mockMode.sentText)
	}
}

// TestMessageHandlerAllowsAuthorizedSender is the positive control: the same
// path with the sender listed must execute and reply.
func TestMessageHandlerAllowsAuthorizedSender(t *testing.T) {
	agent, mockMode := newPassthroughAgent(t, "")
	defer agent.challengeStore.Close()

	agent.allowlist = connect.NewAllowlist([]string{"alice@example.com"})

	agent.messageHandler(context.Background(), connect.IncomingMessage{
		SpaceID:  "space-1",
		Email:    "alice@example.com",
		Text:     "echo authorized_ran",
		RoomType: "group",
	})

	if !strings.Contains(mockMode.sentText, "authorized_ran") {
		t.Errorf("authorized sender did not get command output, got %q", mockMode.sentText)
	}
}

// TestHealthResponseStructure verifies the health response has the expected structure
func TestHealthResponseStructure(t *testing.T) {
	var resp healthResponse
	resp.Status = "healthy"
	resp.Uptime = "1m0s"
	resp.Connected = true
	resp.LastMsg = time.Now().Format(time.RFC3339)

	// Verify all fields are set
	if resp.Status == "" {
		t.Error("status should not be empty")
	}
	if resp.Uptime == "" {
		t.Error("uptime should not be empty")
	}
	if !resp.Connected {
		t.Error("connected should be true")
	}

	// Verify JSON marshaling works
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal to JSON: %v", err)
	}

	var unmarshaled healthResponse
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("failed to unmarshal from JSON: %v", err)
	}

	if unmarshaled.Status != "healthy" {
		t.Error("status not preserved through JSON marshal/unmarshal")
	}
}

// BenchmarkToolExecutorBridge benchmarks the tool executor bridge
func BenchmarkToolExecutorBridge(b *testing.B) {
	exec := executor.New(30*time.Second, 5*time.Minute, "bash")
	bridge := func(ctx context.Context, toolName string, params map[string]any) (string, error) {
		result, err := exec.Execute(ctx, toolName, params)
		if err != nil {
			return "", err
		}
		output := result.Output
		if result.Error != "" {
			output += "\nError: " + result.Error
		}
		return output, nil
	}

	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = bridge(ctx, "system_info", map[string]any{})
	}
}

// BenchmarkHealthHandler benchmarks the health handler
func BenchmarkHealthHandler(b *testing.B) {
	agent := &Agent{
		logger:    logging.Get(),
		startTime: time.Now(),
	}

	req := httptest.NewRequest("GET", "/health", nil)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		agent.healthHandler(w, req)
	}
}

// TestHealthServerRespectsConcurrency tests concurrent health handler access
func TestHealthServerRespectsConcurrency(t *testing.T) {
	agent := &Agent{
		logger:    logging.Get(),
		startTime: time.Now(),
	}

	// Run multiple concurrent requests
	results := make(chan int, 10)
	for i := 0; i < 10; i++ {
		go func() {
			req := httptest.NewRequest("GET", "/health", nil)
			w := httptest.NewRecorder()
			agent.healthHandler(w, req)
			results <- w.Code
		}()
	}

	// Verify all requests succeeded
	for i := 0; i < 10; i++ {
		code := <-results
		if code != http.StatusOK {
			t.Errorf("expected status 200, got %d", code)
		}
	}
}

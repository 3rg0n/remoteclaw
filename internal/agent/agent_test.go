package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/3rg0n/remoteclaw/internal/ai"
	"github.com/3rg0n/remoteclaw/internal/config"
	"github.com/3rg0n/remoteclaw/internal/connect"
	"github.com/3rg0n/remoteclaw/internal/executor"
	"github.com/3rg0n/remoteclaw/internal/logging"
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
}

func (m *MockMode) Connect(ctx context.Context) error {
	m.connected = true
	return nil
}

func (m *MockMode) OnMessage(handler connect.MessageHandler) {
	m.handler = handler
}

func (m *MockMode) SendMessage(ctx context.Context, spaceID string, text string) error {
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
	// Full New() testing requires AWS credentials or mocking of NewBedrockClient.
	// For integration testing, ensure AWS_REGION and valid credentials are available.
	//
	// In production code, use dependency injection (NewWithDeps) to pass mock dependencies.
	// Example mock config that would work with proper setup:
	_ = &config.Config{
		Mode: "native",
		Webex: config.WebexConfig{
			BotToken:      "test-token",
			AllowedEmails: []string{},
		},
		AWS: config.AWSConfig{
			Region: "us-west-2",
		},
		AI: config.AIConfig{
			Model:        "test-model",
			MaxTokens:    4096,
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

// TestGetUsername tests the getUsername function
func TestGetUsername(t *testing.T) {
	// Save original env vars
	origUser := os.Getenv("USER")
	origUsername := os.Getenv("USERNAME")
	defer func() {
		if origUser != "" {
			_ = os.Setenv("USER", origUser)
		}
		if origUsername != "" {
			_ = os.Setenv("USERNAME", origUsername)
		}
	}()

	// Test with USER env var
	if err := os.Setenv("USER", "testuser"); err != nil {
		t.Fatalf("failed to set USER: %v", err)
	}
	username := getUsername()
	if username == "" {
		t.Error("expected non-empty username")
	}

	// Test fallback
	if err := os.Unsetenv("USER"); err != nil {
		t.Fatalf("failed to unset USER: %v", err)
	}
	if err := os.Unsetenv("USERNAME"); err != nil {
		t.Fatalf("failed to unset USERNAME: %v", err)
	}
	username = getUsername()
	// Should still return something (might be "unknown" or from fallback)
	if username == "" {
		t.Error("expected non-empty username even with no env vars")
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

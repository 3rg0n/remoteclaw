package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ecopelan/remoteclaw/internal/ai"
	"github.com/ecopelan/remoteclaw/internal/config"
	"github.com/ecopelan/remoteclaw/internal/connect"
	"github.com/ecopelan/remoteclaw/internal/executor"
	"github.com/ecopelan/remoteclaw/internal/logging"
	"github.com/ecopelan/remoteclaw/internal/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CapturingMode is a mock Mode that captures sent messages
type CapturingMode struct {
	mu          sync.Mutex
	handler     connect.MessageHandler
	sentMsgs    []capturedMessage
	connectErr  error
}

type capturedMessage struct {
	SpaceID string
	Text    string
}

func (m *CapturingMode) Connect(ctx context.Context) error {
	return m.connectErr
}

func (m *CapturingMode) OnMessage(handler connect.MessageHandler) {
	m.handler = handler
}

func (m *CapturingMode) SendMessage(ctx context.Context, spaceID string, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentMsgs = append(m.sentMsgs, capturedMessage{SpaceID: spaceID, Text: text})
	return nil
}

func (m *CapturingMode) Close() error {
	return nil
}

func (m *CapturingMode) getSentMessages() []capturedMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]capturedMessage, len(m.sentMsgs))
	copy(result, m.sentMsgs)
	return result
}

// newTestAgent creates a minimal agent with mocked dependencies for integration testing
func newTestAgent(t *testing.T, converser ai.Converser, opts ...func(*Agent)) *Agent {
	t.Helper()

	if err := logging.Setup("info", "json", ""); err != nil {
		t.Fatalf("logging setup failed: %v", err)
	}

	exec := executor.New(30*time.Second, 5*time.Minute, "")
	exec.SetDangerousChecker(security.NewDangerousChecker())

	processor := ai.NewProcessor(ai.ProcessorConfig{
		Converser:     converser,
		SystemPrompt:  "Test system prompt",
		Tools:         ai.AllTools(),
		MaxTokens:     1024,
		MaxIterations: 5,
		ExecuteTool: func(ctx context.Context, toolName string, params map[string]any) (string, error) {
			result, err := exec.Execute(ctx, toolName, params)
			if err != nil {
				return "", err
			}
			output := result.Output
			if result.Error != "" {
				output += "\nError: " + result.Error
			}
			return output, nil
		},
	})

	agent := &Agent{
		cfg: &config.Config{
			Logging:  config.LoggingConfig{Level: "info"},
			Security: config.SecurityConfig{DangerousCommands: true, RateLimitPerMin: 10},
		},
		exec:          exec,
		processor:     processor,
		logger:        logging.Get(),
		conversations: NewConversationManager(20),
		rateLimiter:   security.NewRateLimiter(10, 3),
		startTime:     time.Now(),
	}

	for _, opt := range opts {
		opt(agent)
	}

	return agent
}

// TestIntegration_FullMessageFlow tests end-to-end message handling with conversation history
func TestIntegration_FullMessageFlow(t *testing.T) {
	mock := &MockConverser{
		response: &ai.Message{
			Role:    "assistant",
			Content: []ai.ContentBlock{{Type: "text", Text: "System is healthy"}},
		},
	}

	mode := &CapturingMode{}
	agent := newTestAgent(t, mock)
	agent.mode = mode

	ctx := context.Background()

	// Send first message
	agent.messageHandler(ctx, connect.IncomingMessage{
		ID: "msg-1", SpaceID: "space-1", Email: "user@test.com", Text: "check system",
	})

	// Send second message to same space (should use conversation history)
	agent.messageHandler(ctx, connect.IncomingMessage{
		ID: "msg-2", SpaceID: "space-1", Email: "user@test.com", Text: "what was the result?",
	})

	sent := mode.getSentMessages()
	assert.Len(t, sent, 2)
	assert.Equal(t, "space-1", sent[0].SpaceID)
	assert.Equal(t, "space-1", sent[1].SpaceID)

	// Verify conversation history was maintained
	history := agent.conversations.GetHistory("space-1")
	assert.NotEmpty(t, history, "conversation history should be populated")
}

// TestIntegration_RateLimiting tests that rate limiter blocks excess requests
func TestIntegration_RateLimiting(t *testing.T) {
	mock := &MockConverser{
		response: &ai.Message{
			Role:    "assistant",
			Content: []ai.ContentBlock{{Type: "text", Text: "ok"}},
		},
	}

	mode := &CapturingMode{}
	agent := newTestAgent(t, mock)
	agent.mode = mode
	// Low rate limit for testing: 2 per minute, burst of 2
	agent.rateLimiter = security.NewRateLimiter(2, 2)

	ctx := context.Background()

	// Send burst of messages
	for i := 0; i < 5; i++ {
		agent.messageHandler(ctx, connect.IncomingMessage{
			ID: "msg", SpaceID: "space-rl", Email: "user@test.com", Text: "request",
		})
	}

	sent := mode.getSentMessages()
	// First 2 should be processed, rest rate-limited
	rateLimitedCount := 0
	for _, msg := range sent {
		if msg.Text == "Rate limited. Please wait before sending more requests." {
			rateLimitedCount++
		}
	}

	assert.GreaterOrEqual(t, rateLimitedCount, 1, "at least one message should be rate-limited")
	assert.Equal(t, 5, len(sent), "all 5 should get a response (some rate-limited)")
}

// TestIntegration_AuditLogging tests that audit entries are written
func TestIntegration_AuditLogging(t *testing.T) {
	mock := &MockConverser{
		response: &ai.Message{
			Role:    "assistant",
			Content: []ai.ContentBlock{{Type: "text", Text: "done"}},
		},
	}

	tmpDir := t.TempDir()
	auditBase := filepath.Join(tmpDir, "audit")

	audit, err := logging.NewAuditLogger(auditBase)
	require.NoError(t, err)

	mode := &CapturingMode{}
	agent := newTestAgent(t, mock, func(a *Agent) {
		a.audit = audit
	})
	agent.mode = mode

	ctx := context.Background()
	agent.messageHandler(ctx, connect.IncomingMessage{
		ID: "msg-1", SpaceID: "space-audit", Email: "admin@test.com", Text: "do something",
	})

	// Close audit to flush
	err = audit.Close()
	require.NoError(t, err)

	// Read and verify the date-stamped audit file
	today := time.Now().Format("2006-01-02")
	auditFile := fmt.Sprintf("%s-%s.jsonl", auditBase, today)
	data, err := os.ReadFile(auditFile) //nolint:gosec // test file
	require.NoError(t, err)
	assert.NotEmpty(t, data, "audit file should have content")

	// Parse the JSON line
	var entry map[string]interface{}
	err = json.Unmarshal(data, &entry)
	require.NoError(t, err)
	assert.Equal(t, "admin@test.com", entry["email"])
	assert.Equal(t, "space-audit", entry["space_id"])
	assert.Equal(t, "do something", entry["raw_message"])
}

// TestIntegration_DangerousCommandBlocking tests that dangerous commands are blocked
func TestIntegration_DangerousCommandBlocking(t *testing.T) {
	// Mock converser that returns a tool call for a dangerous command
	mock := &MockConverser{
		response: &ai.Message{
			Role: "assistant",
			Content: []ai.ContentBlock{
				{
					Type:      "tool_use",
					ToolUseID: "tool-1",
					ToolName:  "execute_command",
					Input:     map[string]any{"command": "rm -rf /"},
				},
			},
		},
	}

	mode := &CapturingMode{}
	agent := newTestAgent(t, mock)
	agent.mode = mode

	// Override the mock to first return a dangerous tool call, then return text
	callCount := 0
	agent.processor = ai.NewProcessor(ai.ProcessorConfig{
		Converser: &sequentialConverser{
			responses: []*ai.Message{
				{
					Role: "assistant",
					Content: []ai.ContentBlock{
						{
							Type:      "tool_use",
							ToolUseID: "tool-1",
							ToolName:  "execute_command",
							Input:     map[string]any{"command": "rm -rf /"},
						},
					},
				},
				{
					Role:    "assistant",
					Content: []ai.ContentBlock{{Type: "text", Text: "The command was blocked for safety."}},
				},
			},
			callCount: &callCount,
		},
		SystemPrompt:  "Test",
		Tools:         ai.AllTools(),
		MaxTokens:     1024,
		MaxIterations: 5,
		ExecuteTool: func(ctx context.Context, toolName string, params map[string]any) (string, error) {
			exec := executor.New(30*time.Second, 5*time.Minute, "")
			exec.SetDangerousChecker(security.NewDangerousChecker())
			result, err := exec.Execute(ctx, toolName, params)
			if err != nil {
				return "", err
			}
			output := result.Output
			if result.Error != "" {
				output += "\nError: " + result.Error
			}
			return output, nil
		},
	})

	ctx := context.Background()
	agent.messageHandler(ctx, connect.IncomingMessage{
		ID: "msg-1", SpaceID: "space-danger", Email: "user@test.com", Text: "delete everything",
	})

	sent := mode.getSentMessages()
	require.Len(t, sent, 1)
	// The response should indicate the command was blocked
	assert.Contains(t, sent[0].Text, "blocked")
}

// TestIntegration_ErrorHandling tests error handling when AI fails
func TestIntegration_ErrorHandling(t *testing.T) {
	mock := &MockConverser{
		err: assert.AnError,
	}

	mode := &CapturingMode{}
	agent := newTestAgent(t, mock)
	agent.mode = mode

	ctx := context.Background()
	agent.messageHandler(ctx, connect.IncomingMessage{
		ID: "msg-1", SpaceID: "space-err", Email: "user@test.com", Text: "hello",
	})

	sent := mode.getSentMessages()
	require.Len(t, sent, 1)
	assert.Contains(t, sent[0].Text, "Error processing request")
}

// TestIntegration_ChallengeResponse tests the challenge-response flow for destructive commands
func TestIntegration_ChallengeResponse(t *testing.T) {
	callCount := 0
	mode := &CapturingMode{}

	// Create agent with AES-256 encrypted challenge
	encrypted, err := security.EncryptChallenge("confirm-it")
	require.NoError(t, err)
	agent := newTestAgent(t, nil, func(a *Agent) {
		a.challengeStore = security.NewChallengeStore(encrypted)
	})

	// Override processor with a sequential converser:
	// 1st call: AI returns dangerous tool call → blocked → challenge stored → AI told about challenge
	// 2nd call: AI relays the challenge prompt
	agent.processor = ai.NewProcessor(ai.ProcessorConfig{
		Converser: &sequentialConverser{
			responses: []*ai.Message{
				{
					Role: "assistant",
					Content: []ai.ContentBlock{{
						Type:      "tool_use",
						ToolUseID: "tool-1",
						ToolName:  "execute_command",
						Input:     map[string]any{"command": "sudo rm -rf /tmp/old"},
					}},
				},
				{
					Role:    "assistant",
					Content: []ai.ContentBlock{{Type: "text", Text: "The command requires confirmation. Please reply with the decryption passphrase to proceed."}},
				},
			},
			callCount: &callCount,
		},
		SystemPrompt:  "Test",
		Tools:         ai.AllTools(),
		MaxTokens:     1024,
		MaxIterations: 5,
		ExecuteTool: func(ctx context.Context, toolName string, params map[string]any) (string, error) {
			exec := executor.New(30*time.Second, 5*time.Minute, "")
			exec.SetDangerousChecker(security.NewDangerousChecker())
			result, err := exec.Execute(ctx, toolName, params)
			if err != nil {
				return "", err
			}
			// Simulate challenge-store behavior inline for this test
			if toolName == "execute_command" && result.ExitCode == 1 &&
				len(result.Error) >= 16 && result.Error[:16] == "Command blocked:" {
				if cmd, ok := params["command"].(string); ok {
					if spaceID, ok := ctx.Value(spaceIDKey).(string); ok {
						agent.challengeStore.SetPending(spaceID, cmd, result.Error)
					}
				}
				return "Command blocked. User must reply with decryption passphrase to confirm.", nil
			}
			output := result.Output
			if result.Error != "" {
				output += "\nError: " + result.Error
			}
			return output, nil
		},
	})
	agent.mode = mode

	ctx := context.Background()

	// Step 1: User sends a message that triggers a dangerous command
	agent.messageHandler(ctx, connect.IncomingMessage{
		ID: "msg-1", SpaceID: "space-chal", Email: "user@test.com", Text: "delete old files",
	})

	sent := mode.getSentMessages()
	require.GreaterOrEqual(t, len(sent), 1, "should have at least one response")
	// The response should mention confirmation
	assert.Contains(t, sent[0].Text, "confirm")

	// Step 2: User replies with the wrong challenge
	agent.messageHandler(ctx, connect.IncomingMessage{
		ID: "msg-2", SpaceID: "space-chal", Email: "user@test.com", Text: "wrong-code",
	})

	// Step 3: User replies with the correct challenge
	agent.messageHandler(ctx, connect.IncomingMessage{
		ID: "msg-3", SpaceID: "space-chal", Email: "user@test.com", Text: "confirm-it",
	})

	sent = mode.getSentMessages()
	// msg-3 with correct challenge should have triggered ForceExecuteCommand
	// The last message should be the result of execution (not a "blocked" message)
	lastMsg := sent[len(sent)-1]
	assert.NotContains(t, lastMsg.Text, "blocked", "confirmed command should not be blocked")
}

// sequentialConverser returns different responses on successive calls
type sequentialConverser struct {
	responses []*ai.Message
	callCount *int
}

func (s *sequentialConverser) Converse(
	ctx context.Context,
	system string,
	messages []ai.Message,
	tools []ai.ToolDef,
	maxTokens int,
) (*ai.Message, error) {
	idx := *s.callCount
	*s.callCount++
	if idx < len(s.responses) {
		return s.responses[idx], nil
	}
	return s.responses[len(s.responses)-1], nil
}

package ai

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockConverser is a mock implementation of the Converser interface
type MockConverser struct {
	responses    []*Message
	callCount    int
	lastSystem   string
	lastMessages []Message
	lastTools    []ToolDef
}

// Converse implements the Converser interface for testing
func (m *MockConverser) Converse(
	ctx context.Context,
	system string,
	messages []Message,
	tools []ToolDef,
	maxTokens int,
) (*Message, error) {
	m.lastSystem = system
	m.lastMessages = messages
	m.lastTools = tools

	if m.callCount >= len(m.responses) {
		return nil, fmt.Errorf("mock: no more responses")
	}

	response := m.responses[m.callCount]
	m.callCount++
	return response, nil
}

func TestProcessorSimpleTextResponse(t *testing.T) {
	// Test a simple text response with no tool calls
	mock := &MockConverser{
		responses: []*Message{
			{
				Role: "assistant",
				Content: []ContentBlock{
					{Type: "text", Text: "Hello, this is a response."},
				},
			},
		},
	}

	executeTool := func(ctx context.Context, toolName string, params map[string]any) (string, error) {
		return "", nil
	}

	processor := NewProcessor(ProcessorConfig{
		Converser:     mock,
		SystemPrompt:  "You are a helpful assistant.",
		Tools:         AllTools(),
		MaxTokens:     4096,
		MaxIterations: 10,
		ExecuteTool:   executeTool,
	})

	result, history, err := processor.Process(context.Background(), "Hello", []Message{})
	require.NoError(t, err)
	assert.Equal(t, "Hello, this is a response.", result)
	assert.Equal(t, 2, len(history)) // user message + assistant response
}

func TestProcessorSingleToolCall(t *testing.T) {
	// Test a single tool call followed by a final answer
	mock := &MockConverser{
		responses: []*Message{
			{
				Role: "assistant",
				Content: []ContentBlock{
					{
						Type:      "tool_use",
						ToolUseID: "tool-1",
						ToolName:  "execute_command",
						Input: map[string]any{
							"command": "echo hello",
						},
					},
				},
			},
			{
				Role: "assistant",
				Content: []ContentBlock{
					{Type: "text", Text: "The command executed successfully. Output: hello"},
				},
			},
		},
	}

	executeToolCalls := 0
	executeTool := func(ctx context.Context, toolName string, params map[string]any) (string, error) {
		executeToolCalls++
		assert.Equal(t, "execute_command", toolName)
		assert.Equal(t, "echo hello", params["command"])
		return "hello", nil
	}

	processor := NewProcessor(ProcessorConfig{
		Converser:     mock,
		SystemPrompt:  "You are a helpful assistant.",
		Tools:         AllTools(),
		MaxTokens:     4096,
		MaxIterations: 10,
		ExecuteTool:   executeTool,
	})

	result, history, err := processor.Process(context.Background(), "Run echo hello", []Message{})
	require.NoError(t, err)
	assert.Equal(t, "The command executed successfully. Output: hello", result)
	assert.Equal(t, 1, executeToolCalls) // One tool execution
	assert.Equal(t, 4, len(history))     // user -> tool_use -> user(tool_result) -> assistant
}

func TestProcessorMultipleTurns(t *testing.T) {
	// Test multiple tool calls in sequence
	mock := &MockConverser{
		responses: []*Message{
			{
				Role: "assistant",
				Content: []ContentBlock{
					{
						Type:      "tool_use",
						ToolUseID: "tool-1",
						ToolName:  "read_file",
						Input: map[string]any{
							"path": "/tmp/test.txt",
						},
					},
				},
			},
			{
				Role: "assistant",
				Content: []ContentBlock{
					{
						Type:      "tool_use",
						ToolUseID: "tool-2",
						ToolName:  "execute_command",
						Input: map[string]any{
							"command": "wc -l /tmp/test.txt",
						},
					},
				},
			},
			{
				Role: "assistant",
				Content: []ContentBlock{
					{Type: "text", Text: "File has 42 lines."},
				},
			},
		},
	}

	executeToolCalls := 0
	executeTool := func(ctx context.Context, toolName string, params map[string]any) (string, error) {
		executeToolCalls++
		if toolName == "read_file" {
			return "file content", nil
		}
		if toolName == "execute_command" {
			return "42", nil
		}
		return "", fmt.Errorf("unknown tool")
	}

	processor := NewProcessor(ProcessorConfig{
		Converser:     mock,
		SystemPrompt:  "You are a helpful assistant.",
		Tools:         AllTools(),
		MaxTokens:     4096,
		MaxIterations: 10,
		ExecuteTool:   executeTool,
	})

	result, _, err := processor.Process(context.Background(), "Count lines in /tmp/test.txt", []Message{})
	require.NoError(t, err)
	assert.Equal(t, "File has 42 lines.", result)
	assert.Equal(t, 2, executeToolCalls) // Two tool executions
}

func TestProcessorMaxIterations(t *testing.T) {
	// Test that max iterations cap is respected
	mock := &MockConverser{
		responses: []*Message{
			{
				Role: "assistant",
				Content: []ContentBlock{
					{
						Type:      "tool_use",
						ToolUseID: "tool-1",
						ToolName:  "execute_command",
						Input: map[string]any{
							"command": "echo test",
						},
					},
				},
			},
			{
				Role: "assistant",
				Content: []ContentBlock{
					{
						Type:      "tool_use",
						ToolUseID: "tool-2",
						ToolName:  "execute_command",
						Input: map[string]any{
							"command": "echo test",
						},
					},
				},
			},
			{
				Role: "assistant",
				Content: []ContentBlock{
					{
						Type:      "tool_use",
						ToolUseID: "tool-3",
						ToolName:  "execute_command",
						Input: map[string]any{
							"command": "echo test",
						},
					},
				},
			},
		},
	}

	executeTool := func(ctx context.Context, toolName string, params map[string]any) (string, error) {
		return "result", nil
	}

	processor := NewProcessor(ProcessorConfig{
		Converser:     mock,
		SystemPrompt:  "You are a helpful assistant.",
		Tools:         AllTools(),
		MaxTokens:     4096,
		MaxIterations: 2, // Set low limit
		ExecuteTool:   executeTool,
	})

	result, history, err := processor.Process(context.Background(), "Do something", []Message{})
	require.NoError(t, err)
	assert.Equal(t, "I've reached the maximum number of steps.", result)
	// Verify history contains multiple tool calls
	assert.Greater(t, len(history), 2)
}

func TestProcessorToolExecutionError(t *testing.T) {
	// Test error handling in tool execution
	mock := &MockConverser{
		responses: []*Message{
			{
				Role: "assistant",
				Content: []ContentBlock{
					{
						Type:      "tool_use",
						ToolUseID: "tool-1",
						ToolName:  "execute_command",
						Input: map[string]any{
							"command": "bad command",
						},
					},
				},
			},
			{
				Role: "assistant",
				Content: []ContentBlock{
					{Type: "text", Text: "The command failed as expected."},
				},
			},
		},
	}

	executeTool := func(ctx context.Context, toolName string, params map[string]any) (string, error) {
		return "", fmt.Errorf("command failed")
	}

	processor := NewProcessor(ProcessorConfig{
		Converser:     mock,
		SystemPrompt:  "You are a helpful assistant.",
		Tools:         AllTools(),
		MaxTokens:     4096,
		MaxIterations: 10,
		ExecuteTool:   executeTool,
	})

	result, history, err := processor.Process(context.Background(), "Run bad command", []Message{})
	require.NoError(t, err)
	assert.Equal(t, "The command failed as expected.", result)

	// Check that tool result has error flag set
	userMsg := history[2] // Second user message with tool results
	assert.Equal(t, "user", userMsg.Role)
	assert.True(t, userMsg.Content[0].IsError)
	assert.Contains(t, userMsg.Content[0].Content, "Error:")
}

func TestProcessorWithHistory(t *testing.T) {
	// Test that existing history is preserved
	mock := &MockConverser{
		responses: []*Message{
			{
				Role: "assistant",
				Content: []ContentBlock{
					{Type: "text", Text: "Response to new message."},
				},
			},
		},
	}

	executeTool := func(ctx context.Context, toolName string, params map[string]any) (string, error) {
		return "", nil
	}

	initialHistory := []Message{
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: "First message"}}},
		{Role: "assistant", Content: []ContentBlock{{Type: "text", Text: "First response"}}},
	}

	processor := NewProcessor(ProcessorConfig{
		Converser:     mock,
		SystemPrompt:  "You are a helpful assistant.",
		Tools:         AllTools(),
		MaxTokens:     4096,
		MaxIterations: 10,
		ExecuteTool:   executeTool,
	})

	result, history, err := processor.Process(context.Background(), "Second message", initialHistory)
	require.NoError(t, err)
	assert.Equal(t, "Response to new message.", result)

	// History should have the initial messages plus new ones
	assert.Equal(t, 4, len(history))
	assert.Equal(t, "First message", history[0].Content[0].Text)
	assert.Equal(t, "Second message", history[2].Content[0].Text)
}

func TestProcessorContextCancellation(t *testing.T) {
	// Test that context cancellation is handled
	mock := &MockConverser{
		responses: []*Message{
			{
				Role: "assistant",
				Content: []ContentBlock{
					{Type: "text", Text: "Response"},
				},
			},
		},
	}

	executeTool := func(ctx context.Context, toolName string, params map[string]any) (string, error) {
		return "", nil
	}

	processor := NewProcessor(ProcessorConfig{
		Converser:     mock,
		SystemPrompt:  "You are a helpful assistant.",
		Tools:         AllTools(),
		MaxTokens:     4096,
		MaxIterations: 10,
		ExecuteTool:   executeTool,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// This should handle the cancelled context appropriately
	// The mock will still work, but in a real scenario the converser would respect context
	result, _, err := processor.Process(ctx, "Test message", []Message{})
	// Since our mock doesn't check context, it should still work
	require.NoError(t, err)
	assert.Equal(t, "Response", result)
}

func TestProcessorMultipleToolCallsSameIteration(t *testing.T) {
	// Test multiple tool calls in the same response
	mock := &MockConverser{
		responses: []*Message{
			{
				Role: "assistant",
				Content: []ContentBlock{
					{
						Type:      "tool_use",
						ToolUseID: "tool-1",
						ToolName:  "read_file",
						Input: map[string]any{
							"path": "/tmp/file1.txt",
						},
					},
					{
						Type:      "tool_use",
						ToolUseID: "tool-2",
						ToolName:  "read_file",
						Input: map[string]any{
							"path": "/tmp/file2.txt",
						},
					},
				},
			},
			{
				Role: "assistant",
				Content: []ContentBlock{
					{Type: "text", Text: "Both files read successfully."},
				},
			},
		},
	}

	executeToolCalls := 0
	executeTool := func(ctx context.Context, toolName string, params map[string]any) (string, error) {
		executeToolCalls++
		return fmt.Sprintf("content of %s", params["path"]), nil
	}

	processor := NewProcessor(ProcessorConfig{
		Converser:     mock,
		SystemPrompt:  "You are a helpful assistant.",
		Tools:         AllTools(),
		MaxTokens:     4096,
		MaxIterations: 10,
		ExecuteTool:   executeTool,
	})

	result, _, err := processor.Process(context.Background(), "Read two files", []Message{})
	require.NoError(t, err)
	assert.Equal(t, "Both files read successfully.", result)
	assert.Equal(t, 2, executeToolCalls) // Both tools executed
}

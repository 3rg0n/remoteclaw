package ai

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"

	"github.com/ecopelan/remoteclaw/internal/logging"
	"github.com/ollama/ollama/api"
)

// OllamaClient wraps the Ollama API client
type OllamaClient struct {
	client    *api.Client
	model     string
	temp      float64
	toolIDSeq atomic.Int64
}

// NewOllamaClient creates a new Ollama client, verifies the server is
// reachable, and ensures the model is available locally (pulling if needed).
func NewOllamaClient(ctx context.Context, model string, temperature float64, ollamaHost string) (*OllamaClient, error) {
	if ollamaHost != "" {
		if err := os.Setenv("OLLAMA_HOST", ollamaHost); err != nil {
			return nil, fmt.Errorf("failed to set OLLAMA_HOST: %w", err)
		}
	}

	client, err := api.ClientFromEnvironment()
	if err != nil {
		return nil, fmt.Errorf("failed to create ollama client: %w", err)
	}

	oc := &OllamaClient{
		client: client,
		model:  model,
		temp:   temperature,
	}

	if err := oc.ensureModel(ctx); err != nil {
		return nil, err
	}

	return oc, nil
}

// ensureModel verifies the Ollama server is reachable and the model is
// available locally, pulling it automatically if needed.
func (oc *OllamaClient) ensureModel(ctx context.Context) error {
	logger := logging.Get()

	// Verify Ollama server is reachable
	if err := oc.client.Heartbeat(ctx); err != nil {
		return fmt.Errorf("ollama server not reachable (is it running?): %w", err)
	}
	logger.Debug().Msg("Ollama server is reachable")

	// Check if model already exists
	_, err := oc.client.Show(ctx, &api.ShowRequest{Model: oc.model})
	if err == nil {
		logger.Debug().Str("model", oc.model).Msg("Ollama model already available")
		return nil
	}

	// Model not found — pull it
	logger.Info().Str("model", oc.model).Msg("Model not found locally, pulling...")
	if err := oc.client.Pull(ctx, &api.PullRequest{Model: oc.model}, func(resp api.ProgressResponse) error {
		if resp.Total > 0 {
			pct := float64(resp.Completed) / float64(resp.Total) * 100
			logger.Info().Str("status", resp.Status).Float64("percent", pct).Msg("Pulling model")
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to pull model %q: %w", oc.model, err)
	}

	logger.Info().Str("model", oc.model).Msg("Model pull complete")
	return nil
}

// Converse calls the Ollama Chat API
func (oc *OllamaClient) Converse(
	ctx context.Context,
	system string,
	messages []Message,
	tools []ToolDef,
	maxTokens int,
) (*Message, error) {
	// Build Ollama messages starting with system prompt
	ollamaMessages := make([]api.Message, 0, len(messages)+1)
	ollamaMessages = append(ollamaMessages, api.Message{
		Role:    "system",
		Content: system,
	})

	// Convert internal messages to Ollama format
	for _, msg := range messages {
		ollamaMessages = append(ollamaMessages, messagesToOllama(msg)...)
	}

	// Convert tool definitions
	ollamaTools := toolsToOllama(tools)

	// Build chat request
	stream := false
	req := &api.ChatRequest{
		Model:    oc.model,
		Messages: ollamaMessages,
		Stream:   &stream,
		Tools:    ollamaTools,
		Options: map[string]any{
			"temperature": oc.temp,
			"num_predict": maxTokens,
		},
	}

	// Call Chat API (streaming with single callback for non-streaming)
	var finalResp api.ChatResponse
	err := oc.client.Chat(ctx, req, func(resp api.ChatResponse) error {
		finalResp = resp
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("ollama chat failed: %w", err)
	}

	// Convert response to internal format
	return oc.ollamaResponseToMessage(&finalResp), nil
}

// messagesToOllama converts an internal Message to one or more Ollama Messages.
// tool_result content blocks are converted to individual user messages.
func messagesToOllama(msg Message) []api.Message {
	var result []api.Message

	// Collect text content and tool_use blocks
	var textContent string
	var toolCalls []api.ToolCall

	for _, cb := range msg.Content {
		switch cb.Type {
		case "text":
			textContent += cb.Text
		case "tool_use":
			args := api.NewToolCallFunctionArguments()
			for k, v := range cb.Input {
				args.Set(k, v)
			}
			toolCalls = append(toolCalls, api.ToolCall{
				ID: cb.ToolUseID,
				Function: api.ToolCallFunction{
					Name:      cb.ToolName,
					Arguments: args,
				},
			})
		case "tool_result":
			// Tool results become user messages with the tool role
			content := cb.Content
			if cb.IsError {
				content = "Error: " + content
			}
			result = append(result, api.Message{
				Role:       "tool",
				Content:    content,
				ToolCallID: cb.ToolUseID,
			})
		}
	}

	// If we have text or tool calls, add an assistant/user message
	if textContent != "" || len(toolCalls) > 0 {
		ollamaMsg := api.Message{
			Role:      msg.Role,
			Content:   textContent,
			ToolCalls: toolCalls,
		}
		// Prepend before any tool results
		result = append([]api.Message{ollamaMsg}, result...)
	}

	return result
}

// toolsToOllama converts internal tool definitions to Ollama format
func toolsToOllama(toolDefs []ToolDef) api.Tools {
	tools := make(api.Tools, len(toolDefs))

	for i, td := range toolDefs {
		props := api.NewToolPropertiesMap()

		// Extract properties from InputSchema
		if propsRaw, ok := td.InputSchema["properties"].(map[string]any); ok {
			for name, propRaw := range propsRaw {
				propMap, ok := propRaw.(map[string]any)
				if !ok {
					continue
				}
				tp := api.ToolProperty{}
				if t, ok := propMap["type"].(string); ok {
					tp.Type = api.PropertyType{t}
				}
				if d, ok := propMap["description"].(string); ok {
					tp.Description = d
				}
				props.Set(name, tp)
			}
		}

		// Extract required fields
		var required []string
		if reqRaw, ok := td.InputSchema["required"].([]string); ok {
			required = reqRaw
		} else if reqRaw, ok := td.InputSchema["required"].([]any); ok {
			for _, r := range reqRaw {
				if s, ok := r.(string); ok {
					required = append(required, s)
				}
			}
		}

		tools[i] = api.Tool{
			Type: "function",
			Function: api.ToolFunction{
				Name:        td.Name,
				Description: td.Description,
				Parameters: api.ToolFunctionParameters{
					Type:       "object",
					Required:   required,
					Properties: props,
				},
			},
		}
	}

	return tools
}

// ollamaResponseToMessage converts an Ollama ChatResponse to an internal Message
func (oc *OllamaClient) ollamaResponseToMessage(resp *api.ChatResponse) *Message {
	var content []ContentBlock

	// Add text content if present
	if resp.Message.Content != "" {
		content = append(content, ContentBlock{
			Type: "text",
			Text: resp.Message.Content,
		})
	}

	// Add tool calls if present
	for _, tc := range resp.Message.ToolCalls {
		toolID := tc.ID
		if toolID == "" {
			toolID = fmt.Sprintf("ollama-%d", oc.toolIDSeq.Add(1))
		}

		content = append(content, ContentBlock{
			Type:      "tool_use",
			ToolUseID: toolID,
			ToolName:  tc.Function.Name,
			Input:     tc.Function.Arguments.ToMap(),
		})
	}

	return &Message{
		Role:    "assistant",
		Content: content,
	}
}

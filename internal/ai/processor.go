package ai

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Converser interface for mocking Bedrock in tests
type Converser interface {
	Converse(
		ctx context.Context,
		system string,
		messages []Message,
		tools []ToolDef,
		maxTokens int,
	) (*Message, error)
}

// Processor runs the agentic AI loop
type Processor struct {
	converser    Converser
	systemPrompt string
	tools        []ToolDef
	maxIter      int
	maxTokens    int
	executeTool  func(ctx context.Context, toolName string, params map[string]any) (string, error)
}

// ProcessorConfig holds configuration for creating a Processor
type ProcessorConfig struct {
	Converser    Converser
	SystemPrompt string
	Tools        []ToolDef
	MaxTokens    int
	MaxIterations int
	ExecuteTool  func(ctx context.Context, toolName string, params map[string]any) (string, error)
}

// NewProcessor creates a new AI processor
func NewProcessor(cfg ProcessorConfig) *Processor {
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 10
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 4096
	}
	return &Processor{
		converser:    cfg.Converser,
		systemPrompt: cfg.SystemPrompt,
		tools:        cfg.Tools,
		maxIter:      cfg.MaxIterations,
		maxTokens:    cfg.MaxTokens,
		executeTool:  cfg.ExecuteTool,
	}
}

// maxProcessDuration is the hard timeout for an entire Process() call.
const maxProcessDuration = 5 * time.Minute

// maxToolResultBytes caps individual tool result content passed back to the LLM.
const maxToolResultBytes = 32 * 1024 // 32KB

// wrapUserInput wraps user input in XML delimiters to prevent prompt injection.
func wrapUserInput(text string) string {
	return "<user_input>\n" + text + "\n</user_input>"
}

// sanitizeToolOutput wraps tool output in delimiters and truncates if too large.
func sanitizeToolOutput(result string) string {
	if len(result) > maxToolResultBytes {
		result = result[:maxToolResultBytes] + "\n[output truncated]"
	}
	// Defang potential injection patterns in tool output
	result = strings.ReplaceAll(result, "<user_input>", "&lt;user_input&gt;")
	result = strings.ReplaceAll(result, "</user_input>", "&lt;/user_input&gt;")
	return "<tool_output>\n" + result + "\n</tool_output>"
}

// Process runs the agentic loop to process a user message
func (p *Processor) Process(
	ctx context.Context,
	userMessage string,
	history []Message,
) (string, []Message, error) {
	// Enforce a hard deadline on the entire processing loop
	ctx, cancel := context.WithTimeout(ctx, maxProcessDuration)
	defer cancel()

	// Create a working copy of history to avoid mutating the input
	workingHistory := slices.Clone(history)

	// Append user message wrapped in delimiters to prevent prompt injection
	workingHistory = append(workingHistory, Message{
		Role:    "user",
		Content: []ContentBlock{{Type: "text", Text: wrapUserInput(userMessage)}},
	})

	// Track consecutive tool errors for circuit breaker
	consecutiveErrors := 0

	// Run agentic loop
	for iteration := 0; iteration < p.maxIter; iteration++ {
		// Call Bedrock Converse
		response, err := p.converser.Converse(
			ctx,
			p.systemPrompt,
			workingHistory,
			p.tools,
			p.maxTokens,
		)
		if err != nil {
			return "", workingHistory, fmt.Errorf("converser failed: %w", err)
		}

		// Append assistant response to history
		workingHistory = append(workingHistory, *response)

		// Check if response contains tool calls
		toolCalls := extractToolCalls(response.Content)
		if len(toolCalls) == 0 {
			// No tool calls, extract final text and return
			finalText := extractText(response.Content)
			return finalText, workingHistory, nil
		}

		// Execute tools and collect results
		toolResults := make([]ContentBlock, 0, len(toolCalls))
		iterationHadError := false
		for _, toolCall := range toolCalls {
			result, err := p.executeTool(ctx, toolCall.ToolName, toolCall.Input)
			if err != nil {
				iterationHadError = true
				result = fmt.Sprintf("Error: %v", err)
				toolResults = append(toolResults, ContentBlock{
					Type:      "tool_result",
					ToolUseID: toolCall.ToolUseID,
					Content:   sanitizeToolOutput(result),
					IsError:   true,
				})
			} else {
				toolResults = append(toolResults, ContentBlock{
					Type:      "tool_result",
					ToolUseID: toolCall.ToolUseID,
					Content:   sanitizeToolOutput(result),
					IsError:   false,
				})
			}
		}

		// Circuit breaker: abort if too many consecutive tool errors
		if iterationHadError {
			consecutiveErrors++
		} else {
			consecutiveErrors = 0
		}
		if consecutiveErrors >= 3 {
			return "I've encountered repeated errors and stopped to avoid further issues. Please review and try again.", workingHistory, nil
		}

		// Append user message with tool results
		workingHistory = append(workingHistory, Message{
			Role:    "user",
			Content: toolResults,
		})
	}

	// Max iterations reached
	return "I've reached the maximum number of steps.", workingHistory, nil
}

// extractToolCalls extracts tool use blocks from content
func extractToolCalls(content []ContentBlock) []struct {
	ToolUseID string
	ToolName  string
	Input     map[string]any
} {
	var toolCalls []struct {
		ToolUseID string
		ToolName  string
		Input     map[string]any
	}

	for _, block := range content {
		if block.Type == "tool_use" {
			// Ensure Input is non-nil
			input := block.Input
			if input == nil {
				input = make(map[string]any)
			}
			toolCalls = append(toolCalls, struct {
				ToolUseID string
				ToolName  string
				Input     map[string]any
			}{
				ToolUseID: block.ToolUseID,
				ToolName:  block.ToolName,
				Input:     input,
			})
		}
	}

	return toolCalls
}

// extractText extracts all text content from content blocks
func extractText(content []ContentBlock) string {
	var text string
	for _, block := range content {
		if block.Type == "text" {
			text += block.Text
		}
	}
	return text
}


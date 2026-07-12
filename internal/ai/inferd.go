package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	inferd "github.com/3rg0n/inferd/clients/go"
	"github.com/3rg0n/remoteclaw/internal/logging"
)

// InferdClient wraps the inferd inference daemon client
type InferdClient struct {
	socketOverride string
	temp           float64
	toolIDSeq      atomic.Int64
}

// NewInferdClient creates a new inferd client and verifies connectivity to the daemon.
// If socketOverride is empty, uses the platform-default socket (UDS on unix, named pipe on windows).
// Returns an error if the daemon is not reachable.
func NewInferdClient(ctx context.Context, socketOverride string, temperature float64) (*InferdClient, error) {
	ic := &InferdClient{
		socketOverride: socketOverride,
		temp:           temperature,
	}

	// Verify daemon is reachable with a probe connection.
	client, err := ic.dial(ctx)
	if err != nil {
		return nil, fmt.Errorf("inferd daemon not reachable (is it running?): %w", err)
	}
	_ = client.Close()
	return ic, nil
}

// dialBusyRetries bounds how long dial retries a "pipe busy" condition on
// Windows. The daemon serves one pipe instance at a time and briefly has no
// free instance while it rebinds between clients; the upstream client's own
// retry is case-sensitive and misses the actual "All pipe instances are busy."
// message, so we retry here defensively. See inferd issue on the case mismatch.
const (
	dialBusyRetries = 20
	dialBusyBackoff = 25 * time.Millisecond
)

// dial opens a connection to the daemon, honoring the socket override and
// retrying transient "pipe busy" errors (Windows named-pipe rebind window).
func (ic *InferdClient) dial(ctx context.Context) (*inferd.Client, error) {
	var lastErr error
	for attempt := 0; attempt < dialBusyRetries; attempt++ {
		var client *inferd.Client
		var err error
		if ic.socketOverride == "" {
			client, err = inferd.DialInfer(ctx)
		} else {
			client, err = dialInferdOverride(ctx, ic.socketOverride)
		}
		if err == nil {
			return client, nil
		}
		lastErr = err
		if !isPipeBusyErr(err) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(dialBusyBackoff):
		}
	}
	return nil, lastErr
}

// isPipeBusyErr reports whether err is the transient Windows "all pipe
// instances are busy" condition, matched case-insensitively.
func isPipeBusyErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "all pipe instances are busy")
}

// Converse calls the inferd GenerateV2 API with streaming frame accumulation
func (ic *InferdClient) Converse(
	ctx context.Context,
	system string,
	messages []Message,
	tools []ToolDef,
	maxTokens int,
) (*Message, error) {
	// Dial a fresh client for this call (one connection per Converse).
	client, err := ic.dial(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to dial inferd daemon: %w", err)
	}
	defer func() {
		_ = client.Close()
	}()

	// Build inferd message list: system message first (if non-empty), then convert RemoteClaw messages
	inferdMessages := make([]inferd.MessageV2, 0, len(messages)+1)

	if system != "" {
		inferdMessages = append(inferdMessages, inferd.MessageV2{
			Role: inferd.RoleSystem,
			Content: []inferd.ContentBlock{
				{
					Type: inferd.ContentText,
					Text: system,
				},
			},
		})
	}

	// Convert RemoteClaw messages to inferd MessageV2
	for _, msg := range messages {
		inferdMsg, err := rcMessageToInferd(msg)
		if err != nil {
			return nil, fmt.Errorf("failed to convert message: %w", err)
		}
		// Only append non-empty messages
		if len(inferdMsg.Content) > 0 {
			inferdMessages = append(inferdMessages, inferdMsg)
		}
	}

	// Build inferd tool list
	inferdTools := make([]inferd.ToolV2, len(tools))
	for i, td := range tools {
		schema, err := json.Marshal(td.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal tool schema for %q: %w", td.Name, err)
		}
		inferdTools[i] = inferd.ToolV2{
			Name:        td.Name,
			Description: td.Description,
			InputSchema: schema,
		}
	}

	// Build GenerateV2 request
	req := inferd.RequestV2{
		Messages: inferdMessages,
		Tools:    inferdTools,
	}

	// Set temperature
	temp := ic.temp
	req.Temperature = &temp

	// Set maxTokens if valid
	if maxTokens > 0 && maxTokens <= 1_000_000 {
		mt := uint32(maxTokens) //nolint:gosec // G115: maxTokens is range-checked (0 < n <= 1000000)
		req.MaxTokens = &mt
	}

	// Stream generation and accumulate frames
	frames, err := client.GenerateV2(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("inferd generation failed: %w", err)
	}

	var textBuilder strings.Builder
	var contentBlocks []ContentBlock

streamLoop:
	for frame := range frames {
		switch frame.Type {
		case inferd.ResponseV2Frame:
			if frame.Block == nil {
				continue
			}
			switch frame.Block.Type {
			case inferd.BlockText:
				// Accumulate text delta
				textBuilder.WriteString(frame.Block.Delta)
			case inferd.BlockThinking:
				// Ignore thinking blocks; do not leak reasoning
			case inferd.BlockToolUse:
				// Accumulate tool use block
				toolCallID := frame.Block.ToolCallID
				if toolCallID == "" {
					toolCallID = fmt.Sprintf("inferd-%d", ic.toolIDSeq.Add(1))
				}

				// Unmarshal tool input
				var input map[string]any
				if frame.Block.Input != nil {
					if err := json.Unmarshal(frame.Block.Input, &input); err != nil {
						logger := logging.Get()
						logger.Warn().Err(err).Str("tool", frame.Block.Name).
							Msg("Failed to unmarshal inferd tool input, using empty map")
						input = make(map[string]any)
					}
				} else {
					input = make(map[string]any)
				}

				contentBlocks = append(contentBlocks, ContentBlock{
					Type:      "tool_use",
					ToolUseID: toolCallID,
					ToolName:  frame.Block.Name,
					Input:     input,
				})
			}
		case inferd.ResponseV2Error:
			return nil, fmt.Errorf("inferd error [%s]: %s", frame.Code, frame.Message)
		case inferd.ResponseV2Done:
			// Terminal frame — stop reading the stream.
			break streamLoop
		}
	}

	// Assemble final message: text first (if non-empty), then tool calls
	content := []ContentBlock{}

	if text := textBuilder.String(); text != "" {
		content = append(content, ContentBlock{
			Type: "text",
			Text: text,
		})
	}

	content = append(content, contentBlocks...)

	// If no content was accumulated and context wasn't cancelled, error
	if len(content) == 0 && ctx.Err() == nil {
		return nil, fmt.Errorf("inferd stream ended without a terminal frame")
	}

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	return &Message{
		Role:    "assistant",
		Content: content,
	}, nil
}

// rcMessageToInferd converts a RemoteClaw Message to an inferd MessageV2
func rcMessageToInferd(msg Message) (inferd.MessageV2, error) {
	// Map Role: "user" -> RoleUser, "assistant" -> RoleAssistant, default -> RoleUser
	role := inferd.RoleUser
	if msg.Role == "assistant" {
		role = inferd.RoleAssistant
	}

	// Convert content blocks
	inferdContent := make([]inferd.ContentBlock, 0, len(msg.Content))

	for _, cb := range msg.Content {
		switch cb.Type {
		case "text":
			inferdContent = append(inferdContent, inferd.ContentBlock{
				Type: inferd.ContentText,
				Text: cb.Text,
			})

		case "tool_use":
			// Marshal input to JSON
			inputJSON, err := json.Marshal(cb.Input)
			if err != nil {
				return inferd.MessageV2{}, fmt.Errorf("failed to marshal tool input: %w", err)
			}
			inferdContent = append(inferdContent, inferd.ContentBlock{
				Type:       inferd.ContentToolUse,
				ToolCallID: cb.ToolUseID,
				Name:       cb.ToolName,
				Input:      inputJSON,
			})

		case "tool_result":
			// Tool result as nested content block
			resultText := cb.Content
			if cb.IsError {
				resultText = "Error: " + resultText
			}
			inferdContent = append(inferdContent, inferd.ContentBlock{
				Type:       inferd.ContentToolResult,
				ToolCallID: cb.ToolUseID,
				Content: []inferd.ContentBlock{
					{
						Type: inferd.ContentText,
						Text: resultText,
					},
				},
			})
		}
	}

	return inferd.MessageV2{
		Role:    role,
		Content: inferdContent,
	}, nil
}

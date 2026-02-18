package agent

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ecopelan/wcca/internal/ai"
	"github.com/ecopelan/wcca/internal/config"
	"github.com/ecopelan/wcca/internal/connect"
	"github.com/ecopelan/wcca/internal/executor"
	"github.com/ecopelan/wcca/internal/logging"
	"github.com/ecopelan/wcca/internal/security"
	"github.com/rs/zerolog"
)

// contextKey is used for passing values through context in the agent.
type contextKey string

const spaceIDKey contextKey = "spaceID"

// Agent is the main WCC orchestrator
type Agent struct {
	cfg            *config.Config
	mode           connect.Mode
	exec           *executor.Executor
	processor      *ai.Processor
	logger         zerolog.Logger
	health         *http.Server
	audit          *logging.AuditLogger
	conversations  *ConversationManager
	rateLimiter    *security.RateLimiter
	challengeStore *security.ChallengeStore
	mu             sync.RWMutex
	lastMsg        time.Time
	startTime      time.Time
}

// New creates a new Agent with the given configuration
func New(cfg *config.Config) (*Agent, error) {
	logger := logging.Get()

	// Create executor with config timeouts
	exec := executor.New(cfg.Execution.DefaultTimeout, cfg.Execution.MaxTimeout, cfg.Execution.Shell)

	// Wire dangerous command checker into executor
	if cfg.Security.DangerousCommands {
		exec.SetDangerousChecker(security.NewDangerousChecker())
		logger.Info().Msg("Dangerous command checker enabled")
	}

	// Create audit logger
	var audit *logging.AuditLogger
	if cfg.Security.AuditLog != "" {
		var auditErr error
		audit, auditErr = logging.NewAuditLogger(cfg.Security.AuditLog)
		if auditErr != nil {
			return nil, fmt.Errorf("failed to create audit logger: %w", auditErr)
		}
		logger.Info().Str("path", cfg.Security.AuditLog).Msg("Audit logging enabled")
	}

	// Create conversation manager (20 messages of history per space)
	conversations := NewConversationManager(20)

	// Create rate limiter
	var rateLimiter *security.RateLimiter
	if cfg.Security.RateLimitPerMin > 0 {
		rateLimiter = security.NewRateLimiter(cfg.Security.RateLimitPerMin, 3)
		logger.Info().Int("perMin", cfg.Security.RateLimitPerMin).Msg("Rate limiter enabled")
	}

	// Create challenge store for destructive command confirmation
	challengeStore := security.NewChallengeStore(cfg.Security.Challenge)
	if challengeStore.Enabled() {
		logger.Info().Msg("Challenge-response confirmation enabled for destructive commands")
	}

	// Create AI client based on resolved provider
	provider := cfg.ResolveAIProvider()
	var converser ai.Converser
	var err error

	switch provider {
	case "bedrock":
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		model := cfg.AI.Model
		if model == "phi4-mini" || model == "phi4" {
			model = "us.anthropic.claude-sonnet-4-20250514-v1:0"
		}
		converser, err = ai.NewBedrockClient(ctx, cfg.AWS.Region, model, cfg.AI.Temperature)
	case "local":
		// Longer timeout: first run may need to pull the model
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		converser, err = ai.NewOllamaClient(ctx, cfg.AI.Model, cfg.AI.Temperature, cfg.AI.OllamaHost)
	default:
		err = fmt.Errorf("unknown AI provider: %s", provider)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create %s AI client: %w", provider, err)
	}

	logger.Info().Str("provider", provider).Str("model", cfg.AI.Model).Msg("AI provider initialized")

	// Build system prompt
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	username := getUsername()
	osName := runtime.GOOS
	arch := runtime.GOARCH

	systemPrompt := ai.BuildSystemPrompt(osName, arch, hostname, username)

	// Create tool executor bridge
	toolExecutor := func(ctx context.Context, toolName string, params map[string]any) (string, error) {
		result, err := exec.Execute(ctx, toolName, params)
		if err != nil {
			return "", err
		}

		// When a command is blocked and challenge-response is enabled,
		// store the pending challenge and ask the user to confirm.
		if toolName == "execute_command" && challengeStore.Enabled() &&
			result.ExitCode == 1 && strings.HasPrefix(result.Error, "Command blocked:") {
			if cmd, ok := params["command"].(string); ok {
				if spaceID, ok := ctx.Value(spaceIDKey).(string); ok {
					reason := strings.TrimPrefix(result.Error, "Command blocked: ")
					challengeStore.SetPending(spaceID, cmd, reason)
					return fmt.Sprintf(
						"Command blocked: %s\n\nThis command requires confirmation. "+
							"The user must reply with the exact challenge code to proceed. "+
							"The confirmation expires in 2 minutes.",
						reason,
					), nil
				}
			}
		}

		// Combine output and error into a single string for the AI
		output := result.Output
		if result.Error != "" {
			output += "\nError: " + result.Error
		}
		return output, nil
	}

	// Create AI processor
	processor := ai.NewProcessor(ai.ProcessorConfig{
		Converser:     converser,
		SystemPrompt:  systemPrompt,
		Tools:         ai.AllTools(),
		MaxTokens:     cfg.AI.MaxTokens,
		MaxIterations: cfg.AI.MaxIterations,
		ExecuteTool:   toolExecutor,
	})

	// Create the appropriate connection mode
	var mode connect.Mode
	switch cfg.Mode {
	case "wmcp":
		mode = connect.NewWMCPMode(cfg.WMCP.Endpoint, cfg.WMCP.Token, logger)
	default: // native
		mode = connect.NewNativeMode(cfg.Webex.BotToken, cfg.Webex.AllowedEmails, logger)
	}

	agent := &Agent{
		cfg:            cfg,
		mode:           mode,
		exec:           exec,
		processor:      processor,
		logger:         logger,
		audit:          audit,
		conversations:  conversations,
		rateLimiter:    rateLimiter,
		challengeStore: challengeStore,
		startTime:      time.Now(),
	}

	return agent, nil
}

// Run starts the agent and runs the main event loop
func (a *Agent) Run(ctx context.Context) error {
	// Register message handler
	a.mode.OnMessage(a.messageHandler)

	// Connect to Webex or WMCP backend
	if err := a.mode.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	a.logger.Info().Msg("Agent connected to message service")

	// Start health check server if enabled
	if a.cfg.Health.Enabled {
		if err := a.startHealthServer(a.cfg.Health.Addr); err != nil {
			a.logger.Error().Err(err).Msg("Failed to start health server")
			// Don't exit, health server is optional
		} else {
			a.logger.Info().Str("addr", a.cfg.Health.Addr).Msg("Health server started")
		}
	}

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Create a context that we can cancel
	shutdownCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Wait for signal
	go func() {
		<-sigChan
		a.logger.Info().Msg("Shutdown signal received")
		cancel()
	}()

	// Block until context is cancelled
	<-shutdownCtx.Done()

	// Graceful shutdown
	a.logger.Info().Msg("Starting graceful shutdown")

	// Close connection
	if err := a.mode.Close(); err != nil {
		a.logger.Error().Err(err).Msg("Error closing connection")
	}

	// Stop health server
	if a.health != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := a.health.Shutdown(shutdownCtx); err != nil {
			a.logger.Error().Err(err).Msg("Error shutting down health server")
		}
	}

	// Close audit logger
	if a.audit != nil {
		if err := a.audit.Close(); err != nil {
			a.logger.Error().Err(err).Msg("Error closing audit logger")
		}
	}

	a.logger.Info().Msg("Agent shutdown complete")
	return nil
}

// messageHandler processes incoming messages from Webex/WMCP
func (a *Agent) messageHandler(ctx context.Context, msg connect.IncomingMessage) {
	start := time.Now()
	a.mu.Lock()
	a.lastMsg = start
	a.mu.Unlock()

	a.logger.Debug().
		Str("email", msg.Email).
		Str("text", msg.Text).
		Msg("Message received")

	// Check rate limit before processing
	if a.rateLimiter != nil && !a.rateLimiter.Allow(msg.SpaceID) {
		a.logger.Warn().Str("spaceID", msg.SpaceID).Str("email", msg.Email).Msg("Rate limited")
		_ = a.mode.SendMessage(ctx, msg.SpaceID, "Rate limited. Please wait before sending more requests.")
		return
	}

	// Check if this message is a challenge-response confirmation
	if a.challengeStore != nil && a.challengeStore.Enabled() {
		if pc, ok := a.challengeStore.CheckResponse(msg.SpaceID, msg.Text); ok {
			a.handleChallengeConfirmation(ctx, msg, pc, start)
			return
		}
	}

	// Get conversation history for this space
	conversationKey := msg.SpaceID
	history := a.conversations.GetHistory(conversationKey)

	// Set spaceID in context for challenge-response tracking in tool executor
	processCtx := context.WithValue(ctx, spaceIDKey, msg.SpaceID)

	// Process the message with the AI processor
	response, updatedHistory, err := a.processor.Process(processCtx, msg.Text, history)
	var errMsg string
	if err != nil {
		a.logger.Error().Err(err).Msg("Failed to process message")
		response = fmt.Sprintf("Error processing request: %v", err)
		errMsg = err.Error()
	}

	// Update conversation history
	a.conversations.UpdateHistory(conversationKey, updatedHistory)

	// Extract tool call names for audit
	var toolCalls []string
	for _, m := range updatedHistory {
		for _, b := range m.Content {
			if b.Type == "tool_use" {
				toolCalls = append(toolCalls, b.ToolName)
			}
		}
	}

	// Log audit entry
	if a.audit != nil {
		a.audit.Log(logging.AuditEntry{
			Timestamp:  start,
			Email:      msg.Email,
			SpaceID:    msg.SpaceID,
			RawMessage: msg.Text,
			ToolCalls:  toolCalls,
			Response:   response,
			Duration:   time.Since(start),
			Error:      errMsg,
		})
	}

	// Format response for Webex
	formattedResponse := connect.FormatResponse(response)

	// Send response back
	if err := a.mode.SendMessage(ctx, msg.SpaceID, formattedResponse); err != nil {
		a.logger.Error().Err(err).Msg("Failed to send message")
	}
}

// handleChallengeConfirmation executes a confirmed dangerous command after challenge-response.
func (a *Agent) handleChallengeConfirmation(ctx context.Context, msg connect.IncomingMessage, pc *security.PendingChallenge, start time.Time) {
	a.logger.Info().
		Str("email", msg.Email).
		Str("spaceID", msg.SpaceID).
		Str("command", pc.Command).
		Msg("Challenge confirmed, executing previously blocked command")

	result, err := a.exec.ForceExecuteCommand(ctx, pc.Command)
	var response string
	var errMsg string
	if err != nil {
		response = fmt.Sprintf("Error executing confirmed command: %v", err)
		errMsg = err.Error()
	} else {
		response = result.Output
		if result.Error != "" {
			response += "\nError: " + result.Error
		}
		if response == "" {
			response = "Command executed successfully (no output)."
		}
	}

	// Log audit entry for the confirmed execution
	if a.audit != nil {
		a.audit.Log(logging.AuditEntry{
			Timestamp:  start,
			Email:      msg.Email,
			SpaceID:    msg.SpaceID,
			RawMessage: fmt.Sprintf("[challenge confirmed] %s", pc.Command),
			ToolCalls:  []string{"execute_command"},
			Response:   response,
			Duration:   time.Since(start),
			Error:      errMsg,
		})
	}

	formattedResponse := connect.FormatResponse(response)
	if err := a.mode.SendMessage(ctx, msg.SpaceID, formattedResponse); err != nil {
		a.logger.Error().Err(err).Msg("Failed to send challenge confirmation response")
	}
}

// getUsername retrieves the current username
func getUsername() string {
	// Try os/user first for cross-platform support
	if currentUser, err := user.Current(); err == nil {
		return currentUser.Username
	}

	// Fall back to environment variables
	if username := os.Getenv("USER"); username != "" {
		return username
	}
	if username := os.Getenv("USERNAME"); username != "" {
		return username
	}

	return "unknown"
}

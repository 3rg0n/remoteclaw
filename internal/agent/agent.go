package agent

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/ecopelan/wcc/internal/ai"
	"github.com/ecopelan/wcc/internal/config"
	"github.com/ecopelan/wcc/internal/connect"
	"github.com/ecopelan/wcc/internal/executor"
	"github.com/ecopelan/wcc/internal/logging"
	"github.com/rs/zerolog"
)

// Agent is the main WCC orchestrator
type Agent struct {
	cfg       *config.Config
	mode      connect.Mode
	exec      *executor.Executor
	processor *ai.Processor
	logger    zerolog.Logger
	health    *http.Server
	mu        sync.RWMutex
	lastMsg   time.Time
	startTime time.Time
}

// New creates a new Agent with the given configuration
func New(cfg *config.Config) (*Agent, error) {
	logger := logging.Get()

	// Create executor with config timeouts
	exec := executor.New(cfg.Execution.DefaultTimeout, cfg.Execution.MaxTimeout, cfg.Execution.Shell)

	// Create AI client based on resolved provider
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	provider := cfg.ResolveAIProvider()
	var converser ai.Converser
	var err error

	switch provider {
	case "bedrock":
		model := cfg.AI.Model
		if model == "phi4-mini" || model == "phi4" {
			model = "us.anthropic.claude-sonnet-4-20250514-v1:0"
		}
		converser, err = ai.NewBedrockClient(ctx, cfg.AWS.Region, model, cfg.AI.Temperature)
	case "local":
		converser, err = ai.NewOllamaClient(cfg.AI.Model, cfg.AI.Temperature, cfg.AI.OllamaHost)
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
		cfg:       cfg,
		mode:      mode,
		exec:      exec,
		processor: processor,
		logger:    logger,
		startTime: time.Now(),
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

	a.logger.Info().Msg("Agent shutdown complete")
	return nil
}

// messageHandler processes incoming messages from Webex/WMCP
func (a *Agent) messageHandler(ctx context.Context, msg connect.IncomingMessage) {
	a.mu.Lock()
	a.lastMsg = time.Now()
	a.mu.Unlock()

	a.logger.Debug().
		Str("email", msg.Email).
		Str("text", msg.Text).
		Msg("Message received")

	// Process the message with the AI processor
	response, _, err := a.processor.Process(ctx, msg.Text, nil)
	if err != nil {
		a.logger.Error().Err(err).Msg("Failed to process message")
		response = fmt.Sprintf("Error processing request: %v", err)
	}

	// Format response for Webex
	formattedResponse := connect.FormatResponse(response)

	// Send response back
	if err := a.mode.SendMessage(ctx, msg.SpaceID, formattedResponse); err != nil {
		a.logger.Error().Err(err).Msg("Failed to send message")
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

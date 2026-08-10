package agent

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/3rg0n/remoteclaw/internal/ai"
	"github.com/3rg0n/remoteclaw/internal/config"
	"github.com/3rg0n/remoteclaw/internal/connect"
	"github.com/3rg0n/remoteclaw/internal/executor"
	"github.com/3rg0n/remoteclaw/internal/logging"
	"github.com/3rg0n/remoteclaw/internal/security"
	"github.com/rs/zerolog"
)

// contextKey is used for passing values through context in the agent.
type contextKey string

const spaceIDKey contextKey = "spaceID"

// Agent is the main RemoteClaw orchestrator
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
	allowlist      *connect.Allowlist
	mu             sync.RWMutex
	lastMsg        time.Time
	startTime      time.Time
	connected      bool
}

// New creates a new Agent with the given configuration
func New(cfg *config.Config) (*Agent, error) {
	logger := logging.Get()

	// Security: warn if running as root/Administrator — the agent should
	// run under a dedicated low-privilege user account.
	if currentUser, err := user.Current(); err == nil {
		if currentUser.Uid == "0" || currentUser.Username == "root" {
			logger.Warn().Msg("Running as root is strongly discouraged — use a dedicated low-privilege user")
		}
	}

	// Enforce mandatory audit logging when dangerous commands or challenge-response are enabled
	if cfg.Security.AuditLog == "" && (cfg.Security.DangerousCommands || cfg.Security.Challenge != "") {
		logger.Warn().Msg("Audit logging is disabled but security features are enabled — strongly recommend setting security.audit_log")
	}

	// Create executor with config timeouts
	exec := executor.New(cfg.Execution.DefaultTimeout, cfg.Execution.MaxTimeout, cfg.Execution.Shell)

	// Wire the config/secret lockdown guard. It owns the path-based denial for
	// the file tools, and canonicalizes the protected paths the command policy
	// below matches against. The authoritative protection is the OS
	// (config/secrets owned by root, unreadable by the low-privilege service
	// account — set up by the installer); this guard is in-process
	// defense-in-depth. Disabled only when the operator opts out to "wide open".
	guard := executor.NewGuard(cfg.Security.Lockdown, lockdownPaths(cfg))
	exec.SetGuard(guard)
	if guard.Enabled() {
		logger.Info().Msg("Config/secret lockdown enabled — agent tools cannot read or modify config/secrets")
	} else {
		logger.Warn().Msg("Config/secret lockdown DISABLED (security.lockdown=false) — agent tools may access config and secrets")
	}

	// One deny-list engine for execute_command, carrying both rule groups: the
	// destructive-command rules (confirmable via challenge-response) and the
	// config/secret-read rules (hard denials). See ADR 0006.
	exec.SetCommandPolicy(security.NewCommandPolicy(security.CommandPolicyOptions{
		BlockDangerous:   cfg.Security.DangerousCommands,
		BlockSecretReads: guard.Enabled(),
		ProtectedPaths:   guard.ProtectedPaths(),
	}))
	if cfg.Security.DangerousCommands {
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

	// Build the AI processor unless running in passthrough mode, which
	// executes inbound messages directly and needs no local inference.
	var processor *ai.Processor
	if cfg.PassthroughMode() {
		logger.Warn().Msg("Passthrough mode enabled — inbound messages run directly as shell commands (all guardrails remain active)")
	} else {
		provider := cfg.ResolveAIProvider()
		converser, err := newConverser(provider, cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create %s AI client: %w", provider, err)
		}
		logger.Info().Str("provider", provider).Str("model", cfg.AI.Model).Msg("AI provider initialized")

		// Build system prompt
		hostname, err := os.Hostname()
		if err != nil {
			hostname = "unknown"
		}
		systemPrompt := ai.BuildSystemPrompt(runtime.GOOS, runtime.GOARCH, hostname, getUsername())

		// Bridge the processor's tool calls through the shared guarded path.
		toolExecutor := func(ctx context.Context, toolName string, params map[string]any) (string, error) {
			return executeToolGuarded(ctx, exec, challengeStore, toolName, params)
		}

		processor = ai.NewProcessor(ai.ProcessorConfig{
			Converser:     converser,
			SystemPrompt:  systemPrompt,
			Tools:         ai.AllTools(),
			MaxTokens:     cfg.AI.MaxTokens,
			MaxIterations: cfg.AI.MaxIterations,
			ExecuteTool:   toolExecutor,
		})
	}

	// Create the appropriate connection mode. Modes carry no authorization
	// logic: the allowlist below is the single choke point every mode feeds.
	var mode connect.Mode
	switch cfg.Mode {
	case "wmcp":
		mode = connect.NewWMCPMode(cfg.WMCP.Endpoint, cfg.WMCP.Token, logger)
	default: // native
		mode = connect.NewNativeMode(cfg.Webex.BotToken, logger)
	}

	// webex.allowed_emails is the authorization list for every mode, not just
	// native. Warn when it is empty, since that means "allow any direct sender".
	allowlist := connect.NewAllowlist(cfg.Webex.AllowedEmails)
	if len(cfg.Webex.AllowedEmails) == 0 {
		logger.Warn().Msg("webex.allowed_emails is empty — any sender in a 1:1 space may run commands; group and relay senders are denied")
	} else {
		logger.Info().Int("count", len(cfg.Webex.AllowedEmails)).Msg("Sender allowlist enabled")
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
		allowlist:      allowlist,
		startTime:      time.Now(),
	}

	return agent, nil
}

// lockdownPaths assembles the set of paths the agent's own tools must never
// read or modify when lockdown is enabled: the config file + its directory
// (from Config), the pass password store, and the running binary's directory.
func lockdownPaths(cfg *config.Config) []string {
	paths := cfg.LockdownPaths()

	// pass store (holds the GPG-encrypted secrets).
	if storeDir := os.Getenv("PASSWORD_STORE_DIR"); storeDir != "" {
		paths = append(paths, storeDir)
	} else {
		home, err := os.UserHomeDir()
		if err == nil && home != "" {
			paths = append(paths, filepath.Join(home, ".password-store"))
		}
	}

	// Directory of the running binary (prevents the agent overwriting itself).
	if exePath, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Dir(exePath))
	}

	return paths
}

// newConverser constructs the inference client for the resolved provider.
func newConverser(provider string, cfg *config.Config) (ai.Converser, error) {
	switch provider {
	case config.ProviderInferd:
		// Short probe timeout: the daemon is local and either up or not.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return ai.NewInferdClient(ctx, cfg.AI.InferdSocket, cfg.AI.Temperature)
	case config.ProviderOpenAICompat:
		return ai.NewOpenAICompatClient(cfg.AI.OpenAIBaseURL, cfg.AI.OpenAIAPIKey, cfg.AI.Model, cfg.AI.Temperature)
	default:
		return nil, fmt.Errorf("unknown AI provider: %s", provider)
	}
}

// executeToolGuarded is the single command-gating path shared by the AI
// processor loop and (indirectly) the passthrough handler. It runs a tool
// through the executor — which applies the command policy for
// execute_command — and, when a *confirmable* command is blocked and
// challenge-response is enabled, records a pending challenge keyed by the space
// and returns a confirmation prompt instead of the raw block error. Hard
// denials get no confirmation path. All security guardrails
// are enforced here regardless of caller, so interpret and passthrough modes
// gate identically.
func executeToolGuarded(
	ctx context.Context,
	exec *executor.Executor,
	challengeStore *security.ChallengeStore,
	toolName string,
	params map[string]any,
) (string, error) {
	result, err := exec.Execute(ctx, toolName, params)
	if err != nil {
		return "", err
	}

	// When a command is blocked by a *confirmable* policy rule and
	// challenge-response is enabled, store the pending challenge and ask the
	// user to confirm. Hard denials (config/secret access) get no confirmation
	// path — the reply would travel over the same channel whose credentials the
	// lockdown protects — so they fall through to the plain error below.
	if toolName == "execute_command" && challengeStore.Enabled() &&
		result.Denial != nil && result.Denial.Disposition == security.DispositionChallenge {
		if cmd, ok := params["command"].(string); ok {
			if spaceID, ok := ctx.Value(spaceIDKey).(string); ok {
				challengeStore.SetPending(spaceID, cmd, result.Denial.Reason)
				return fmt.Sprintf(
					"Command blocked: %s\n\nThis command requires confirmation. "+
						"Reply with the challenge response to proceed. "+
						"The confirmation expires in 2 minutes.",
					result.Denial.Reason,
				), nil
			}
		}
	}

	// Combine output and error into a single string for the caller.
	output := result.Output
	if result.Error != "" {
		output += "\nError: " + result.Error
	}
	return output, nil
}

// Run starts the agent and runs the main event loop
func (a *Agent) Run(ctx context.Context) error {
	// Register message handler
	a.mode.OnMessage(a.messageHandler)

	// Connect to Webex or WMCP backend
	if err := a.mode.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	a.mu.Lock()
	a.connected = true
	a.mu.Unlock()
	a.logger.Info().Msg("Agent connected to message service")

	// Start health check server if enabled
	if a.cfg.Health.Enabled {
		if a.cfg.Health.AllowNonLoopback {
			a.logger.Warn().Str("addr", a.cfg.Health.Addr).
				Msg("health.allow_non_loopback is set: the unauthenticated health endpoint may be reachable from the network")
		}
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
	a.mu.Lock()
	a.connected = false
	a.mu.Unlock()
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

	// Close challenge store and rate limiter background goroutines
	if a.challengeStore != nil {
		a.challengeStore.Close()
	}
	if a.rateLimiter != nil {
		a.rateLimiter.Close()
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

// authorize is the single authorization decision for inbound messages,
// regardless of connection mode. Keeping it here rather than in each Mode means
// a new Mode cannot silently ship without authz — which is exactly how wmcp mode
// ran unauthorized through v0.6.0. Fails closed: a nil allowlist denies.
func (a *Agent) authorize(msg connect.IncomingMessage) bool {
	if a.allowlist == nil {
		return false
	}
	return a.allowlist.IsAllowedInRoom(msg.Email, msg.RoomType)
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

	// Authorization first: an unauthorized sender must not reach the rate
	// limiter, the challenge store, or the executor, and gets no reply that
	// would confirm the bot is listening.
	if !a.authorize(msg) {
		a.logger.Warn().
			Str("email", msg.Email).
			Str("spaceID", msg.SpaceID).
			Str("roomType", msg.RoomType).
			Msg("Message from unauthorized sender, ignoring")
		if a.audit != nil {
			a.audit.Log(logging.AuditEntry{
				Timestamp:  start,
				Email:      msg.Email,
				SpaceID:    msg.SpaceID,
				RawMessage: msg.Text,
				Response:   "denied: sender not in webex.allowed_emails",
				Duration:   time.Since(start),
				Error:      "unauthorized sender",
			})
		}
		return
	}

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

	// Passthrough mode: execute the message directly as a command with no
	// local inference. Rate limiting, allowlist, and the challenge-response
	// check above have already run; the command itself is gated by the same
	// command policy and challenge-response path as the AI loop.
	if a.processor == nil {
		a.handlePassthrough(ctx, msg, start)
		return
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
		response = "Sorry, an internal error occurred while processing your request. Please try again."
		errMsg = err.Error()
	}

	// Update conversation history
	a.conversations.UpdateHistory(conversationKey, updatedHistory)

	// Extract tool call names and inputs for audit
	var toolCalls []string
	var toolInputs []string
	for _, m := range updatedHistory {
		for _, b := range m.Content {
			if b.Type == "tool_use" {
				toolCalls = append(toolCalls, b.ToolName)
				// Serialize tool input for audit trail
				inputStr := fmt.Sprintf("%s(%v)", b.ToolName, b.Input)
				toolInputs = append(toolInputs, inputStr)
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
			ToolInputs: toolInputs,
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

// handlePassthrough runs an inbound message directly as a shell command with
// no local inference (Webex-as-SSH). It routes through the same guarded path
// as the AI loop, so the command policy and challenge-response confirmation
// apply identically; a confirmable blocked command returns a confirmation
// prompt rather than executing.
func (a *Agent) handlePassthrough(ctx context.Context, msg connect.IncomingMessage, start time.Time) {
	command := strings.TrimSpace(msg.Text)
	if command == "" {
		return
	}

	// Thread spaceID through context so a blocked command can register a
	// pending challenge keyed by the space, exactly as in the AI loop.
	execCtx := context.WithValue(ctx, spaceIDKey, msg.SpaceID)

	response, err := executeToolGuarded(execCtx, a.exec, a.challengeStore, "execute_command",
		map[string]any{"command": command})
	var errMsg string
	if err != nil {
		a.logger.Error().Err(err).Msg("Passthrough command execution failed")
		errMsg = err.Error()
		response = "Sorry, an internal error occurred while executing your command."
	} else if response == "" {
		response = "Command executed successfully (no output)."
	}

	if a.audit != nil {
		a.audit.Log(logging.AuditEntry{
			Timestamp:  start,
			Email:      msg.Email,
			SpaceID:    msg.SpaceID,
			RawMessage: fmt.Sprintf("[passthrough] %s", command),
			ToolCalls:  []string{"execute_command"},
			ToolInputs: []string{fmt.Sprintf("execute_command(command=%q)", command)},
			Response:   response,
			Duration:   time.Since(start),
			Error:      errMsg,
		})
	}

	formattedResponse := connect.FormatResponse(response)
	if err := a.mode.SendMessage(ctx, msg.SpaceID, formattedResponse); err != nil {
		a.logger.Error().Err(err).Msg("Failed to send passthrough response")
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

	// Log audit entry for the confirmed execution — marked as confirmed
	if a.audit != nil {
		a.audit.Log(logging.AuditEntry{
			Timestamp:  start,
			Email:      msg.Email,
			SpaceID:    msg.SpaceID,
			RawMessage: fmt.Sprintf("[challenge confirmed] %s", pc.Command),
			ToolCalls:  []string{"execute_command"},
			ToolInputs: []string{fmt.Sprintf("execute_command(command=%q)", pc.Command)},
			Response:   response,
			Duration:   time.Since(start),
			Error:      errMsg,
			Confirmed:  true,
		})
	}

	formattedResponse := connect.FormatResponse(response)
	if err := a.mode.SendMessage(ctx, msg.SpaceID, formattedResponse); err != nil {
		a.logger.Error().Err(err).Msg("Failed to send challenge confirmation response")
	}
}

// getUsername retrieves the current username for the system prompt.
//
// No env-var fallback: $USER/$USERNAME are caller-controlled and are unset or
// wrong in exactly the contexts RemoteClaw runs in (systemd unit, LaunchAgent,
// Windows scheduled task), so they are worse than admitting we don't know.
func getUsername() string {
	currentUser, err := user.Current()
	if err != nil {
		return "unknown"
	}
	return currentUser.Username
}

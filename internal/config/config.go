package config

import (
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

// Build info variables set via ldflags
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// Config holds all RemoteClaw configuration settings
type Config struct {
	Mode      string            `mapstructure:"mode"`
	Webex     WebexConfig       `mapstructure:"webex"`
	WMCP      WMCPConfig        `mapstructure:"wmcp"`
	AWS       AWSConfig         `mapstructure:"aws"`
	AI        AIConfig          `mapstructure:"ai"`
	Execution ExecutionConfig   `mapstructure:"execution"`
	Security  SecurityConfig    `mapstructure:"security"`
	Logging   LoggingConfig     `mapstructure:"logging"`
	Health    HealthConfig      `mapstructure:"health"`
}

// WebexConfig holds Webex-specific settings
type WebexConfig struct {
	BotToken      string   `mapstructure:"bot_token"`
	AllowedEmails []string `mapstructure:"allowed_emails"`
}

// WMCPConfig holds Webex MCP backend settings
type WMCPConfig struct {
	Endpoint string `mapstructure:"endpoint"`
	Token    string `mapstructure:"token"`
}

// AWSConfig holds AWS-specific settings
type AWSConfig struct {
	Region string `mapstructure:"region"`
}

// AIConfig holds AI model settings
type AIConfig struct {
	Provider      string  `mapstructure:"provider"`     // "auto", "local", "bedrock"
	Model         string  `mapstructure:"model"`
	MaxTokens     int     `mapstructure:"max_tokens"`
	MaxIterations int     `mapstructure:"max_iterations"`
	Temperature   float64 `mapstructure:"temperature"`   // 0.0-1.0
	OllamaHost    string  `mapstructure:"ollama_host"`   // e.g. "http://localhost:11434"
}

// ExecutionConfig holds command execution settings
type ExecutionConfig struct {
	DefaultTimeout time.Duration `mapstructure:"default_timeout"`
	MaxTimeout     time.Duration `mapstructure:"max_timeout"`
	Shell          string        `mapstructure:"shell"`
}

// LoggingConfig holds logging settings
type LoggingConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
	File   string `mapstructure:"file"`
}

// SecurityConfig holds security hardening settings
type SecurityConfig struct {
	DangerousCommands bool   `mapstructure:"dangerous_commands"` // Enable dangerous command blocking
	AuditLog          string `mapstructure:"audit_log"`          // Path to audit log file (empty = disabled)
	RateLimitPerMin   int    `mapstructure:"rate_limit_per_min"` // Max requests per minute per space
	Challenge         string `mapstructure:"challenge"`          // Challenge string for destructive command confirmation (empty = disabled)
}

// HealthConfig holds health check settings
type HealthConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Addr    string `mapstructure:"addr"`
}

// Load reads and parses a YAML config file, applies defaults, and validates the configuration.
// If a .env file exists in the current directory, it is loaded first (does not override system env vars).
func Load(path string) (*Config, error) {
	// Load .env file if present — Overload() means .env values take precedence
	// over system env vars. If .env does not exist, the error is ignored.
	_ = godotenv.Overload()

	cfg := &Config{}

	// Set up viper
	v := viper.New()
	v.SetConfigType("yaml")
	v.SetConfigFile(path)

	// Read the config file
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Apply defaults before unmarshaling
	applyDefaults(v)

	// Unmarshal into Config struct
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Expand environment variables in string fields
	cfg.expandEnvVars()

	// Validate the configuration
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// applyDefaults sets default values in viper before unmarshaling
func applyDefaults(v *viper.Viper) {
	v.SetDefault("mode", "native")
	v.SetDefault("aws.region", "us-west-2")
	v.SetDefault("ai.provider", "auto")
	v.SetDefault("ai.model", "phi4-mini")
	v.SetDefault("ai.max_tokens", 4096)
	v.SetDefault("ai.max_iterations", 10)
	v.SetDefault("ai.temperature", 0.2)
	v.SetDefault("execution.default_timeout", "30s")
	v.SetDefault("execution.max_timeout", "5m")
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "json")
	v.SetDefault("security.dangerous_commands", true)
	v.SetDefault("security.audit_log", "")
	v.SetDefault("security.rate_limit_per_min", 10)
	v.SetDefault("security.challenge", "")
	v.SetDefault("health.enabled", true)
	v.SetDefault("health.addr", "127.0.0.1:9090")
}

// expandEnvVars expands environment variables in string fields using os.ExpandEnv
func (c *Config) expandEnvVars() {
	c.Webex.BotToken = os.ExpandEnv(c.Webex.BotToken)
	c.WMCP.Endpoint = os.ExpandEnv(c.WMCP.Endpoint)
	c.WMCP.Token = os.ExpandEnv(c.WMCP.Token)
	c.AI.OllamaHost = os.ExpandEnv(c.AI.OllamaHost)
	c.Execution.Shell = os.ExpandEnv(c.Execution.Shell)
	c.Security.AuditLog = os.ExpandEnv(c.Security.AuditLog)
	c.Security.Challenge = os.ExpandEnv(c.Security.Challenge)
	c.Logging.File = os.ExpandEnv(c.Logging.File)
}

// ResolveAIProvider determines the AI provider based on config and environment
func (c *Config) ResolveAIProvider() string {
	switch c.AI.Provider {
	case "local", "bedrock":
		return c.AI.Provider
	default: // "auto" or empty
		if os.Getenv("AWS_ACCESS_KEY_ID") != "" && os.Getenv("AWS_SECRET_ACCESS_KEY") != "" {
			return "bedrock"
		}
		return "local"
	}
}

// Validate checks that required fields are populated based on the configured mode
func (c *Config) Validate() error {
	// Validate mode
	if c.Mode != "native" && c.Mode != "wmcp" {
		return fmt.Errorf("invalid mode: %q (must be 'native' or 'wmcp')", c.Mode)
	}

	// Validate mode-specific requirements
	if c.Mode == "native" {
		if c.Webex.BotToken == "" {
			return fmt.Errorf("webex.bot_token is required in native mode")
		}
	}

	if c.Mode == "wmcp" {
		if c.WMCP.Endpoint == "" {
			return fmt.Errorf("wmcp.endpoint is required in wmcp mode")
		}
		if c.WMCP.Token == "" {
			return fmt.Errorf("wmcp.token is required in wmcp mode")
		}
	}

	// AWS region is only required when using Bedrock
	if c.ResolveAIProvider() == "bedrock" {
		if c.AWS.Region == "" {
			return fmt.Errorf("aws.region is required when using bedrock provider")
		}
		// If model is still the local default, override to Bedrock default
		if c.AI.Model == "phi4-mini" || c.AI.Model == "phi4" {
			c.AI.Model = "global.anthropic.claude-sonnet-4-6"
		}
	}

	// Cap temperature to 0.3 for security consistency — high temperatures
	// increase non-determinism which can cause intermittent safety bypass.
	if c.AI.Temperature > 0.3 {
		c.AI.Temperature = 0.3
	}

	return nil
}

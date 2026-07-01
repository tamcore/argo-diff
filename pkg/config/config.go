package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the application configuration
type Config struct {
	// Server configuration
	Port        int
	MetricsPort int

	// Worker configuration
	WorkerCount int
	QueueSize   int

	// Security configuration
	RepoAllowlist []string

	// Logging configuration
	LogLevel string

	// Rate limiting configuration
	RateLimitPerRepo int // requests per minute per repository (0 = disabled)

	// Job processing configuration
	JobTimeout time.Duration // maximum duration for a single diff job

	// ArgoCD configuration
	ArgocdServer    string
	ArgocdPlainText bool
}

// Load reads configuration from environment variables
func Load() (*Config, error) {
	port, err := getEnvInt("PORT", 8080)
	if err != nil {
		return nil, err
	}
	metricsPort, err := getEnvInt("METRICS_PORT", 9090)
	if err != nil {
		return nil, err
	}
	workerCount, err := getEnvInt("WORKER_COUNT", 1)
	if err != nil {
		return nil, err
	}
	queueSize, err := getEnvInt("QUEUE_SIZE", 100)
	if err != nil {
		return nil, err
	}
	rateLimitPerRepo, err := getEnvInt("RATE_LIMIT_PER_REPO", 10) // 10 requests/min default
	if err != nil {
		return nil, err
	}
	argocdPlainText, err := getEnvBool("ARGOCD_PLAINTEXT", true)
	if err != nil {
		return nil, err
	}
	jobTimeout, err := getEnvDuration("JOB_TIMEOUT", 10*time.Minute)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		Port:             port,
		MetricsPort:      metricsPort,
		WorkerCount:      workerCount,
		QueueSize:        queueSize,
		LogLevel:         getEnvString("LOG_LEVEL", "info"),
		RateLimitPerRepo: rateLimitPerRepo,
		JobTimeout:       jobTimeout,
		ArgocdServer:     getEnvString("ARGOCD_SERVER", "argocd-server:80"),
		ArgocdPlainText:  argocdPlainText,
	}

	if err := validate(cfg); err != nil {
		return nil, err
	}

	// Parse repository allowlist (required)
	allowlistStr := os.Getenv("REPO_ALLOWLIST")
	if allowlistStr == "" {
		return nil, fmt.Errorf("REPO_ALLOWLIST environment variable is required")
	}

	cfg.RepoAllowlist = parseAllowlist(allowlistStr)
	if len(cfg.RepoAllowlist) == 0 {
		return nil, fmt.Errorf("REPO_ALLOWLIST must contain at least one entry")
	}

	// Validate ArgoCD server
	if cfg.ArgocdServer == "" {
		return nil, fmt.Errorf("ARGOCD_SERVER environment variable is required")
	}

	return cfg, nil
}

// validate checks that numeric configuration values are within sane ranges
func validate(cfg *Config) error {
	if cfg.Port < 1 || cfg.Port > 65535 {
		return fmt.Errorf("PORT must be between 1 and 65535, got %d", cfg.Port)
	}
	if cfg.MetricsPort < 1 || cfg.MetricsPort > 65535 {
		return fmt.Errorf("METRICS_PORT must be between 1 and 65535, got %d", cfg.MetricsPort)
	}
	if cfg.WorkerCount < 1 {
		return fmt.Errorf("WORKER_COUNT must be at least 1, got %d", cfg.WorkerCount)
	}
	if cfg.QueueSize < 1 {
		return fmt.Errorf("QUEUE_SIZE must be at least 1, got %d", cfg.QueueSize)
	}
	if cfg.RateLimitPerRepo < 0 {
		return fmt.Errorf("RATE_LIMIT_PER_REPO must not be negative, got %d", cfg.RateLimitPerRepo)
	}
	if cfg.JobTimeout <= 0 {
		return fmt.Errorf("JOB_TIMEOUT must be positive, got %s", cfg.JobTimeout)
	}
	return nil
}

// IsRepoAllowed checks if a repository matches the allowlist
func (c *Config) IsRepoAllowed(repo string) bool {
	repo = strings.ToLower(strings.TrimSpace(repo))

	for _, pattern := range c.RepoAllowlist {
		if matchPattern(pattern, repo) {
			return true
		}
	}

	return false
}

// matchPattern checks if a repository matches a pattern
// Supports exact matches (owner/repo) and wildcards (owner/*)
func matchPattern(pattern, repo string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	repo = strings.ToLower(strings.TrimSpace(repo))

	// Full wildcard
	if pattern == "*/*" {
		return true
	}

	// Exact match
	if pattern == repo {
		return true
	}

	// Wildcard match (e.g., "myorg/*")
	if before, ok := strings.CutSuffix(pattern, "/*"); ok {
		prefix := before
		return strings.HasPrefix(repo, prefix+"/")
	}

	return false
}

// parseAllowlist splits comma-separated allowlist into individual patterns
func parseAllowlist(allowlistStr string) []string {
	parts := strings.Split(allowlistStr, ",")
	result := make([]string, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}

	return result
}

// getEnvInt reads an integer from environment variable with a default value.
// Returns an error if the variable is set but not a valid integer.
func getEnvInt(key string, defaultValue int) (int, error) {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue, nil
	}

	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer, got %q", key, valueStr)
	}

	return value, nil
}

// getEnvDuration reads a duration (e.g. "10m", "90s") from environment
// variable with a default value. Returns an error if the variable is set but
// not a valid duration.
func getEnvDuration(key string, defaultValue time.Duration) (time.Duration, error) {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue, nil
	}

	value, err := time.ParseDuration(valueStr)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration (e.g. \"10m\"), got %q", key, valueStr)
	}

	return value, nil
}

// getEnvString reads a string from environment variable with a default value
func getEnvString(key string, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// getEnvBool reads a boolean from environment variable with a default value.
// Returns an error if the variable is set but not a valid boolean.
func getEnvBool(key string, defaultValue bool) (bool, error) {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue, nil
	}

	value, err := strconv.ParseBool(valueStr)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean, got %q", key, valueStr)
	}

	return value, nil
}

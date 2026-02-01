package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
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
}

// Load reads configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{
		Port:             getEnvInt("PORT", 8080),
		MetricsPort:      getEnvInt("METRICS_PORT", 9090),
		WorkerCount:      getEnvInt("WORKER_COUNT", 1),
		QueueSize:        getEnvInt("QUEUE_SIZE", 100),
		LogLevel:         getEnvString("LOG_LEVEL", "info"),
		RateLimitPerRepo: getEnvInt("RATE_LIMIT_PER_REPO", 10), // 10 requests/min default
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

	return cfg, nil
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
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
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

// getEnvInt reads an integer from environment variable with a default value
func getEnvInt(key string, defaultValue int) int {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}

	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return defaultValue
	}

	return value
}

// getEnvString reads a string from environment variable with a default value
func getEnvString(key string, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

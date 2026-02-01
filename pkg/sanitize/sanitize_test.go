package sanitize

import (
	"errors"
	"testing"
)

func TestString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "token pattern with equals",
			input:    `token="abc123def456"`,
			expected: `token: [REDACTED]`,
		},
		{
			name:     "password pattern",
			input:    `password: mysecretpassword`,
			expected: `password: [REDACTED]`,
		},
		{
			name:     "secret pattern",
			input:    `secret=very_secret_value`,
			expected: `secret: [REDACTED]`,
		},
		{
			name:     "key pattern",
			input:    `api_key: "my-api-key-12345"`,
			expected: `api_key: [REDACTED]`,
		},
		{
			name:     "bearer token",
			input:    `Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9`,
			expected: `Authorization: Bearer [REDACTED]`,
		},
		{
			name:     "GitHub token ghp",
			input:    `ghp_abcdefghijklmnopqrstuvwxyz1234567890`,
			expected: `[REDACTED_GH_TOKEN]`,
		},
		{
			name:     "GitHub token gho",
			input:    `gho_abcdefghijklmnopqrstuvwxyz1234567890`,
			expected: `[REDACTED_GH_TOKEN]`,
		},
		{
			name:     "no sensitive data",
			input:    `This is a normal log message`,
			expected: `This is a normal log message`,
		},
		{
			name:     "multiple patterns",
			input:    `token="abc" and Bearer xyz123 and ghp_abcdefghijklmnopqrstuvwxyz1234567890`,
			expected: `token: [REDACTED] and Bearer [REDACTED] and [REDACTED_GH_TOKEN]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := String(tt.input)
			if result != tt.expected {
				t.Errorf("String(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestToken(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "short token",
			input:    "abc",
			expected: "[REDACTED]",
		},
		{
			name:     "exactly 8 chars",
			input:    "12345678",
			expected: "[REDACTED]",
		},
		{
			name:     "normal token",
			input:    "ghp_abcdefghijklmnopqrstuvwxyz123456",
			expected: "ghp_...3456",
		},
		{
			name:     "long token",
			input:    "very_long_secret_token_value_here_1234567890",
			expected: "very...7890",
		},
		{
			name:     "empty token",
			input:    "",
			expected: "[REDACTED]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Token(tt.input)
			if result != tt.expected {
				t.Errorf("Token(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestError(t *testing.T) {
	tests := []struct {
		name     string
		input    error
		expected string
	}{
		{
			name:     "nil error",
			input:    nil,
			expected: "",
		},
		{
			name:     "error with token",
			input:    errors.New(`failed with token="secret123"`),
			expected: `failed with token: [REDACTED]`,
		},
		{
			name:     "error with bearer",
			input:    errors.New(`unauthorized: Bearer abc123def`),
			expected: `unauthorized: Bearer [REDACTED]`,
		},
		{
			name:     "error without sensitive data",
			input:    errors.New("connection timeout"),
			expected: "connection timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Error(tt.input)
			if result != tt.expected {
				t.Errorf("Error(%v) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestHeaders(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string][]string
		checkKey string
		expected []string
	}{
		{
			name: "authorization header",
			input: map[string][]string{
				"Authorization": {"Bearer secret-token"},
			},
			checkKey: "Authorization",
			expected: []string{"[REDACTED]"},
		},
		{
			name: "x-api-key header",
			input: map[string][]string{
				"X-Api-Key": {"my-api-key"},
			},
			checkKey: "X-Api-Key",
			expected: []string{"[REDACTED]"},
		},
		{
			name: "custom token header",
			input: map[string][]string{
				"X-Access-Token": {"abc123"},
			},
			checkKey: "X-Access-Token",
			expected: []string{"[REDACTED]"},
		},
		{
			name: "content-type header preserved",
			input: map[string][]string{
				"Content-Type": {"application/json"},
			},
			checkKey: "Content-Type",
			expected: []string{"application/json"},
		},
		{
			name: "mixed headers",
			input: map[string][]string{
				"Authorization": {"Bearer secret"},
				"Content-Type":  {"application/json"},
				"User-Agent":    {"test-client"},
			},
			checkKey: "Content-Type",
			expected: []string{"application/json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Headers(tt.input)
			if val, ok := result[tt.checkKey]; !ok {
				t.Errorf("Headers() missing key %q", tt.checkKey)
			} else if len(val) != len(tt.expected) || val[0] != tt.expected[0] {
				t.Errorf("Headers()[%q] = %v, want %v", tt.checkKey, val, tt.expected)
			}
		})
	}
}

func TestHeadersEmpty(t *testing.T) {
	result := Headers(map[string][]string{})
	if len(result) != 0 {
		t.Errorf("Headers(empty) should return empty map, got %v", result)
	}
}

func TestHeadersDoesNotModifyOriginal(t *testing.T) {
	original := map[string][]string{
		"Authorization": {"Bearer secret"},
	}
	Headers(original)

	// Original should be unchanged
	if original["Authorization"][0] != "Bearer secret" {
		t.Error("Headers() modified the original map")
	}
}

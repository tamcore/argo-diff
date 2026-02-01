package sanitize

import (
	"regexp"
	"strings"
)

var (
	// tokenPattern matches common token patterns
	tokenPattern = regexp.MustCompile(`(?i)(token|password|secret|key|auth)["\s:=]+["']?([a-zA-Z0-9_\-\.]+)["']?`)
	// bearerPattern matches Bearer tokens in headers
	bearerPattern = regexp.MustCompile(`(?i)bearer\s+[a-zA-Z0-9_\-\.]+`)
	// ghTokenPattern matches GitHub tokens
	ghTokenPattern = regexp.MustCompile(`gh[pousr]_[a-zA-Z0-9]{36,}`)
)

// String redacts sensitive information from a string
func String(s string) string {
	s = tokenPattern.ReplaceAllString(s, "${1}: [REDACTED]")
	s = bearerPattern.ReplaceAllString(s, "Bearer [REDACTED]")
	s = ghTokenPattern.ReplaceAllString(s, "[REDACTED_GH_TOKEN]")
	return s
}

// Token masks a token, showing only first and last 4 characters
func Token(token string) string {
	if len(token) <= 8 {
		return "[REDACTED]"
	}
	return token[:4] + "..." + token[len(token)-4:]
}

// Error sanitizes error messages that might contain sensitive data
func Error(err error) string {
	if err == nil {
		return ""
	}
	return String(err.Error())
}

// Headers sanitizes HTTP headers, redacting Authorization
func Headers(headers map[string][]string) map[string][]string {
	sanitized := make(map[string][]string)
	for k, v := range headers {
		lowerKey := strings.ToLower(k)
		if lowerKey == "authorization" || lowerKey == "x-api-key" || strings.Contains(lowerKey, "token") {
			sanitized[k] = []string{"[REDACTED]"}
		} else {
			sanitized[k] = v
		}
	}
	return sanitized
}

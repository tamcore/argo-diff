package logging

import (
	"context"
	"log/slog"
	"os"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const (
	// RequestIDKey is the context key for request IDs
	RequestIDKey contextKey = "request_id"
	// JobIDKey is the context key for job IDs
	JobIDKey contextKey = "job_id"
)

var logger *slog.Logger

// Init initializes the global logger
func Init(level string) {
	var logLevel slog.Level
	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})
	logger = slog.New(handler)
	slog.SetDefault(logger)
}

// Logger returns the global logger
func Logger() *slog.Logger {
	if logger == nil {
		Init("info")
	}
	return logger
}

// WithRequestID adds a request ID to the context
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, RequestIDKey, requestID)
}

// WithJobID adds a job ID to the context
func WithJobID(ctx context.Context, jobID string) context.Context {
	return context.WithValue(ctx, JobIDKey, jobID)
}

// FromContext returns a logger with context values
func FromContext(ctx context.Context) *slog.Logger {
	l := Logger()
	if requestID, ok := ctx.Value(RequestIDKey).(string); ok {
		l = l.With("request_id", requestID)
	}
	if jobID, ok := ctx.Value(JobIDKey).(string); ok {
		l = l.With("job_id", jobID)
	}
	return l
}

// WithFields returns a logger with additional fields
func WithFields(args ...any) *slog.Logger {
	return Logger().With(args...)
}

// Info logs at info level
func Info(msg string, args ...any) {
	Logger().Info(msg, args...)
}

// Error logs at error level
func Error(msg string, args ...any) {
	Logger().Error(msg, args...)
}

// Debug logs at debug level
func Debug(msg string, args ...any) {
	Logger().Debug(msg, args...)
}

// Warn logs at warn level
func Warn(msg string, args ...any) {
	Logger().Warn(msg, args...)
}

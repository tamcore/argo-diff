package logging
package logging

import (
	"context"
	"log/slog"
	"os"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string





















































































}	Logger().Warn(msg, args...)func Warn(msg string, args ...any) {// Warn logs at warn level}	Logger().Debug(msg, args...)func Debug(msg string, args ...any) {// Debug logs at debug level}	Logger().Error(msg, args...)func Error(msg string, args ...any) {// Error logs at error level}	Logger().Info(msg, args...)func Info(msg string, args ...any) {// Info logs at info level}	return l	}		l = l.With("job_id", jobID)	if jobID, ok := ctx.Value(JobIDKey).(string); ok {	}		l = l.With("request_id", requestID)	if requestID, ok := ctx.Value(RequestIDKey).(string); ok {	l := Logger()func FromContext(ctx context.Context) *slog.Logger {// FromContext returns a logger with context values}	return context.WithValue(ctx, JobIDKey, jobID)func WithJobID(ctx context.Context, jobID string) context.Context {// WithJobID adds a job ID to the context}	return context.WithValue(ctx, RequestIDKey, requestID)func WithRequestID(ctx context.Context, requestID string) context.Context {// WithRequestID adds a request ID to the context}	return logger	}		Init("info")	if logger == nil {func Logger() *slog.Logger {// Logger returns the global logger}	slog.SetDefault(logger)	logger = slog.New(handler)	})		Level: logLevel,	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{	}		logLevel = slog.LevelInfo	default:		logLevel = slog.LevelError	case "error":		logLevel = slog.LevelWarn	case "warn":		logLevel = slog.LevelInfo	case "info":		logLevel = slog.LevelDebug	case "debug":	switch level {	var logLevel slog.Levelfunc Init(level string) {// Init initializes the global loggervar logger *slog.Logger)	JobIDKey contextKey = "job_id"	// JobIDKey is the context key for job IDs	RequestIDKey contextKey = "request_id"	// RequestIDKey is the context key for request IDsconst (